import { tokenStorage } from '../../api/token'
import { LocalStorageStore } from '../storage/LocalStorageStore'
import type { IKeyValueStore } from '../storage/IKeyValueStore'
import { TrainingReportQueue } from './TrainingReportQueue'
import type { ITrainingReportTransport, TrainingReportPayload } from './TrainingReport'

/** 单条直传结果：区分 HTTP 状态错误与网络不可达（后者无需继续补传）。 */
type SendResult = { ok: true } | { ok: false; reason: 'http' | 'network'; detail: string }

/**
 * HTTP 上报传输：POST {base}/api/v1/training/records 直传 Golang 后端（PG 训练摘要）。
 * 由 createTrainingReportTransport 依据 VITE_REPORT_MODE 选择；本类仅在配置了地址时实例化。
 * 请求携带 Authorization（令牌取自 src/api/token，与 src/api/http.ts 一致）；
 * 直传失败的记录落入本地待发队列，由 flush() 在恢复登录/网络后逐条补传。
 */
export class HttpTrainingReportTransport implements ITrainingReportTransport {
  readonly kind = 'http' as const
  private readonly queue: TrainingReportQueue
  /** 进行中的补传任务：并发调用 flush 复用同一批，避免同一条被上传两次或移除两次。 */
  private flushing: Promise<{ flushed: number }> | null = null

  constructor(private readonly baseUrl: string, store: IKeyValueStore = new LocalStorageStore()) {
    this.queue = new TrainingReportQueue(store)
  }

  async submit(payload: TrainingReportPayload): Promise<{ accepted: boolean; queueLength?: number }> {
    const result = await this.send(payload)
    if (result.ok) return { accepted: true, queueLength: await this.queue.size() }
    // 401/403/5xx/断网都不丢数据：先落本地待发队列，再由 flush() 补传。
    await this.queue.append(payload)
    throw new Error(`${result.detail}（已存入本地待发队列，待补传）`)
  }

  /** 待发条数：取本地队列真实长度（含断网期间攒下的记录）。 */
  async pendingCount(): Promise<number> {
    return this.queue.size()
  }

  /**
   * 补传本地待发队列，返回本轮成功上传的条数。
   * 只有上传成功的记录才从队列移除，失败原样保留等待下次；重复调用共享同一轮任务。
   */
  async flush(): Promise<{ flushed: number }> {
    this.flushing ??= this.drain().finally(() => { this.flushing = null })
    return this.flushing
  }

  private async drain(): Promise<{ flushed: number }> {
    const pending = await this.queue.read()
    const uploaded: TrainingReportPayload[] = []
    for (const payload of pending) {
      const result = await this.send(payload)
      if (result.ok) {
        uploaded.push(payload)
        continue
      }
      // 网络不可达时后续记录必然同样失败，本轮提前结束，未成功的记录全部保留。
      if (result.reason === 'network') break
    }
    await this.queue.removeUploaded(uploaded)
    return { flushed: uploaded.length }
  }

  /**
   * 单条直传。仅 2xx 视为成功；无令牌时也照常发送，
   * 兼容后端部署为公开上报的场景（此时后端不校验 Authorization）。
   */
  private async send(payload: TrainingReportPayload): Promise<SendResult> {
    const headers = new Headers({ 'Content-Type': 'application/json' })
    const accessToken = tokenStorage.accessToken
    if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`)

    let response: Response
    try {
      response = await fetch(`${this.baseUrl}/api/v1/training/records`, {
        method: 'POST',
        headers,
        body: JSON.stringify(payload),
      })
    } catch (error) {
      return { ok: false, reason: 'network', detail: `训练记录上报失败：${error instanceof Error ? error.message : String(error)}` }
    }
    if (!response.ok) return { ok: false, reason: 'http', detail: `训练记录上报失败：${response.status}` }
    return { ok: true }
  }
}
