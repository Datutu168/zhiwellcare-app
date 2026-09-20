import type { ISensorTransport } from '../../core/sensor/ISensorTransport'
import type { SensorConnectionState, SensorDataPacket, SensorDevice } from '../../core/sensor/SensorDevice'
import { SensorTransportError } from '../../core/sensor/SensorTransportError'

const WEB_BLE_MESSAGE = '当前浏览器环境不支持训练设备 BLE，请在桌面端应用（Windows / macOS）或 Android 应用中操作。'

/** 普通浏览器只承担界面预览，所有真实设备入口都返回可读提示。 */
export class WebUnsupportedSensorTransport implements ISensorTransport {
  scan(): Promise<SensorDevice[]> { return Promise.reject(this.unsupported()) }
  connect(): Promise<void> { return Promise.reject(this.unsupported()) }
  write(): Promise<void> { return Promise.reject(this.unsupported()) }
  async disconnect(): Promise<void> { /* 浏览器没有原生连接需要释放。 */ }
  onData(_callback: (packet: SensorDataPacket) => void): () => void { return () => undefined }
  onStateChanged(_callback: (state: SensorConnectionState) => void): () => void { return () => undefined }
  async dispose(): Promise<void> { /* 无平台监听需要清理。 */ }

  private unsupported(): SensorTransportError {
    return new SensorTransportError('unsupported', WEB_BLE_MESSAGE)
  }
}
