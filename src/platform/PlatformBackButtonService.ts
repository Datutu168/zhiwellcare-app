import type { IBackButtonService } from './back/IBackButtonService'
import { NoopBackButtonService } from './back/NoopBackButtonService'
import { CapacitorBackButtonService } from './capacitor/CapacitorBackButtonService'
import { isAndroidNativeRuntime } from './PlatformRuntime'

/**
 * 只有 Android 使用 Native Back。
 *
 * 刻意不放进 isCapacitorNativeRuntime：@capacitor/app 的 backButton 事件仅 Android 会触发，
 * iOS 没有硬件返回键（边缘手势由系统接管），桌面与浏览器也各自处理。
 */
export function createBackButtonService(): IBackButtonService {
  return isAndroidNativeRuntime() ? new CapacitorBackButtonService() : new NoopBackButtonService()
}
