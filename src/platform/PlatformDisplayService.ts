import type { IDisplayService } from './display/IDisplayService'
import { NoopDisplayService } from './display/NoopDisplayService'
import { CapacitorDisplayService } from './capacitor/CapacitorDisplayService'
import { isCapacitorNativeRuntime } from './PlatformRuntime'

/**
 * 训练期横屏 + 常亮 + 隐藏系统栏：Android 与 iOS 都需要（两个插件都提供 iOS 实现）。
 * CapacitorDisplayService 内部对每个原生调用单独容错（失败只返回 false，不中断训练），
 * 因此旧版 iOS 上 ScreenOrientation.lock 不受支持时也不会影响训练流程。
 */
export function createDisplayService(): IDisplayService {
  return isCapacitorNativeRuntime() ? new CapacitorDisplayService() : new NoopDisplayService()
}
