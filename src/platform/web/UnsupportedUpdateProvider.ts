import type { IUpdateProvider, UpdateInstallPermission } from '../../core/update/IUpdateProvider'
import type { UpdateInfo, UpdateProgress } from '../../core/update/UpdateInfo'
import releaseVersion from '../../../release-version.json'

// 这个 Provider 有三种使用场景：浏览器预览、iOS（App Store 不允许应用自更新）、以及后续
// 任何没有原生更新通道的宿主。文案不能写成「请在 Windows 或 Android 应用中操作」——
// 那样在 iOS 与 macOS 上都是答非所问。
const MESSAGE = '当前环境不支持应用内更新，请通过应用商店或官网下载最新版本。'

/** 浏览器保留设置预览能力，但不会伪装成已经是最新版本。 */
export class UnsupportedUpdateProvider implements IUpdateProvider {
  readonly platform = 'unsupported' as const
  readonly supported = false

  async getCurrentVersion(): Promise<string> { return releaseVersion.productVersion }
  async checkForUpdate(): Promise<UpdateInfo | null> { throw new Error(MESSAGE) }
  async download(_update: UpdateInfo, _onProgress?: (progress: UpdateProgress) => void): Promise<void> { throw new Error(MESSAGE) }
  async install(): Promise<void> { throw new Error(MESSAGE) }
  async getInstallPermission(): Promise<UpdateInstallPermission> { return 'unsupported' }
  async openInstallPermissionSettings(): Promise<void> { throw new Error(MESSAGE) }
  async dispose(): Promise<void> {}
}
