import type { IKeyValueStore } from '../storage/IKeyValueStore'
import { LOCAL_SAMPLE_QUEUE_KEY } from './SampleUpload'
import type { PendingSampleUpload, ResolvedSampleContent, SampleUploadIdentity } from './SampleUpload'

/** 入队入参：内容已归一化，时间与尝试次数由队列补齐。 */
export type PendingSampleDraft = SampleUploadIdentity & ResolvedSampleContent

/**
 * 训练采样待发队列：以纯文本 JSON 数组持久化（默认 localStorage），键名为 LOCAL_SAMPLE_QUEUE_KEY。
 * 与训练摘要队列分开存放，避免高频采样拖慢摘要补传；
 * 所有读写经内部互斥串行化，并发上传时不会因为「读-改-写」竞争丢条目。
 */
export class SampleUploadQueue {
  /** 串行化用的锁链（每个任务在上一任务结束后才开始）。 */
  private lock: Promise<void> = Promise.resolve()

  constructor(private readonly store: IKeyValueStore) {}

  /** 读取队列：存储缺失、JSON 损坏或条目非法时安全降级（过滤坏条目，不抛错）。 */
  async read(): Promise<PendingSampleUpload[]> {
    return this.exclusive(() => this.readUnlocked())
  }

  /** 待补传条数。 */
  async size(): Promise<number> {
    return (await this.read()).length
  }

  /**
   * 入队一条采样；同一记录的同名文件只保留一条（后端对象键相同，重试即覆盖），
   * 重复入队时更新内容并累加 attempts，返回入队后长度。
   */
  async append(draft: PendingSampleDraft): Promise<number> {
    return this.exclusive(async () => {
      const queue = await this.readUnlocked()
      const existing = queue.find((item) => isSameSample(item, draft))
      if (existing) {
        existing.content = draft.content
        existing.encoding = draft.encoding
        existing.contentType = draft.contentType
        existing.queuedAt = Date.now()
        existing.attempts += 1
      } else {
        queue.push({ ...draft, queuedAt: Date.now(), attempts: 1 })
      }
      await this.writeUnlocked(queue)
      return queue.length
    })
  }

  /**
   * 移除已成功直传的条目，返回剩余长度。
   * 以「最新队列内容」为准写回：补传过程中新入队的采样不会被旧快照覆盖丢失。
   */
  async removeUploaded(uploaded: SampleUploadIdentity[]): Promise<number> {
    return this.exclusive(async () => {
      const latest = await this.readUnlocked()
      if (uploaded.length === 0) return latest.length
      const remaining = latest.filter((item) => !uploaded.some((done) => isSameSample(done, item)))
      if (remaining.length !== latest.length) await this.writeUnlocked(remaining)
      return remaining.length
    })
  }

  private async readUnlocked(): Promise<PendingSampleUpload[]> {
    const raw = await this.store.get(LOCAL_SAMPLE_QUEUE_KEY)
    if (!raw) return []
    try {
      const parsed: unknown = JSON.parse(raw)
      if (!Array.isArray(parsed)) return []
      const entries: PendingSampleUpload[] = []
      for (const item of parsed) {
        const entry = toPendingSample(item)
        if (entry) entries.push(entry)
      }
      return entries
    } catch {
      return []
    }
  }

  private async writeUnlocked(queue: PendingSampleUpload[]): Promise<void> {
    await this.store.set(LOCAL_SAMPLE_QUEUE_KEY, JSON.stringify(queue))
  }

  /** 互斥执行：保证并发 append / removeUploaded 之间的读改写不互相覆盖。 */
  private async exclusive<T>(action: () => Promise<T>): Promise<T> {
    const previous = this.lock
    let release!: () => void
    this.lock = new Promise<void>((resolve) => { release = resolve })
    await previous
    try {
      return await action()
    } finally {
      release()
    }
  }
}

/** 同一条采样判定：recordId + filename（即对象键身份）。 */
function isSameSample(left: SampleUploadIdentity, right: SampleUploadIdentity): boolean {
  return left.recordId === right.recordId && left.filename === right.filename
}

/** 队列条目归一化：历史/损坏数据只丢弃坏条目，缺省字段补默认值，不影响其余采样补传。 */
function toPendingSample(value: unknown): PendingSampleUpload | null {
  if (typeof value !== 'object' || value === null) return null
  const item = value as Partial<PendingSampleUpload>
  if (typeof item.recordId !== 'string' || item.recordId === '') return null
  if (typeof item.filename !== 'string' || item.filename === '') return null
  if (typeof item.content !== 'string') return null
  if (item.encoding !== 'text' && item.encoding !== 'base64') return null
  return {
    recordId: item.recordId,
    filename: item.filename,
    contentType: typeof item.contentType === 'string' && item.contentType !== '' ? item.contentType : 'application/octet-stream',
    content: item.content,
    encoding: item.encoding,
    queuedAt: typeof item.queuedAt === 'number' && Number.isFinite(item.queuedAt) ? item.queuedAt : 0,
    attempts: typeof item.attempts === 'number' && Number.isFinite(item.attempts) && item.attempts >= 0 ? item.attempts : 1,
  }
}
