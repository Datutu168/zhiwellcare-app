import { request } from '../../api/http'

/** 固件接口应答（统一信封内的 data）。 */
export interface FirmwareInfo {
  /** 是否有可用更新；true 时 version 为当前最新版本。 */
  upToDate: boolean
  version: string
  url?: string
  sha256?: string
  size?: number
  notes?: string[]
  publishedAt?: string
}

/** 固件检查结果：只读展示用。任何失败都收敛到 error 字段，不抛到 UI。 */
export interface FirmwareCheckResult {
  /** 是否有可用更新（检查失败时为 false）。 */
  available: boolean
  /** 可用版本号或后端返回的最新版本号。 */
  version?: string
  /** 固件包地址（仅展示，本模块不执行任何刷写）。 */
  url?: string
  /** 版本说明列表。 */
  notes?: string[]
  size?: number
  /** 失败原因：网络异常 / 接口错误 / 未连接设备。 */
  error?: string
}

/**
 * 固件更新检查（只读）：GET {base}/api/v1/device/{modelId}/firmware?currentVersion=…
 *
 * 公开接口（无需登录；复用 src/api/http.ts 的统一信封解析，已登录时会顺带带上令牌，服务端不校验）。
 * 检查需要知道当前机型 id：取不到时直接返回「未连接设备」，不发起请求。
 * 本模块只查询与展示，**不实现任何固件刷写**（消费版固件 OTA 由后端白名单管控）。
 */
export class FirmwareUpdateService {
  /** 检查固件更新；网络失败、非 0 业务码、结构异常都返回带 error 的结果，绝不抛错。 */
  async checkFirmware(modelId: string, currentVersion: string): Promise<FirmwareCheckResult> {
    const id = modelId.trim()
    if (!id) return { available: false, error: '未连接设备' }

    const version = currentVersion.trim()
    const query = version ? `?currentVersion=${encodeURIComponent(version)}` : ''
    try {
      const data = await request<FirmwareInfo>(`/api/v1/device/${encodeURIComponent(id)}/firmware${query}`)
      if (!data || typeof data !== 'object' || typeof data.upToDate !== 'boolean') {
        return { available: false, error: '固件接口返回结构异常' }
      }
      const latest = typeof data.version === 'string' ? data.version : undefined
      if (data.upToDate) return { available: false, version: latest }
      return {
        available: true,
        version: latest,
        url: typeof data.url === 'string' ? data.url : undefined,
        size: typeof data.size === 'number' && Number.isFinite(data.size) ? data.size : undefined,
        notes: normalizeNotes(data.notes),
      }
    } catch (error) {
      return { available: false, error: error instanceof Error ? error.message : String(error) }
    }
  }
}

/** 说明列表归一化：只保留非空字符串，避免脏数据打乱设置页排版。 */
function normalizeNotes(notes: unknown): string[] {
  if (!Array.isArray(notes)) return []
  return notes.filter((item): item is string => typeof item === 'string' && item.trim() !== '')
}
