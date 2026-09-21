import type { ISensorTransport } from '../core/sensor/ISensorTransport'
import { CapacitorBleTransport } from './capacitor/CapacitorBleTransport'
import { TauriBleTransport } from './tauri/TauriBleTransport'
import { WebUnsupportedSensorTransport } from './web/WebUnsupportedSensorTransport'
import { isCapacitorNativeRuntime, isTauriRuntime } from './PlatformRuntime'

/**
 * 运行时选择唯一 Transport，业务服务不需要知道当前原生外壳。
 *
 * 选择顺序：Tauri（桌面）→ Capacitor 原生（Android / iOS，同一套 bluetooth-le 插件）→ 浏览器兜底。
 * 浏览器没有 BLE 权限，因此兜底实现只返回可读提示。
 */
export function createSensorTransport(): ISensorTransport {
  if (isTauriRuntime()) return new TauriBleTransport()
  if (isCapacitorNativeRuntime()) return new CapacitorBleTransport()
  return new WebUnsupportedSensorTransport()
}
