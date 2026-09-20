import { findManifestFile, parseGameResourceManifest } from './GameResourceManifest'
import { normalizeResourceBase, normalizeResourceRelativePath, resolveResourceLocation } from './GameResourcePath'
import type { ResourceLocation } from './GameResourcePath'
import { GAME_RESOURCE_MANIFEST_FILE } from './GameResourceTypes'
import type { GameResourceLocator, GameResourceManifest, GameResourceManifestFile } from './GameResourceTypes'

/** 可注入的 fetch：测试与原生壳可替换（默认走 globalThis.fetch）。 */
export type GameResourceFetch = (url: string, init?: RequestInit) => Promise<Response>

/** 延迟读取 globalThis.fetch，便于单测 stubGlobal。 */
export const defaultGameResourceFetch: GameResourceFetch = (url, init) => globalThis.fetch(url, init)

/** 清单缓存键前缀（localStorage）：{prefix}{gameId}@{resourceVersion} 。 */
export const GAME_RESOURCE_CACHE_PREFIX = 'zhiwellcare.game-resource.manifest.v1.'

/** 清单成功缓存 TTL：资源版本变化会换键，因此可以缓存较久（6 小时）。 */
export const MANIFEST_TTL_MS = 6 * 60 * 60 * 1000

/** 「无清单」缓存 TTL（5 分钟）：CDN 抖动或暂未发布清单时不至于反复打请求。 */
export const MANIFEST_FAILURE_TTL_MS = 5 * 60 * 1000

/** 缓存记录：manifest 为 null 表示「本轮确认无清单」。 */
interface CacheRecord {
  cachedAt: number
  manifest: GameResourceManifest | null
}

export interface GameResourceResolverOptions {
  fetchImpl?: GameResourceFetch
  /** 全局兜底 CDN 前缀（VITE_GAME_RESOURCE_CDN）；条目自带 resourceUrl 优先。 */
  cdnPrefix?: string
  ttlMs?: number
  failureTtlMs?: number
  /** 注入时钟（便于测试 TTL）。 */
  now?: () => number
}

/**
 * 远程游戏资源解析器：
 * 1) 同步解析 —— `resolveUrl()` 把目录条目的 resourceUrl 与相对路径拼成 CDN 绝对地址；
 *    resourceUrl 为空（且未配置全局兜底前缀）时返回 null，调用方静默使用内置资源。
 * 2) 清单校验 —— `loadManifest()` 异步取清单，内存 + localStorage 双层缓存（带 TTL）；
 *    获取失败或结构非法一律降级为「无清单」，绝不抛错、绝不阻塞游戏打开。
 * 3) 清单声明校验 —— `resolveDeclaredUrl()` 只在清单登记了该相对路径时给出 CDN 地址，
 *    未声明即回退内置资源（避免 CDN 缺文件导致画面空白）。
 *
 * resourceUrl 形状兼容两种（后端 fillResourceAssets 回填的是对象文件本身的 URL）：
 * - 目录前缀 `…/1.0.0/`  → base 即它本身，清单为 `…/1.0.0/manifest.json`；
 * - 文件 URL `…/1.0.0/manifest.json` → base 取父目录 `…/1.0.0/`，且该文件本身就是清单
 *   （不会再拼成 `…/manifest.json/manifest.json`）；
 * - 文件 URL `…/1.0.0/cover.png` → base 同样取父目录 `…/1.0.0/`，同目录资源按父目录拼接。
 */
export class GameResourceResolver {
  private readonly fetchImpl: GameResourceFetch
  private readonly cdnPrefix: string | undefined
  private readonly ttlMs: number
  private readonly failureTtlMs: number
  private readonly now: () => number
  /** 进程内缓存：同一会话内免网络、免 localStorage 反序列化。 */
  private readonly memory = new Map<string, CacheRecord>()
  /** 进行中的清单请求：并发调用共享同一次网络请求。 */
  private readonly inflight = new Map<string, Promise<GameResourceManifest | null>>()

  constructor(options: GameResourceResolverOptions = {}) {
    this.fetchImpl = options.fetchImpl ?? defaultGameResourceFetch
    this.cdnPrefix = options.cdnPrefix
    this.ttlMs = options.ttlMs ?? MANIFEST_TTL_MS
    this.failureTtlMs = options.failureTtlMs ?? MANIFEST_FAILURE_TTL_MS
    this.now = options.now ?? Date.now
  }

  /** 同步解析资源地址；resourceUrl 为空（且无全局兜底前缀）或路径非法时返回 null。 */
  resolveUrl(game: GameResourceLocator, relativePath: string): string | null {
    const location = this.locate(game)
    if (!location?.base) return null
    const relative = normalizeResourceRelativePath(relativePath)
    if (!relative) return null
    // 目录前缀自带查询串（如 ?v=1）时追加在文件名之后，保证地址仍可访问。
    return `${location.base}${relative}${location.suffix}`
  }

  /** 清单文件地址（无 CDN 前缀时为 null）；resourceUrl 本身即 manifest.json 时就是它本身。 */
  manifestUrl(game: GameResourceLocator): string | null {
    return this.locate(game)?.manifestUrl ?? null
  }

  /** 异步获取清单：失败/非法/无 CDN 前缀都返回 null，不抛错。 */
  async loadManifest(game: GameResourceLocator): Promise<GameResourceManifest | null> {
    const url = this.manifestUrl(game)
    if (!url) return null
    const key = gameResourceManifestCacheKey(game.gameId, game.resourceVersion)
    const cached = this.readCache(key)
    if (cached) return cached.manifest

    const pending = this.inflight.get(key)
    if (pending) return pending

    const request = this.fetchManifest(url)
      .then((manifest) => {
        this.writeCache(key, manifest)
        return manifest
      })
      .catch(() => {
        // 兜底：任何未预期异常都按「无清单」处理并做短暂负缓存。
        this.writeCache(key, null)
        return null
      })
      .finally(() => { this.inflight.delete(key) })
    this.inflight.set(key, request)
    return request
  }

  /** 清单中登记的文件（含 sha256/size）；未登记或无清单返回 null。 */
  async lookupFile(game: GameResourceLocator, relativePath: string): Promise<GameResourceManifestFile | null> {
    return findManifestFile(await this.loadManifest(game), relativePath)
  }

  /** 清单声明校验后的 CDN 地址：未声明（含无清单）返回 null → 调用方用内置资源。 */
  async resolveDeclaredUrl(game: GameResourceLocator, relativePath: string): Promise<string | null> {
    const file = await this.lookupFile(game, relativePath)
    return file ? this.resolveUrl(game, file.path) : null
  }

  /** 缓存失效：不传参数清全部（内存 + localStorage），传条目只清该游戏该版本。 */
  invalidate(game?: GameResourceLocator): void {
    if (!game) {
      this.memory.clear()
      clearPersistedManifests()
      return
    }
    const key = gameResourceManifestCacheKey(game.gameId, game.resourceVersion)
    this.memory.delete(key)
    removePersistedManifest(key)
  }

  /** 清理入口别名（语义与 invalidate() 一致，供 UI/调试页调用）。 */
  clearCache(): void {
    this.invalidate()
  }

  /** 目录条目资源位置：resourceUrl（目录前缀或文件 URL）优先，缺失时用全局兜底前缀。 */
  private locate(game: GameResourceLocator): ResourceLocation | null {
    return resolveResourceLocation(game.resourceUrl) ?? this.fallbackLocation(game)
  }

  /**
   * 全局兜底前缀推导的目录：{prefix}/{gameId}/{resourceVersion}/（gameId 与版本做转义，避免路径注入）。
   * 注意：后端对象键为 `games/{gameId}/{version}/{filename}`，因此 VITE_GAME_RESOURCE_CDN
   * 需要包含 `games` 这一段，例如配成 `https://cdn.example.com/games`。
   */
  private fallbackLocation(game: GameResourceLocator): ResourceLocation | null {
    const prefix = normalizeResourceBase(this.cdnPrefix ?? gameResourceCdnPrefix())
    if (!prefix) return null
    const gameId = game.gameId.trim()
    const version = game.resourceVersion.trim()
    if (!gameId || !version) return null
    const base = `${prefix}/${encodeURIComponent(gameId)}/${encodeURIComponent(version)}/`
    return { base, suffix: '', manifestUrl: `${base}${GAME_RESOURCE_MANIFEST_FILE}` }
  }

  private async fetchManifest(url: string): Promise<GameResourceManifest | null> {
    try {
      const response = await this.fetchImpl(url, { headers: { Accept: 'application/json' } })
      if (!response.ok) return null
      return parseGameResourceManifest(await response.json())
    } catch {
      // 网络不可达 / 非 JSON 响应 → 无清单。
      return null
    }
  }

  private readCache(key: string): CacheRecord | null {
    const record = this.memory.get(key) ?? readPersistedManifest(key)
    if (!record) return null
    const ttl = record.manifest ? this.ttlMs : this.failureTtlMs
    if (this.now() - record.cachedAt >= ttl) {
      this.memory.delete(key)
      removePersistedManifest(key)
      return null
    }
    // localStorage 命中的记录回填内存，后续调用不再读存储。
    this.memory.set(key, record)
    return record
  }

  private writeCache(key: string, manifest: GameResourceManifest | null): void {
    const record: CacheRecord = { cachedAt: this.now(), manifest }
    this.memory.set(key, record)
    writePersistedManifest(key, record)
  }
}

/** 全局单例：页面与渲染层直接复用同一份清单缓存。 */
export const gameResourceResolver = new GameResourceResolver()

/** 便捷函数：走全局单例解析 CDN 地址（resourceUrl 为空返回 null → 使用内置资源）。 */
export function resolveGameResourceUrl(game: GameResourceLocator, relativePath: string): string | null {
  return gameResourceResolver.resolveUrl(game, relativePath)
}

/** 清单缓存键：键里同时含 gameId 与 resourceVersion，版本升级自然失效。 */
export function gameResourceManifestCacheKey(gameId: string, resourceVersion: string): string {
  return `${GAME_RESOURCE_CACHE_PREFIX}${gameId.trim()}@${resourceVersion.trim()}`
}

/**
 * 全局兜底 CDN 前缀（VITE_GAME_RESOURCE_CDN，可留空）；条目自带的 resourceUrl 优先。
 * 该前缀需包含后端对象键的 `games` 这一段（对象键为 games/{gameId}/{version}/{filename}），
 * 例如 `https://cdn.example.com/games`，最终拼成 `…/games/{gameId}/{version}/{filename}`。
 */
export function gameResourceCdnPrefix(): string {
  return import.meta.env.VITE_GAME_RESOURCE_CDN ?? ''
}

function readPersistedManifest(key: string): CacheRecord | null {
  try {
    if (typeof localStorage === 'undefined') return null
    const raw = localStorage.getItem(key)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null) return null
    const { cachedAt, manifest } = parsed as { cachedAt?: unknown; manifest?: unknown }
    if (typeof cachedAt !== 'number') return null
    if (manifest === null || manifest === undefined) return { cachedAt, manifest: null }
    const normalised = parseGameResourceManifest(manifest)
    // 缓存内容结构非法时按未命中处理（重新拉取），避免坏缓存长期生效。
    return normalised ? { cachedAt, manifest: normalised } : null
  } catch {
    return null
  }
}

function writePersistedManifest(key: string, record: CacheRecord): void {
  try {
    if (typeof localStorage === 'undefined') return
    localStorage.setItem(key, JSON.stringify(record))
  } catch {
    // 存储受限（隐私模式/配额）时仅保留内存缓存。
  }
}

function removePersistedManifest(key: string): void {
  try { if (typeof localStorage !== 'undefined') localStorage.removeItem(key) } catch { /* 忽略存储受限 */ }
}

/** 清理全部清单缓存条目（仅限本模块前缀，不动其它本地数据）。 */
function clearPersistedManifests(): void {
  try {
    if (typeof localStorage === 'undefined') return
    const keys: string[] = []
    for (let index = 0; index < localStorage.length; index += 1) {
      const key = localStorage.key(index)
      if (key && key.startsWith(GAME_RESOURCE_CACHE_PREFIX)) keys.push(key)
    }
    for (const key of keys) localStorage.removeItem(key)
  } catch {
    // 忽略存储受限
  }
}
