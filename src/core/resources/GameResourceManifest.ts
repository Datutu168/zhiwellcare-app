import { normalizeResourceRelativePath } from './GameResourcePath'
import type { GameResourceManifest, GameResourceManifestFile } from './GameResourceTypes'

/**
 * 资源清单解析：CDN 前缀下的 manifest.json 结构为
 * `{"version":"1.0.0","files":[{"path":"art/tile.png","sha256":"...","size":123}]}`。
 * 清单是「可选」的：获取失败或结构非法时返回 null（降级为无清单），调用方据此回退内置资源。
 */
export function parseGameResourceManifest(raw: unknown): GameResourceManifest | null {
  if (!isRecord(raw)) return null
  // 缺少 files 数组即视为结构非法：清单的用途就是声明资源文件。
  if (!Array.isArray(raw.files)) return null
  const version = typeof raw.version === 'string' ? raw.version.trim() : ''
  const files: GameResourceManifestFile[] = []
  for (const item of raw.files) {
    const file = parseManifestFile(item)
    if (file) files.push(file)
  }
  return { version, files }
}

/** 在清单中查找某个相对路径（路径按统一规则规范化后比较）；未登记返回 null。 */
export function findManifestFile(
  manifest: GameResourceManifest | null,
  relativePath: string,
): GameResourceManifestFile | null {
  const normalized = normalizeResourceRelativePath(relativePath)
  if (!manifest || !normalized) return null
  return manifest.files.find((file) => file.path === normalized) ?? null
}

/** 单条文件声明：path 必须为非空字符串，sha256/size 非法时忽略（不因一条坏数据丢弃整份清单）。 */
function parseManifestFile(raw: unknown): GameResourceManifestFile | null {
  if (!isRecord(raw)) return null
  const path = normalizeResourceRelativePath(typeof raw.path === 'string' ? raw.path : '')
  if (!path) return null
  const file: GameResourceManifestFile = { path }
  if (typeof raw.sha256 === 'string' && raw.sha256.trim()) file.sha256 = raw.sha256.trim()
  if (typeof raw.size === 'number' && Number.isFinite(raw.size) && raw.size >= 0) file.size = raw.size
  return file
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
