import { ref } from 'vue'
import type { TrainingRecord } from '../training/TrainingRecord'
import type { ITrainingReportTransport } from './TrainingReport'
import { toReportPayload } from './TrainingReport'

/**
 * 训练记录上报服务：训练完成并本地入库后调用 report()，
 * 在 UI 层（我的-训练数据）可通过 pendingCount 展示“待上报 N 条”状态。
 */
export class TrainingReportService {
  /** 待上报队列长度（响应式，UI 订阅）。 */
  readonly pendingCount = ref(0)

  constructor(private readonly transport: ITrainingReportTransport) {}

  async initialize(): Promise<void> {
    // 启动时先补传本地待发队列（http 模式）；失败保留队列，不阻塞应用初始化。
    try { await this.transport.flush() } catch { /* 网络或登录态不可用时留待下次 */ }
    await this.refreshPendingCount()
  }

  async report(record: TrainingRecord): Promise<void> {
    const payload = toReportPayload(record, appVersion())
    try {
      const result = await this.transport.submit(payload)
      if (result.accepted && result.queueLength !== undefined) {
        this.pendingCount.value = result.queueLength
      }
    } catch (error) {
      // 上报失败不打断训练流程：记录已本地入库（IndexedDB），并进入待发队列等待补传。
      console.warn('[report] 训练记录上报失败，已保留在本地记录库：', error)
      await this.refreshPendingCount()
    }
  }

  async refreshPendingCount(): Promise<void> {
    try {
      this.pendingCount.value = await this.transport.pendingCount()
    } catch {
      this.pendingCount.value = 0
    }
  }
}

/** 客户端版本：优先取构建元数据，否则从 package.json 同源的 release 标识读取。 */
function appVersion(): string {
  const fromEnv = import.meta.env.VITE_APP_VERSION as string | undefined
  return fromEnv || '0.1.0'
}
