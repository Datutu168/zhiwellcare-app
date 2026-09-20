import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { findManifestFile, parseGameResourceManifest } from '../src/core/resources/GameResourceManifest'
import {
  GAME_RESOURCE_CACHE_PREFIX,
  GameResourceResolver,
  gameResourceManifestCacheKey,
  resolveGameResourceUrl,
} from '../src/core/resources/GameResourceResolver'
import type { GameResourceLocator } from '../src/core/resources/GameResourceTypes'

const CDN = 'https://cdn.example.com/games/kart-racing/1.0.0/'

/** 目录条目形态的资源定位信息（只依赖三个字段）。 */
function locator(resourceUrl: string, overrides: Partial<GameResourceLocator> = {}): GameResourceLocator {
  return { gameId: 'kart-racing', resourceVersion: '1.0.0', resourceUrl, ...overrides }
}

/** node 环境 localStorage polyfill（清单缓存按需读取）。 */
function installLocalStorage(): void {
  const data = new Map<string, string>()
  const storage: Storage = {
    get length() { return data.size },
    clear: () => data.clear(),
    getItem: (key) => data.get(key) ?? null,
    key: (index) => [...data.keys()][index] ?? null,
    removeItem: (key) => { data.delete(key) },
    setItem: (key, value) => { data.set(key, String(value)) },
  }
  ;(globalThis as Record<string, unknown>).localStorage = storage
}

/** 返回 JSON 的 fetch mock（body 传字符串时原样返回，便于构造非法 JSON）。 */
function jsonFetch(body: unknown, status = 200) {
  return vi.fn(async (_url: string, _init?: RequestInit) => new Response(
    typeof body === 'string' ? body : JSON.stringify(body),
    { status, headers: { 'Content-Type': 'application/json' } },
  ))
}

// 全局兜底前缀默认留空：确保「resourceUrl 为空 → 回退内置资源」的判定与开发者本地 .env 无关。
beforeEach(() => { vi.stubEnv('VITE_GAME_RESOURCE_CDN', '') })
afterEach(() => { vi.unstubAllEnvs() })

describe('远程游戏资源地址解析（resourceUrl 为空 → 回退内置资源）', () => {
  it('resourceUrl 为空串 / 纯空白时返回 null，调用方使用内置资源', () => {
    expect(resolveGameResourceUrl(locator(''), 'art/tile.png')).toBeNull()
    expect(resolveGameResourceUrl(locator('   '), 'art/tile.png')).toBeNull()
  })

  it('目录前缀形状（以 / 结尾）拼出 CDN 绝对地址，多重斜杠被折叠', () => {
    const expected = `${CDN}art/tile.png`
    expect(resolveGameResourceUrl(locator(CDN), 'art/tile.png')).toBe(expected)
    expect(resolveGameResourceUrl(locator(` ${CDN}/// `), 'art/tile.png')).toBe(expected)
    expect(resolveGameResourceUrl(locator('https://cdn.example.com//games//kart-racing//1.0.0//'), 'art/tile.png')).toBe(expected)
  })

  it('文件形状（后端 resourceUrl 指向对象文件本身）取父目录拼接，不把文件当目录', () => {
    const expected = `${CDN}art/tile.png`
    // 后端 fillResourceAssets 回填的是资产对象文件 URL（games/{gameId}/{version}/{filename}）。
    expect(resolveGameResourceUrl(locator(`${CDN}manifest.json`), 'art/tile.png')).toBe(expected)
    expect(resolveGameResourceUrl(locator(`${CDN}asset.zip`), 'art/tile.png')).toBe(expected)
    expect(resolveGameResourceUrl(locator(`${CDN}cover.png`), 'art/tile.png')).toBe(expected)
    // 关键回归：绝不能拼出 …/manifest.json/art/tile.png。
    expect(resolveGameResourceUrl(locator(`${CDN}manifest.json`), 'art/tile.png')).not.toContain('manifest.json/')
  })

  it('相对路径的前导斜杠、`.` 段与重复斜杠被规范化', () => {
    const expected = `${CDN}art/tile.png`
    expect(resolveGameResourceUrl(locator(CDN), '/art/tile.png')).toBe(expected)
    expect(resolveGameResourceUrl(locator(CDN), 'art/./tile.png')).toBe(expected)
    expect(resolveGameResourceUrl(locator(CDN), 'art//tile.png')).toBe(expected)
    expect(resolveGameResourceUrl(locator(CDN), 'art\\tile.png')).toBe(expected)
  })

  it('相对路径为空或只有分隔符时返回 null', () => {
    expect(resolveGameResourceUrl(locator(CDN), '')).toBeNull()
    expect(resolveGameResourceUrl(locator(CDN), '   ')).toBeNull()
    expect(resolveGameResourceUrl(locator(CDN), '/')).toBeNull()
  })
})

describe('远程游戏资源地址解析（安全边界）', () => {
  it('拒绝相对路径中的 `..` 上跳（含 %2e 编码形式）', () => {
    expect(resolveGameResourceUrl(locator(CDN), '../secret.png')).toBeNull()
    expect(resolveGameResourceUrl(locator(CDN), 'art/../../secret.png')).toBeNull()
    expect(resolveGameResourceUrl(locator(CDN), '%2e%2e/secret.png')).toBeNull()
    expect(resolveGameResourceUrl(locator(`${CDN}../`), 'art/tile.png')).toBeNull()
  })

  it('拒绝相对路径中的绝对 URL / 协议无关地址注入', () => {
    expect(resolveGameResourceUrl(locator(CDN), 'https://evil.example.com/x.png')).toBeNull()
    expect(resolveGameResourceUrl(locator(CDN), '//evil.example.com/x.png')).toBeNull()
    expect(resolveGameResourceUrl(locator(CDN), 'javascript:alert(1)')).toBeNull()
    expect(resolveGameResourceUrl(locator(CDN), 'data:text/plain,x')).toBeNull()
  })

  it('resourceUrl 为非 http(s) scheme 或协议无关地址时返回 null', () => {
    expect(resolveGameResourceUrl(locator('javascript:alert(1)'), 'a.png')).toBeNull()
    expect(resolveGameResourceUrl(locator('data:text/plain,x'), 'a.png')).toBeNull()
    expect(resolveGameResourceUrl(locator('//evil.example.com/'), 'a.png')).toBeNull()
    expect(resolveGameResourceUrl(locator('file:///c:/games/'), 'a.png')).toBeNull()
  })

  it('resourceUrl 支持根绝对路径与普通相对路径（同源部署）', () => {
    expect(resolveGameResourceUrl(locator('/cdn/games/kart-racing/1.0.0/'), 'a.png')).toBe('/cdn/games/kart-racing/1.0.0/a.png')
    expect(resolveGameResourceUrl(locator('cdn/games/kart-racing/1.0.0/cover.png'), 'a.png')).toBe('cdn/games/kart-racing/1.0.0/a.png')
    // 目录形态必须带尾斜杠或指向目录内文件：否则按文件 URL 取父目录（不会产生重复拼接的坏地址）。
    expect(resolveGameResourceUrl(locator('cdn/games/kart-racing/1.0.0'), 'a.png')).toBe('cdn/games/kart-racing/a.png')
  })

  it('配置 VITE_GAME_RESOURCE_CDN 时作为兜底前缀；条目自带 resourceUrl 优先', () => {
    // 前缀需包含后端对象键的 games 段：对象键为 games/{gameId}/{version}/{filename}。
    vi.stubEnv('VITE_GAME_RESOURCE_CDN', 'https://cdn.example.com/games')

    expect(resolveGameResourceUrl(locator(''), 'art/tile.png')).toBe('https://cdn.example.com/games/kart-racing/1.0.0/art/tile.png')
    expect(new GameResourceResolver().manifestUrl(locator(''))).toBe('https://cdn.example.com/games/kart-racing/1.0.0/manifest.json')
    // 条目自带前缀优先于全局兜底前缀。
    expect(resolveGameResourceUrl(locator(CDN), 'art/tile.png')).toBe(`${CDN}art/tile.png`)
    expect(new GameResourceResolver().manifestUrl(locator(`${CDN}manifest.json`))).toBe(`${CDN}manifest.json`)
    // gameId / resourceVersion 缺失时无法推导目录，仍回退内置资源。
    expect(resolveGameResourceUrl(locator('', { gameId: '' }), 'art/tile.png')).toBeNull()
    expect(resolveGameResourceUrl(locator('', { resourceVersion: '' }), 'art/tile.png')).toBeNull()
  })
})

describe('资源清单地址（目录前缀 / 文件 URL 两种形状）', () => {
  const manifestBody = { version: '1.0.0', files: [{ path: 'cover.png' }, { path: 'art/tile.png' }] }

  beforeEach(() => { installLocalStorage() })

  it('目录前缀形状：清单为 {前缀}manifest.json，同目录资源按前缀拼接', async () => {
    const fetchMock = jsonFetch(manifestBody)
    const resolver = new GameResourceResolver({ fetchImpl: fetchMock })

    const manifest = await resolver.loadManifest(locator(CDN))

    expect(manifest).toEqual(manifestBody)
    expect(resolver.manifestUrl(locator(CDN))).toBe(`${CDN}manifest.json`)
    expect(fetchMock.mock.calls[0][0]).toBe(`${CDN}manifest.json`)
    expect(await resolver.resolveDeclaredUrl(locator(CDN), 'cover.png')).toBe(`${CDN}cover.png`)
  })

  it('文件形状（resourceUrl 就是 manifest.json）：直接用它作为清单，不再重复拼接', async () => {
    const fileUrl = `${CDN}manifest.json`
    const fetchMock = jsonFetch(manifestBody)
    const resolver = new GameResourceResolver({ fetchImpl: fetchMock })

    const manifest = await resolver.loadManifest(locator(fileUrl))

    expect(manifest).toEqual(manifestBody)
    expect(resolver.manifestUrl(locator(fileUrl))).toBe(fileUrl)
    expect(fetchMock.mock.calls[0][0]).toBe(fileUrl)
    expect(fetchMock.mock.calls[0][0]).not.toContain('manifest.json/manifest.json')
  })

  it('文件形状下 cover.png 解析为同目录地址（不得把 manifest.json 当目录）', async () => {
    const fileUrl = `${CDN}manifest.json`
    const resolver = new GameResourceResolver({ fetchImpl: jsonFetch(manifestBody) })

    const cover = await resolver.resolveDeclaredUrl(locator(fileUrl), 'cover.png')

    expect(cover).toBe(`${CDN}cover.png`)
    expect(cover).not.toContain('manifest.json')
    // 清单声明的其它资源同样落在同目录。
    expect(await resolver.resolveDeclaredUrl(locator(fileUrl), 'art/tile.png')).toBe(`${CDN}art/tile.png`)
  })

  it('边界：尾斜杠缺失、多重斜杠、查询串都不产生错误地址', async () => {
    const resolver = new GameResourceResolver({ fetchImpl: jsonFetch(manifestBody) })

    // 尾斜杠缺失（显式指向目录内某个文件）→ 仍取到同目录清单。
    expect(resolver.manifestUrl(locator(`${CDN}cover.png`))).toBe(`${CDN}manifest.json`)
    expect(await resolver.resolveDeclaredUrl(locator(`${CDN}cover.png`), 'cover.png')).toBe(`${CDN}cover.png`)

    // 多重斜杠折叠为单斜杠。
    expect(resolver.manifestUrl(locator('https://cdn.example.com//games//kart-racing//1.0.0//'))).toBe(`${CDN}manifest.json`)

    // 文件形状带查询串：该文件本身就是清单（保留查询串），同目录资源不带它。
    expect(resolver.manifestUrl(locator(`${CDN}manifest.json?v=1`))).toBe(`${CDN}manifest.json?v=1`)
    expect(resolver.resolveUrl(locator(`${CDN}manifest.json?v=1`), 'cover.png')).toBe(`${CDN}cover.png`)

    // 目录形状带查询串：清单与同目录资源都保留该查询串，且不会插到路径中间。
    expect(resolver.manifestUrl(locator(`${CDN}?v=1`))).toBe(`${CDN}manifest.json?v=1`)
    expect(resolver.resolveUrl(locator(`${CDN}?v=1`), 'cover.png')).toBe(`${CDN}cover.png?v=1`)

    // 裸文件名（无目录）：可作清单地址，但无法推导同目录资源 → 回退内置。
    expect(resolver.manifestUrl(locator('manifest.json'))).toBe('manifest.json')
    expect(resolver.resolveUrl(locator('manifest.json'), 'cover.png')).toBeNull()
  })
})

describe('资源清单获取与缓存', () => {
  const manifestBody = {
    version: '1.0.0',
    files: [{ path: 'art/tile.png', sha256: 'abc', size: 123 }, { path: 'cover.png' }],
  }

  beforeEach(() => { installLocalStorage() })

  it('清单结构合法时返回解析结果，并请求 {前缀}manifest.json', async () => {
    const fetchMock = jsonFetch(manifestBody)
    const resolver = new GameResourceResolver({ fetchImpl: fetchMock })

    const manifest = await resolver.loadManifest(locator(CDN))

    expect(manifest).toEqual(manifestBody)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe(`${CDN}manifest.json`)
  })

  it('内存缓存命中时不再发起请求；并发调用共享同一次请求', async () => {
    const fetchMock = jsonFetch(manifestBody)
    const resolver = new GameResourceResolver({ fetchImpl: fetchMock })

    await resolver.loadManifest(locator(CDN))
    await resolver.loadManifest(locator(CDN))
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // 另一个实例 + 另一个资源版本（缓存键不同）：并发两次调用只发一次请求。
    const concurrent = new GameResourceResolver({ fetchImpl: fetchMock })
    const next = locator(CDN, { resourceVersion: '1.1.0' })
    await Promise.all([concurrent.loadManifest(next), concurrent.loadManifest(next)])
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('localStorage 缓存命中时新实例也不发请求；键含 gameId 与 resourceVersion', async () => {
    const fetchMock = jsonFetch(manifestBody)
    await new GameResourceResolver({ fetchImpl: fetchMock }).loadManifest(locator(CDN))
    expect(fetchMock).toHaveBeenCalledTimes(1)

    const key = gameResourceManifestCacheKey('kart-racing', '1.0.0')
    expect(key).toBe(`${GAME_RESOURCE_CACHE_PREFIX}kart-racing@1.0.0`)
    expect(localStorage.getItem(key)).not.toBeNull()

    const secondFetch = jsonFetch(manifestBody)
    const manifest = await new GameResourceResolver({ fetchImpl: secondFetch }).loadManifest(locator(CDN))

    expect(manifest).toEqual(manifestBody)
    expect(secondFetch).not.toHaveBeenCalled()
  })

  it('清单获取失败（404 / 网络异常 / 非法 JSON）不抛错，一律降级为无清单', async () => {
    const notFound = jsonFetch('', 404)
    await expect(new GameResourceResolver({ fetchImpl: notFound }).loadManifest(locator(CDN))).resolves.toBeNull()

    const broken = vi.fn(async () => { throw new TypeError('fetch failed') })
    await expect(new GameResourceResolver({ fetchImpl: broken }).loadManifest(locator(CDN))).resolves.toBeNull()

    const invalidJson = new GameResourceResolver({ fetchImpl: jsonFetch('{ 不是合法 JSON') })
    await expect(invalidJson.loadManifest(locator(CDN))).resolves.toBeNull()
  })

  it('清单结构非法时降级为无清单；坏文件条目被过滤而保留合法条目', async () => {
    // 各场景用不同资源版本，避免上一个场景的「无清单」负缓存命中。
    const missingFiles = new GameResourceResolver({ fetchImpl: jsonFetch({ version: '1.0.0' }) })
    expect(await missingFiles.loadManifest(locator(CDN))).toBeNull()

    const notObject = new GameResourceResolver({
      fetchImpl: jsonFetch([1, 2, 3]),
    })
    expect(await notObject.loadManifest(locator(CDN, { resourceVersion: '1.0.1' }))).toBeNull()

    const partial = new GameResourceResolver({
      fetchImpl: jsonFetch({ version: '1.0.0', files: [{ path: '' }, 'x', { path: '/art/tile.png', size: -1 }, { path: 'art/ok.png', size: 8 }] }),
    })
    const manifest = await partial.loadManifest(locator(CDN, { resourceVersion: '1.0.2' }))
    expect(manifest?.files).toEqual([{ path: 'art/tile.png' }, { path: 'art/ok.png', size: 8 }])
  })

  it('无 CDN 前缀时不发任何请求（resourceUrl 为空即内置资源）', async () => {
    const fetchMock = jsonFetch(manifestBody)
    const resolver = new GameResourceResolver({ fetchImpl: fetchMock })

    expect(await resolver.loadManifest(locator(''))).toBeNull()
    expect(await resolver.lookupFile(locator(''), 'art/tile.png')).toBeNull()
    expect(await resolver.resolveDeclaredUrl(locator(''), 'art/tile.png')).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('TTL 到期后重新请求；未到期沿用缓存', async () => {
    let now = 1_000_000
    const fetchMock = jsonFetch(manifestBody)
    const resolver = new GameResourceResolver({ fetchImpl: fetchMock, now: () => now, ttlMs: 1000 })

    await resolver.loadManifest(locator(CDN))
    now += 999
    await resolver.loadManifest(locator(CDN))
    expect(fetchMock).toHaveBeenCalledTimes(1)

    now += 2
    await resolver.loadManifest(locator(CDN))
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('失效的 localStorage 缓存（超时或结构非法）被忽略并重新请求', async () => {
    const fetchMock = jsonFetch(manifestBody)
    const resolver = new GameResourceResolver({ fetchImpl: fetchMock })
    const key = gameResourceManifestCacheKey('kart-racing', '1.0.0')

    localStorage.setItem(key, JSON.stringify({ cachedAt: Date.now() - 10 * 60 * 60 * 1000, manifest: manifestBody }))
    await resolver.loadManifest(locator(CDN))
    expect(fetchMock).toHaveBeenCalledTimes(1)

    localStorage.setItem(key, JSON.stringify({ cachedAt: Date.now(), manifest: { version: '1.0.0' } }))
    await new GameResourceResolver({ fetchImpl: fetchMock }).loadManifest(locator(CDN))
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('缓存失效与清理入口可用（invalidate / clearCache 清内存与 localStorage）', async () => {
    const fetchMock = jsonFetch(manifestBody)
    const resolver = new GameResourceResolver({ fetchImpl: fetchMock })
    const key = gameResourceManifestCacheKey('kart-racing', '1.0.0')

    await resolver.loadManifest(locator(CDN))
    resolver.invalidate(locator(CDN))
    expect(localStorage.getItem(key)).toBeNull()
    await resolver.loadManifest(locator(CDN))
    expect(fetchMock).toHaveBeenCalledTimes(2)

    await resolver.loadManifest(locator(CDN, { resourceVersion: '1.1.0' }))
    resolver.clearCache()
    expect(localStorage.length).toBe(0)
    await resolver.loadManifest(locator(CDN))
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })

  it('resolveDeclaredUrl 只在清单登记该路径时给出 CDN 地址，未声明回退内置资源', async () => {
    const fetchMock = jsonFetch(manifestBody)
    const resolver = new GameResourceResolver({ fetchImpl: fetchMock })

    expect(await resolver.resolveDeclaredUrl(locator(CDN), 'cover.png')).toBe(`${CDN}cover.png`)
    expect(await resolver.resolveDeclaredUrl(locator(CDN), 'art/missing.png')).toBeNull()

    const noManifest = new GameResourceResolver({ fetchImpl: jsonFetch('', 404) })
    expect(await noManifest.resolveDeclaredUrl(locator(CDN, { resourceVersion: '1.0.1' }), 'cover.png')).toBeNull()
  })
})

describe('资源清单解析（纯函数）', () => {
  it('findManifestFile 按规范化路径查找', () => {
    const manifest = parseGameResourceManifest({ version: '1.0.0', files: [{ path: 'art/tile.png' }] })

    expect(findManifestFile(manifest, '/art/tile.png')?.path).toBe('art/tile.png')
    expect(findManifestFile(manifest, 'art/./tile.png')?.path).toBe('art/tile.png')
    expect(findManifestFile(manifest, 'art/other.png')).toBeNull()
    expect(findManifestFile(null, 'art/tile.png')).toBeNull()
    expect(findManifestFile(manifest, '../art/tile.png')).toBeNull()
  })
})
