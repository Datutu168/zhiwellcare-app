import {
  BleClient,
  type BleClientInterface,
  type BleService,
  type ScanResult,
} from '@capacitor-community/bluetooth-le'
import type { ISensorTransport } from '../../core/sensor/ISensorTransport'
import type { SensorConnectionState, SensorDataPacket, SensorDevice } from '../../core/sensor/SensorDevice'
import { SensorTransportError } from '../../core/sensor/SensorTransportError'

const SERVICE_UUID = '0000ffe5-0000-1000-8000-00805f9a34fb'
const NOTIFY_UUID = '0000ffe4-0000-1000-8000-00805f9a34fb'
const WRITE_UUID = '0000ffe9-0000-1000-8000-00805f9a34fb'
const SCAN_DURATION_MS = 3_000

type DelayFunction = (milliseconds: number) => Promise<void>
type Clock = () => number

/**
 * Capacitor 原生 BLE（Android / iOS 共用）：协议解析和业务重连继续复用 Core。
 *
 * initialize 里的 `androidNeverForLocation` 是 Android 专属开关（Android 12+ 用它避免申请定位权限），
 * iOS 原生实现不读该字段、传了也无副作用，所以不必按平台分支。
 */
export class CapacitorBleTransport implements ISensorTransport {
  private initialized: Promise<void> | null = null
  private dataCallbacks = new Set<(packet: SensorDataPacket) => void>()
  private stateCallbacks = new Set<(state: SensorConnectionState) => void>()
  private connectedDeviceId: string | null = null
  private writeWithoutResponse = false
  private scanning = false
  private notificationsStarted = false
  private enabledNotificationsStarted = false
  private currentState: SensorConnectionState = 'idle'
  private intentionalDisconnect = false

  constructor(
    private readonly client: BleClientInterface = BleClient,
    private readonly delay: DelayFunction = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)),
    // Android 数据包必须使用 Epoch 毫秒，才能与 ROM 页面和 Windows 时间戳直接比较。
    private readonly now: Clock = Date.now,
  ) {}

  async scan(): Promise<SensorDevice[]> {
    await this.ensureReady()
    const devices = new Map<string, SensorDevice>()
    const connectionKeptAlive = this.connectedDeviceId !== null
    // 更换设备时旧连接继续提供断线监测，因此不把全局状态改成 scanning。
    if (!connectionKeptAlive) this.emitState('scanning')
    this.scanning = true
    try {
      await this.client.requestLEScan({ allowDuplicates: false }, (result: ScanResult) => {
        const name = result.localName || result.device.name || 'Unknown BLE'
        if (!name.toUpperCase().includes('BS')) return
        devices.set(result.device.deviceId, { id: result.device.deviceId, name })
      })
      await this.delay(SCAN_DURATION_MS)
      return [...devices.values()]
    } catch (error) {
      // 更换设备扫描失败不代表旧设备断线，保留旧连接状态供训练安全暂停判断。
      if (!connectionKeptAlive) this.emitState('error')
      throw mapCapacitorBleError(error)
    } finally {
      if (this.scanning) await this.stopScanSafely()
      if (!connectionKeptAlive && this.currentState !== 'error') this.emitState('idle')
    }
  }

  async connect(deviceId: string): Promise<void> {
    await this.ensureReady()
    if (this.connectedDeviceId && this.connectedDeviceId !== deviceId) await this.disconnect()
    this.emitState('connecting')
    try {
      await this.client.connect(deviceId, () => this.handleNativeDisconnect(deviceId))
      this.connectedDeviceId = deviceId
      this.emitState('discovering')
      const services = await this.client.getServices(deviceId)
      const characteristics = findBsBt91Characteristics(services)
      this.writeWithoutResponse = characteristics.write.properties.writeWithoutResponse
      this.emitState('subscribing')
      await this.client.startNotifications(deviceId, SERVICE_UUID, NOTIFY_UUID, (value) => this.publishData(value))
      this.notificationsStarted = true
      this.emitState('connected')
    } catch (error) {
      // Native 已建立但特征验证失败时也要断开，避免遗留半连接设备。
      await this.cleanupConnection(true)
      this.emitState('error')
      throw mapCapacitorBleError(error)
    }
  }

  async disconnect(): Promise<void> {
    this.intentionalDisconnect = true
    try {
      await this.cleanupConnection(true)
    } finally {
      this.intentionalDisconnect = false
      this.emitState('disconnected')
    }
  }

  async write(data: Uint8Array): Promise<void> {
    const deviceId = this.connectedDeviceId
    if (!deviceId) throw new SensorTransportError('operation-failed', 'BS-BT91 尚未连接。')
    const copy = Uint8Array.from(data)
    const value = new DataView(copy.buffer)
    try {
      if (this.writeWithoutResponse) await this.client.writeWithoutResponse(deviceId, SERVICE_UUID, WRITE_UUID, value)
      else await this.client.write(deviceId, SERVICE_UUID, WRITE_UUID, value)
    } catch (error) {
      // 单次寄存器读取失败不改变连接状态，由调用方决定是否再次读取。
      throw mapCapacitorBleError(error)
    }
  }

  onData(callback: (packet: SensorDataPacket) => void): () => void {
    this.dataCallbacks.add(callback)
    return () => this.dataCallbacks.delete(callback)
  }

  onStateChanged(callback: (state: SensorConnectionState) => void): () => void {
    this.stateCallbacks.add(callback)
    return () => this.stateCallbacks.delete(callback)
  }

  async dispose(): Promise<void> {
    if (this.scanning) await this.stopScanSafely()
    await this.cleanupConnection(true)
    if (this.enabledNotificationsStarted) {
      try { await this.client.stopEnabledNotifications() } catch { /* Native 已销毁时无需继续抛错。 */ }
    }
    this.enabledNotificationsStarted = false
    this.dataCallbacks.clear()
    this.stateCallbacks.clear()
  }

  private async ensureReady(): Promise<void> {
    this.initialized ??= this.initializeClient()
    await this.initialized
    if (!await this.client.isEnabled()) {
      throw new SensorTransportError('bluetooth-disabled', '手机蓝牙未开启，请开启蓝牙后重试。')
    }
  }

  private async initializeClient(): Promise<void> {
    try {
      await this.client.initialize({ androidNeverForLocation: true })
      await this.client.startEnabledNotifications((enabled) => {
        if (!enabled && this.connectedDeviceId) this.handleNativeDisconnect(this.connectedDeviceId)
      })
      this.enabledNotificationsStarted = true
    } catch (error) {
      this.initialized = null
      throw mapCapacitorBleError(error)
    }
  }

  private publishData(value: DataView): void {
    // Native DataView 可能复用底层缓冲区，进入 Core 前必须复制成独立数据包。
    const source = new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
    const packet: SensorDataPacket = { data: Uint8Array.from(source), timestamp: this.now() }
    for (const callback of this.dataCallbacks) callback(packet)
  }

  private handleNativeDisconnect(deviceId: string): void {
    if (deviceId !== this.connectedDeviceId || this.intentionalDisconnect) return
    this.connectedDeviceId = null
    this.notificationsStarted = false
    this.writeWithoutResponse = false
    this.emitState('disconnected')
  }

  private async cleanupConnection(disconnectNative: boolean): Promise<void> {
    const deviceId = this.connectedDeviceId
    this.connectedDeviceId = null
    if (!deviceId) return
    if (this.notificationsStarted) {
      try { await this.client.stopNotifications(deviceId, SERVICE_UUID, NOTIFY_UUID) } catch { /* 断线后通知可能已由系统移除。 */ }
    }
    this.notificationsStarted = false
    this.writeWithoutResponse = false
    if (disconnectNative) {
      try { await this.client.disconnect(deviceId) } catch { /* 设备已断开时按清理成功处理。 */ }
    }
  }

  private async stopScanSafely(): Promise<void> {
    try { await this.client.stopLEScan() } catch { /* 停止已结束的扫描无需阻塞后续操作。 */ }
    this.scanning = false
  }

  private emitState(state: SensorConnectionState): void {
    if (this.currentState === state) return
    this.currentState = state
    for (const callback of this.stateCallbacks) callback(state)
  }
}

/** 连接前验证 BS-BT91 的三个关键特征，避免把不兼容设备报告为已连接。 */
function findBsBt91Characteristics(services: BleService[]) {
  const service = services.find((item) => item.uuid.toLowerCase() === SERVICE_UUID)
  if (!service) throw new SensorTransportError('operation-failed', '目标设备不存在 FFE5 Service，可能不是兼容的 BS-BT91。')
  const notify = service.characteristics.find((item) => item.uuid.toLowerCase() === NOTIFY_UUID && (item.properties.notify || item.properties.indicate))
  if (!notify) throw new SensorTransportError('operation-failed', '未找到 FFE4 Notify Characteristic。')
  const write = service.characteristics.find((item) => item.uuid.toLowerCase() === WRITE_UUID && (item.properties.writeWithoutResponse || item.properties.write))
  if (!write) throw new SensorTransportError('operation-failed', '未找到 FFE9 Write Characteristic。')
  return { notify, write }
}

/** 将插件的英文/平台错误归一为页面可直接展示的中文错误。 */
export function mapCapacitorBleError(error: unknown): SensorTransportError {
  if (error instanceof SensorTransportError) return error
  const source = error instanceof Error ? `${error.name} ${error.message}` : String(error)
  const normalized = source.toLowerCase()
  if (normalized.includes('never ask again') || normalized.includes('permanent')) {
    return new SensorTransportError('permission-permanently-denied', '蓝牙权限已被永久拒绝，请前往系统设置为 ZhiWellCare 开启附近设备权限。', { cause: error })
  }
  if (normalized.includes('permission') || normalized.includes('denied') || normalized.includes('not authorized')) {
    return new SensorTransportError('permission-denied', '未获得蓝牙权限，请允许 ZhiWellCare 使用附近设备后重试。', { cause: error })
  }
  if (normalized.includes('disabled') || normalized.includes('powered off') || normalized.includes('not enabled')) {
    return new SensorTransportError('bluetooth-disabled', '手机蓝牙未开启，请开启蓝牙后重试。', { cause: error })
  }
  return new SensorTransportError('operation-failed', error instanceof Error ? error.message : String(error), { cause: error })
}
