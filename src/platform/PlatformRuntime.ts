import { Capacitor } from '@capacitor/core'
import { isTauri } from '@tauri-apps/api/core'

/** 平台判断集中维护，避免页面各自猜测当前原生外壳。 */
export function isTauriRuntime(): boolean {
  return typeof window !== 'undefined' && isTauri()
}

/**
 * Capacitor 原生外壳（Android / iOS 手机与平板）。
 *
 * 与平台无关的能力（BLE、横屏与常亮、前后台生命周期）都用这一个判断：
 * 这些插件的 JS API 在两端一致，差异由插件自己的原生实现吸收。
 */
export function isCapacitorNativeRuntime(): boolean {
  if (!Capacitor.isNativePlatform()) return false
  const platform = Capacitor.getPlatform()
  return platform === 'android' || platform === 'ios'
}

/** 仅 Android：返回键、APK 自更新等 Android 专属能力才用它。 */
export function isAndroidNativeRuntime(): boolean {
  return Capacitor.isNativePlatform() && Capacitor.getPlatform() === 'android'
}

/** 仅 iOS：目前没有 iOS 专属分支，保留该判断供后续按平台分支时使用。 */
export function isIosNativeRuntime(): boolean {
  return Capacitor.isNativePlatform() && Capacitor.getPlatform() === 'ios'
}
