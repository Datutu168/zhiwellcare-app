import type { IKeyValueStore } from '../storage/IKeyValueStore'
import type { TrainingReportPayload } from './TrainingReport'

/** 本地待发队列键（VITE_REPORT_MODE=local 默认模式）。 */
export const LOCAL_REPORT_QUEUE_KEY = 'zhiwellcare.training.report.queue.v1'

/**
 * 训练上报待发队列：以纯文本 JSON 数组（TrainingReportPayload[]）持久化在键值存储中（默认 localStorage）。
 * local 模式负责写入，http 模式负责补传并在成功后逐条移除；
 * 读写格式与历史 v1 数据完全兼容（仅按原字段读取，不新增必填字段）。
 */
export class TrainingReportQueue {
  constructor(private readonly store: IKeyValueStore) {}

  /** 读取队列；存储缺失、JSON 损坏或非数组内容时安全降级为空队列，不抛错。 */
  async read(): Promise<TrainingReportPayload[]> {
    const raw = await this.store.get(LOCAL_REPORT_QUEUE_KEY)
    if (!raw) return []
    try {
      const parsed: unknown = JSON.parse(raw)
      const list: unknown[] = Array.isArray(parsed) ? (parsed as unknown[]) : []
      // 兼容历史数据：过滤非对象条目（如 null），避免补传时读取字段崩溃。
      return list.filter((item): item is TrainingReportPayload => typeof item === 'object' && item !== null)
    } catch {
      return []
    }
  }

  /** 待发条数。 */
  async size(): Promise<number> {
    return (await this.read()).length
  }

  /** 入队一条记录：同一 recordId 只保留一条（后端按 record_id 幂等），返回入队后长度。 */
  async append(payload: TrainingReportPayload): Promise<number> {
    const queue = await this.read()
    if (queue.some((item) => isSameRecord(item, payload))) return queue.length
    queue.push(payload)
    await this.write(queue)
    return queue.length
  }

  /**
   * 移除已成功上传的记录，返回剩余长度。
   * 以“最新队列内容”为准写回：补传过程中新入队的记录不会被旧快照覆盖丢失。
   */
  async removeUploaded(uploaded: TrainingReportPayload[]): Promise<number> {
    const latest = await this.read()
    if (uploaded.length === 0) return latest.length
    const remaining = latest.filter((item) => !uploaded.some((done) => isSameRecord(done, item)))
    if (remaining.length !== latest.length) await this.write(remaining)
    return remaining.length
  }

  private async write(queue: TrainingReportPayload[]): Promise<void> {
    await this.store.set(LOCAL_REPORT_QUEUE_KEY, JSON.stringify(queue))
  }
}

/** 同一条记录判定：优先按 recordId；无 recordId 的历史条目退化为引用相等。 */
function isSameRecord(left: TrainingReportPayload, right: TrainingReportPayload): boolean {
  if (left === right) return true
  const leftId = typeof left.recordId === 'string' ? left.recordId : ''
  const rightId = typeof right.recordId === 'string' ? right.recordId : ''
  return leftId !== '' && leftId === rightId
}
