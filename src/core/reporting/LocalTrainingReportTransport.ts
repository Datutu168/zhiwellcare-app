import type { IKeyValueStore } from '../storage/IKeyValueStore'
import { LOCAL_REPORT_QUEUE_KEY, TrainingReportQueue } from './TrainingReportQueue'
import type { ITrainingReportTransport, TrainingReportPayload } from './TrainingReport'

export { LOCAL_REPORT_QUEUE_KEY }

/**
 * 本地上报传输：后端未接入前把训练摘要写入待发队列（localStorage），
 * 切换 http 模式后由 HttpTrainingReportTransport 读取同一队列逐条回传，数据不丢失。
 */
export class LocalTrainingReportTransport implements ITrainingReportTransport {
  readonly kind = 'local' as const
  private readonly queue: TrainingReportQueue

  constructor(store: IKeyValueStore) {
    this.queue = new TrainingReportQueue(store)
  }

  async submit(payload: TrainingReportPayload): Promise<{ accepted: boolean; queueLength?: number }> {
    const queueLength = await this.queue.append(payload)
    return { accepted: true, queueLength }
  }

  async pendingCount(): Promise<number> {
    return this.queue.size()
  }

  async flush(): Promise<{ flushed: number }> {
    // 纯本地模式不发送；接入 http 模式后由统一服务编排迁移队列。
    return { flushed: 0 }
  }
}
