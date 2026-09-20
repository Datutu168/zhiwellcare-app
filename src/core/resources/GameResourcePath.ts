import { GAME_RESOURCE_MANIFEST_FILE } from './GameResourceTypes'

/**
 * 游戏资源路径规范化与定位（纯函数，无 IO）。
 *
 * 后端 resourceUrl 的真实形状（backend fillResourceAssets 回填的是「资产登记的那个对象文件」的 URL，
 * 对象键为 `games/{gameId}/{version}/{filename}`），因此本模块同时兼容两种形状：
 * - 以 `/` 结尾 → 视为资源目录前缀，直接作为 base；
 * - 否则视为**文件 URL**，取其**父目录**作为 base（`…/1.0.0/manifest.json` → base `…/1.0.0/`）。
 *
 * 安全约定：resourceUrl 与相对路径都可能来自后端目录数据，因此：
 * - 只接受 http(s) 绝对地址、根绝对路径或普通相对路径（拒绝协议无关地址与其它 scheme）；
 * - 相对路径禁止绝对 URL / 协议无关地址 / 任何 `..` 上跳（含 %2e 编码形式）。
 * 任一非法输入返回 null，调用方静默回退内置资源，绝不因为坏数据让游戏打不开。
 */

/** URL scheme 前缀，如 `https:`、`javascript:`、`data:`。 */
const SCHEME_PATTERN = /^[a-zA-Z][a-zA-Z0-9+.-]*:/

/** 资源定位结果：目录 base + 目录级查询串 + 清单地址。 */
export interface ResourceLocation {
  /** 资源目录 base（以 `/` 结尾，不含查询串）；只有裸文件名（无目录）时为 null。 */
  base: string | null
  /** 目录自带的查询串/片段（如 `?v=1`）：拼接资源时追加在文件名之后；无则为空串。 */
  suffix: string
  /** 清单地址：resourceUrl 本身即 manifest.json 时直接用它，不再重复拼接。 */
  manifestUrl: string | null
}

/**
 * 由 resourceUrl 推导资源目录与清单地址；非法输入返回 null（调用方回退内置资源）。
 * 结尾斜杠、多重斜杠、查询串都做了归一化，不会产生 `manifest.json/manifest.json`
 * 或 `manifest.json/cover.png` 这类把文件当目录的地址。
 */
export function resolveResourceLocation(resourceUrl: string): ResourceLocation | null {
  const normalized = normalizeResourceUrl(resourceUrl)
  if (!normalized) return null
  const { path, suffix } = splitResourceSuffix(normalized)
  if (!path) return null

  // 形状一：目录前缀（以 / 结尾）——直接用，清单与同目录资源都在它下面。
  if (path.endsWith('/')) {
    return { base: path, suffix, manifestUrl: `${path}${GAME_RESOURCE_MANIFEST_FILE}${suffix}` }
  }

  // 形状二：文件 URL——取父目录作为 base（同目录资源按父目录拼接）。
  const slashIndex = path.lastIndexOf('/')
  const lastSegment = path.slice(slashIndex + 1)
  const base = slashIndex >= 0 ? path.slice(0, slashIndex + 1) : null
  if (lastSegment.toLowerCase() === GAME_RESOURCE_MANIFEST_FILE) {
    // 该文件本身就是清单：直接用它（保留其查询串，兼容签名/版本参数）。
    return { base, suffix: '', manifestUrl: `${path}${suffix}` }
  }
  // 其它文件：清单在父目录下；该文件自带的查询串不外溢到同目录资源。
  return { base, suffix: '', manifestUrl: base ? `${base}${GAME_RESOURCE_MANIFEST_FILE}` : null }
}

/** 规范化资源目录前缀（用于全局兜底前缀 VITE_GAME_RESOURCE_CDN）：抹平结尾斜杠；非法或空返回 null。 */
export function normalizeResourceBase(base: string): string | null {
  const trimmed = base.trim().replace(/\\/g, '/')
  if (!trimmed) return null
  // 协议无关地址（//host/...）会把请求导向任意主机，直接拒绝。
  if (trimmed.startsWith('//')) return null
  const scheme = SCHEME_PATTERN.exec(trimmed)
  if (scheme && !/^https?:$/i.test(scheme[0])) return null
  if (hasParentSegment(trimmed)) return null
  const normalized = trimmed.replace(/\/+$/, '')
  return normalized || null
}

/** 规范化资源相对路径：统一分隔符、去掉前导 `/` 与 `.` 段；非法或空返回 null。 */
export function normalizeResourceRelativePath(relativePath: string): string | null {
  const trimmed = relativePath.trim().replace(/\\/g, '/')
  if (!trimmed) return null
  // 绝对 URL / 协议无关地址注入（https://evil.com/x.png、//evil.com/x.png）直接拒绝。
  if (trimmed.startsWith('//') || SCHEME_PATTERN.test(trimmed)) return null
  if (hasParentSegment(trimmed)) return null
  const segments = trimmed.split('/').filter((segment) => segment !== '' && segment !== '.')
  return segments.length ? segments.join('/') : null
}

/** 规范化资源地址：校验 scheme / `..`，折叠路径中的重复斜杠（保留查询串原样）。 */
function normalizeResourceUrl(resourceUrl: string): string | null {
  const trimmed = resourceUrl.trim().replace(/\\/g, '/')
  if (!trimmed) return null
  if (trimmed.startsWith('//')) return null
  const scheme = SCHEME_PATTERN.exec(trimmed)
  if (scheme && !/^https?:$/i.test(scheme[0])) return null

  const { path, suffix } = splitResourceSuffix(trimmed)
  if (!path || hasParentSegment(path)) return null
  // 只折叠路径部分的重复斜杠：scheme 后的 //（authority 分隔符）单独保留。
  const pathScheme = SCHEME_PATTERN.exec(path)
  const head = pathScheme ? pathScheme[0] : ''
  let rest = path.slice(head.length)
  const authority = head && rest.startsWith('//') ? '//' : ''
  if (authority) rest = rest.slice(2)
  rest = rest.replace(/\/{2,}/g, '/')
  return `${head}${authority}${rest}${suffix}`
}

/** 拆出查询串/片段：`…/manifest.json?v=1` → path `…/manifest.json`、suffix `?v=1`。 */
function splitResourceSuffix(url: string): { path: string; suffix: string } {
  const index = url.search(/[?#]/)
  return index < 0 ? { path: url, suffix: '' } : { path: url.slice(0, index), suffix: url.slice(index) }
}

/**
 * 是否包含上跳段 `..`；同时识别 %2e 编码形式，
 * 避免 `%2e%2e/secret` 这类绕过（仅解码点号，不影响合法文件名的其它百分号编码）。
 */
function hasParentSegment(path: string): boolean {
  const decoded = path.replace(/%2e/gi, '.')
  return decoded.split('/').some((segment) => segment === '..')
}
