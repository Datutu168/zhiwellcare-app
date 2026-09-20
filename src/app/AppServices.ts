import { ref } from 'vue'
import { LocalStorageMotionProfileRepository } from '../core/motion/LocalStorageMotionProfileRepository'
import { MotionProfileService } from '../core/motion/MotionProfileService'
import type { MotionProfile } from '../core/motion/MotionProfile'
import { LocalStorageDeviceBindingRepository } from '../core/sensor/DeviceBinding'
import { SensorConnectionManager } from '../core/sensor/SensorConnectionManager'
import { SensorService } from '../core/sensor/SensorService'
import type { SensorDevice } from '../core/sensor/SensorDevice'
import { LocalStorageStore } from '../core/storage/LocalStorageStore'
import { IndexedDbTrainingRepository } from '../core/training/IndexedDbTrainingRepository'
import type { TrainingRecord } from '../core/training/TrainingRecord'
import type { TrainingReplay } from '../core/replay/TrainingReplay'
import type { BaseTrainingResult } from '../core/training/BaseTrainingResult'
import type { GameDefinition } from '../core/game/GameDefinition'
import { createSensorTransport } from '../platform/PlatformSensorTransport'
import { createDisplayService } from '../platform/PlatformDisplayService'
import { createAppLifecycleService } from '../platform/PlatformAppLifecycleService'
import { createBackButtonService } from '../platform/PlatformBackButtonService'
import { AppUpdateService } from '../core/update/AppUpdateService'
import { UpdateInstallGuard } from '../core/update/UpdateInstallGuard'
import { UpdatePolicyRepository } from '../core/update/UpdatePolicyRepository'
import { createUpdateProvider } from '../platform/update/createUpdateProvider'
// 智为康乐新增：设备-游戏目录 / 训练上报 / 采样直传 / 固件检查
import { CatalogService } from '../core/catalog/CatalogService'
import { resolveDeviceModel } from '../core/catalog/tagMatching'
// 内容（课程/商城）：与设备目录同一套「HTTP 优先 + 内置兜底」策略
import { ContentService } from '../core/content/ContentService'
import type { TrainingRecordDeviceSnapshot } from '../core/training/TrainingRecord'
import { TrainingReportService } from '../core/reporting/TrainingReportService'
import { createTrainingReportTransport } from '../core/reporting/createTrainingReportTransport'
import { createSampleUploader } from '../core/reporting/createSampleUploader'
import { FirmwareUpdateService } from '../platform/update/FirmwareUpdateService'
import { useDeviceLibraryStore } from '../stores/deviceLibrary'

/** 应用级单例服务，确保切换页面时不会重复创建 BLE 监听与传感器处理链路。 */
export const transport = createSensorTransport()
export const displayService = createDisplayService()
export const appLifecycleService = createAppLifecycleService()
export const backButtonService = createBackButtonService()
export const sensorService = new SensorService(transport)
const localStore = new LocalStorageStore()
export const connectionManager = new SensorConnectionManager(sensorService, new LocalStorageDeviceBindingRepository(localStore))
export const motionProfileService = new MotionProfileService(new LocalStorageMotionProfileRepository(localStore), sensorService)
export const trainingRepository = new IndexedDbTrainingRepository()

/** 设备-游戏目录（本地 Mock ↔ Golang 后端，运行时可切换/回退）。 */
export const catalogService = new CatalogService()

/** 内容（课程/教程 + 商城商品）：内置内容兜底 ↔ Golang 后端内容接口，由 VITE_CATALOG_MODE 切换。 */
export const contentService = new ContentService()

/** 训练记录上报（local 待发队列 ↔ http 直传后端）。 */
export const reportService = new TrainingReportService(createTrainingReportTransport())

/** 训练高频采样文件直传（VITE_SAMPLES_MODE=http 且配置 VITE_API_BASE 时启用；默认完全关闭，不发任何请求）。 */
export const sampleUploader = createSampleUploader()

/** 设备固件更新检查（只读展示，不做任何刷机动作）。 */
export const firmwareUpdateService = new FirmwareUpdateService()

export const updateInstallGuard = new UpdateInstallGuard()
export const updateService = new AppUpdateService(
  createUpdateProvider(),
  new UpdatePolicyRepository(localStore),
  updateInstallGuard,
)

/** 结果页使用的短期内存状态；历史记录才是可跨重启的数据来源。 */
export const latestTrainingRecord = ref<TrainingRecord | null>(null)

let initialized: Promise<void> | null = null

/** 首次加载 Profile、目录、绑定与上报队列，并在后台启动有限次数的设备恢复。 */
export function initializeAppServices(): Promise<void> {
  initialized ??= (async () => {
    await motionProfileService.load()
    await connectionManager.initialize()
    await catalogService.loadSnapshot()
    // 内容（课程/商城）预取：不阻塞启动，失败由 ContentService 内部回退内置内容。
    void contentService.loadSnapshot().catch(() => { /* 内置兜底，页面自身仍会取到内容 */ })
    await reportService.initialize()
    // 采样待发队列补传：未启用采样直传时为空操作，不产生任何请求。
    try { await sampleUploader.flush() } catch { /* 采样补传失败不影响启动 */ }
    await useDeviceLibraryStore().load()
    // 自动连接不能阻塞历史、设置或回放页面的首次渲染。
    void connectionManager.startupConnect()
  })()
  return initialized
}

/** 当前激活绑定 → 设备型号解析（按 BLE 广播名匹配目录型号）。 */
export async function resolveCurrentDeviceModel() {
  const snapshot = connectionManager.getSnapshot()
  if (!snapshot.binding) return null
  const models = await catalogService.listDeviceModels()
  const pseudo: SensorDevice = {
    id: snapshot.binding.deviceId,
    address: snapshot.binding.address ?? '',
    name: snapshot.binding.name,
  }
  return resolveDeviceModel(models, pseudo)
}

/** 设备上下文快照（训练记录强制携带：型号 + 能力标签快照）。 */
export async function resolveCurrentDeviceContext(): Promise<TrainingRecordDeviceSnapshot | null> {
  const model = await resolveCurrentDeviceModel()
  if (!model) return null
  return {
    modelId: model.modelId,
    modelName: model.name,
    capabilityTags: [...model.capabilityTags],
  }
}

/** 连接成功后登记进设备库（设备首页/游戏页共用）。 */
export async function syncActiveDeviceToLibrary(): Promise<void> {
  const snapshot = connectionManager.getSnapshot()
  if (!snapshot.binding) return
  const library = useDeviceLibraryStore()
  const models = await catalogService.listDeviceModels()
  const pseudo: SensorDevice = {
    id: snapshot.binding.deviceId,
    address: snapshot.binding.address ?? '',
    name: snapshot.binding.name,
  }
  const model = resolveDeviceModel(models, pseudo)
  await library.upsertFromConnection({ device: pseudo, model, active: true })
}

/**
 * 将任意正式游戏结果连同当时配置写入同一个历史仓库，
 * 并自动携带设备上下文（型号 + 能力标签快照）与游戏 ID，随后异步上报。
 */
export async function persistTrainingResult<TResult extends BaseTrainingResult, TConfig>(input: {
  game: GameDefinition
  result: TResult
  replay: TrainingReplay
  gameConfig: TConfig
}): Promise<TrainingRecord<TResult, TConfig>> {
  const deviceContext = await resolveCurrentDeviceContext()
  const record: TrainingRecord<TResult, TConfig> = {
    schemaVersion: 2,
    id: createRecordId(),
    gameId: input.game.id,
    gameName: input.game.name,
    completedAt: Date.now(),
    result: structuredClone(input.result),
    motionProfile: motionProfileService.getCurrent(),
    gameConfig: structuredClone(input.gameConfig),
    replay: structuredClone(input.replay),
    device: deviceContext,
  }
  await trainingRepository.save(record)
  latestTrainingRecord.value = record
  // 上报失败不阻塞：训练已本地入库，状态可在“我的-训练数据”查看。
  void reportService.report(record)
  // 高频采样（回放 JSON）直传对象存储：未配置 VITE_SAMPLES_MODE=http 时为空操作，不发请求。
  void sampleUploader.uploadReplay(record)
  return record
}

/** 使用标准 UUID；极少数不支持环境采用时间与随机值组合。 */
function createRecordId(): string {
  return typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `record-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

/** Settings 重置 Profile 后保留默认配置并立即更新实时输入。 */
export async function resetMotionProfile(): Promise<MotionProfile> {
  return motionProfileService.reset()
}
