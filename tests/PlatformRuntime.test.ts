import { beforeEach, describe, expect, it, vi } from 'vitest'

// Capacitor 的平台判断由原生外壳注入，测试里直接控制它；@tauri-apps/api 在 jsdom 下恒为 false。
const runtime = vi.hoisted(() => ({ native: false, platform: 'web' }))

vi.mock('@capacitor/core', () => ({
  Capacitor: {
    isNativePlatform: () => runtime.native,
    getPlatform: () => runtime.platform,
  },
  SystemBars: {
    hide: vi.fn(async () => undefined),
    show: vi.fn(async () => undefined),
  },
  // Android APK 自更新插件用 registerPlugin 注册；本文件只关心"选了哪个 Provider"，
  // 因此桥接对象给最小实现即可（真正调用它的是 AndroidUpdateProvider.test.ts）。
  registerPlugin: () => ({
    getCurrentVersion: async () => ({ packageName: '', versionName: '', versionCode: 0 }),
    addListener: async () => ({ remove: async () => undefined }),
  }),
}))

vi.mock('@tauri-apps/api/core', () => ({ isTauri: () => false }))

import { isAndroidNativeRuntime, isCapacitorNativeRuntime, isIosNativeRuntime, isTauriRuntime } from '../src/platform/PlatformRuntime'
import { createSensorTransport } from '../src/platform/PlatformSensorTransport'
import { createDisplayService } from '../src/platform/PlatformDisplayService'
import { createAppLifecycleService } from '../src/platform/PlatformAppLifecycleService'
import { createBackButtonService } from '../src/platform/PlatformBackButtonService'
import { createUpdateProvider } from '../src/platform/update/createUpdateProvider'
import { CapacitorBleTransport } from '../src/platform/capacitor/CapacitorBleTransport'
import { WebUnsupportedSensorTransport } from '../src/platform/web/WebUnsupportedSensorTransport'
import { CapacitorDisplayService } from '../src/platform/capacitor/CapacitorDisplayService'
import { NoopDisplayService } from '../src/platform/display/NoopDisplayService'
import { CapacitorAppLifecycleService } from '../src/platform/capacitor/CapacitorAppLifecycleService'
import { NoopAppLifecycleService } from '../src/platform/lifecycle/NoopAppLifecycleService'
import { CapacitorBackButtonService } from '../src/platform/capacitor/CapacitorBackButtonService'
import { NoopBackButtonService } from '../src/platform/back/NoopBackButtonService'
import { AndroidUpdateProvider } from '../src/platform/capacitor/AndroidUpdateProvider'
import { UnsupportedUpdateProvider } from '../src/platform/web/UnsupportedUpdateProvider'

/** 切到指定运行环境（web / android / ios）。 */
function setRuntime(native: boolean, platform: string): void {
  runtime.native = native
  runtime.platform = platform
}

beforeEach(() => setRuntime(false, 'web'))

describe('平台判断', () => {
  it('浏览器环境：所有原生判断都为 false', () => {
    setRuntime(false, 'web')
    expect(isTauriRuntime()).toBe(false)
    expect(isCapacitorNativeRuntime()).toBe(false)
    expect(isAndroidNativeRuntime()).toBe(false)
    expect(isIosNativeRuntime()).toBe(false)
  })

  it('Android 原生：Capacitor 原生成立，iOS 判断不成立', () => {
    setRuntime(true, 'android')
    expect(isCapacitorNativeRuntime()).toBe(true)
    expect(isAndroidNativeRuntime()).toBe(true)
    expect(isIosNativeRuntime()).toBe(false)
  })

  it('iOS 原生：Capacitor 原生成立，Android 专属判断不成立', () => {
    setRuntime(true, 'ios')
    expect(isCapacitorNativeRuntime()).toBe(true)
    expect(isAndroidNativeRuntime()).toBe(false)
    expect(isIosNativeRuntime()).toBe(true)
  })

  it('Capacitor 的 web 平台（isNativePlatform=false）不算原生', () => {
    setRuntime(false, 'android')
    expect(isCapacitorNativeRuntime()).toBe(false)
    expect(isAndroidNativeRuntime()).toBe(false)
  })
})

describe('按平台选择实现', () => {
  it('浏览器：BLE 走兜底提示，横屏/常亮、生命周期、返回键全为 Noop，更新走 Unsupported', () => {
    setRuntime(false, 'web')
    expect(createSensorTransport()).toBeInstanceOf(WebUnsupportedSensorTransport)
    expect(createDisplayService()).toBeInstanceOf(NoopDisplayService)
    expect(createAppLifecycleService()).toBeInstanceOf(NoopAppLifecycleService)
    expect(createBackButtonService()).toBeInstanceOf(NoopBackButtonService)
    expect(createUpdateProvider()).toBeInstanceOf(UnsupportedUpdateProvider)
  })

  it('Android：BLE/横屏常亮/生命周期/返回键都走 Capacitor，更新走 APK 自更新', () => {
    setRuntime(true, 'android')
    expect(createSensorTransport()).toBeInstanceOf(CapacitorBleTransport)
    expect(createDisplayService()).toBeInstanceOf(CapacitorDisplayService)
    expect(createAppLifecycleService()).toBeInstanceOf(CapacitorAppLifecycleService)
    expect(createBackButtonService()).toBeInstanceOf(CapacitorBackButtonService)
    expect(createUpdateProvider()).toBeInstanceOf(AndroidUpdateProvider)
  })

  it('iOS：BLE 与横屏常亮/生命周期同样走 Capacitor（此前会静默降级成"浏览器不支持 BLE"）', () => {
    setRuntime(true, 'ios')
    expect(createSensorTransport()).toBeInstanceOf(CapacitorBleTransport)
    expect(createSensorTransport()).not.toBeInstanceOf(WebUnsupportedSensorTransport)
    expect(createDisplayService()).toBeInstanceOf(CapacitorDisplayService)
    expect(createAppLifecycleService()).toBeInstanceOf(CapacitorAppLifecycleService)
  })

  it('iOS：返回键保持 Noop（backButton 事件只有 Android 会触发）', () => {
    setRuntime(true, 'ios')
    expect(createBackButtonService()).toBeInstanceOf(NoopBackButtonService)
    expect(createBackButtonService()).not.toBeInstanceOf(CapacitorBackButtonService)
  })

  it('iOS：不使用 Android 的 APK 自更新（App Store 也不允许应用自更新）', () => {
    setRuntime(true, 'ios')
    expect(createUpdateProvider()).toBeInstanceOf(UnsupportedUpdateProvider)
    expect(createUpdateProvider()).not.toBeInstanceOf(AndroidUpdateProvider)
  })
})

describe('无更新通道时的提示文案', () => {
  it('不再写死"Windows 或 Android"，iOS 与 macOS 用户也能看懂', async () => {
    setRuntime(true, 'ios')
    const provider = createUpdateProvider()
    await expect(provider.checkForUpdate()).rejects.toThrow('请通过应用商店或官网下载最新版本')
    await expect(provider.checkForUpdate()).rejects.not.toThrow('Windows')
  })
})
