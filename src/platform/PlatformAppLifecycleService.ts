import { CapacitorAppLifecycleService } from './capacitor/CapacitorAppLifecycleService'
import { NoopAppLifecycleService } from './lifecycle/NoopAppLifecycleService'
import type { IAppLifecycleService } from './lifecycle/IAppLifecycleService'
import { isCapacitorNativeRuntime } from './PlatformRuntime'

/**
 * 页面只订阅统一生命周期：Capacitor 原生（Android / iOS）下接管前后台事件，
 * Web 与 Tauri 桌面不需要移动端行为（Tauri 的窗口生命周期由窗口系统处理）。
 */
export function createAppLifecycleService(): IAppLifecycleService {
  return isCapacitorNativeRuntime() ? new CapacitorAppLifecycleService() : new NoopAppLifecycleService()
}
