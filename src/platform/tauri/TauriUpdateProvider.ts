import { getVersion } from '@tauri-apps/api/app'
import { check, type DownloadEvent, type Update } from '@tauri-apps/plugin-updater'
import type { IUpdateProvider, UpdateInstallPermission } from '../../core/update/IUpdateProvider'
import type { UpdateInfo, UpdateProgress } from '../../core/update/UpdateInfo'

/** Tauri Provider 保留 Update 资源，使下载和安全时机安装可以分开执行。 */
export class TauriUpdateProvider implements IUpdateProvider {
  readonly platform = 'tauri' as const
  readonly supported = true
  private update: Update | null = null

  constructor(
    private readonly checker: typeof check = check,
    private readonly versionReader: typeof getVersion = getVersion,
  ) {}

  getCurrentVersion(): Promise<string> {
    return this.versionReader()
  }

  async checkForUpdate(): Promise<UpdateInfo | null> {
    await this.closeCurrentUpdate()
    this.update = await this.checker({ timeout: 15_000 })
    if (!this.update) return null
    return {
      currentVersion: this.update.currentVersion,
      latestVersion: this.update.version,
      displayVersion: this.update.version,
      notes: splitNotes(this.update.body),
      publishedAt: this.update.date ?? null,
      sizeBytes: null,
    }
  }

  async download(_update: UpdateInfo, onProgress?: (progress: UpdateProgress) => void): Promise<void> {
    if (!this.update) throw new Error('Tauri 更新资源尚未准备。')
    let downloadedBytes = 0
    let totalBytes: number | null = null
    await this.update.download((event: DownloadEvent) => {
      if (event.event === 'Started') totalBytes = finiteOrNull(event.data.contentLength)
      else if (event.event === 'Progress') downloadedBytes += event.data.chunkLength
      onProgress?.({
        downloadedBytes,
        totalBytes,
        percent: totalBytes && totalBytes > 0 ? Math.min(100, Math.round(downloadedBytes / totalBytes * 100)) : null,
      })
    }, { timeout: 120_000 })
  }

  async install(): Promise<void> {
    if (!this.update) throw new Error('请先下载更新包。')
    // 桌面端安装：Windows 由安装器接管并退出当前进程（不额外调用 relaunch）；
    // macOS 要求应用已签名才能自更新，未签名版本会在这一步失败——请改用手动安装新 .dmg。
    await this.update.install()
  }

  async getInstallPermission(): Promise<UpdateInstallPermission> { return 'not-required' }
  async openInstallPermissionSettings(): Promise<void> {}
  async dispose(): Promise<void> { await this.closeCurrentUpdate() }

  private async closeCurrentUpdate(): Promise<void> {
    const update = this.update
    this.update = null
    if (update) await update.close()
  }
}

function splitNotes(value: string | undefined): string[] {
  return value?.split(/\r?\n/).map((line) => line.trim()).filter(Boolean) ?? []
}

function finiteOrNull(value: number | undefined): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
}
