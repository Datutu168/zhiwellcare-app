import { ApiError, request } from '../../api/http'
import { LocalStorageStore } from '../storage/LocalStorageStore'
import type { IKeyValueStore } from '../storage/IKeyValueStore'
import type { TrainingRecord } from '../training/TrainingRecord'
import {
  decodeSampleBody,
  REPLAY_SAMPLE_FILENAME,
  resolveSampleContent,
} from './SampleUpload'
import type {
  ISampleUploader,
  PendingSampleUpload,
  ResolvedSampleContent,
  SampleUploadInput,
  SampleUploadResult,
} from './SampleUpload'
import { SampleUploadQueue } from './SampleUploadQueue'
import type { PendingSampleDraft } from './SampleUploadQueue'

/** 直传地址应答（统一信封内的 data）。 */
interface SampleUploadTicket {
  key?: string
  uploadUrl?: string
  method?: string
  expiresInSeconds?: number
  publicUrl?: string
}

/** 可注入的 PUT 实现：默认 globalThis.fetch（延迟读取，便于单测 stubGlobal）。 */
export type SamplePutFetch = (url: string, init: RequestInit) => Promise<Response>
export const defaultSamplePutFetch: SamplePutFetch = (url, init) => globalThis.fetch(url, init)

/** 单次直传结果：区分 HTTP 状态错误与网络不可达（后者无需继续补传）。 */
type DispatchResult =
  | { ok: true; key?: string; publicUrl?: string }
  | { ok: false; reason: 'http' | 'network'; detail: string }

/**
 * 训练采样文件 HTTP 直传：
 * 1) POST {base}/api/v1/training/records/{recordId}/samples/upload-url（携带 Authorization，复用 src/api/http.ts）；
 * 2) PUT {uploadUrl}（预签名地址，只带 Content-Type：额外携带 Authorization 会破坏 S3 签名校验）；
 * 由 createSampleUploader 依据 VITE_SAMPLES_MODE/VITE_API_BASE 选择，本类仅在配置了地址时实例化。
 * 失败不抛错：内容落入本地待发队列，由 flush() 在恢复登录/网络后逐条补传。
 */
export class HttpSampleUploader implements ISampleUploader {
  readonly enabled = true
  private readonly queue: SampleUploadQueue
  private readonly put: SamplePutFetch
  /** 进行中的补传任务：并发调用 flush 复用同一批，避免同一条被上传两次或移除两次。 */
  private flushing: Promise<{ flushed: number }> | null = null

  constructor(options: { store?: IKeyValueStore; putFetch?: SamplePutFetch } = {}) {
    this.queue = new SampleUploadQueue(options.store ?? new LocalStorageStore())
    this.put = options.putFetch ?? defaultSamplePutFetch
  }

  /** 上传一段采样内容；成功返回对象键，失败落本地待发队列，绝不抛到调用方。 */
  async upload(recordId: string, input: SampleUploadInput): Promise<SampleUploadResult> {
    const id = recordId.trim()
    let resolved: ResolvedSampleContent
    try {
      if (!id) throw new Error('recordId 不能为空')
      resolved = await resolveSampleContent(input)
    } catch (error) {
      return { status: 'failed', error: `采样内容无法序列化：${describeError(error)}` }
    }

    const draft: PendingSampleDraft = { recordId: id, ...resolved }
    const result = await this.dispatch(draft)
    if (result.ok) {
      // 直传成功：顺带清掉同名的历史待发条目（补传成功场景），清理失败不影响本次结果。
      await this.queue.removeUploaded([draft]).catch(() => 0)
      return { status: 'uploaded', key: result.key, publicUrl: result.publicUrl }
    }

    try {
      await this.queue.append(draft)
    } catch (error) {
      return { status: 'failed', error: `${result.detail}；写入待发队列失败：${describeError(error)}` }
    }
    return { status: 'queued', error: result.detail }
  }

  /** 上传训练回放（replay JSON 序列化）；V1 旧记录无回放数据时不入队、不请求。 */
  async uploadReplay(
    record: Pick<TrainingRecord, 'id' | 'replay'>,
    filename: string = REPLAY_SAMPLE_FILENAME,
  ): Promise<SampleUploadResult> {
    if (!record.replay) return { status: 'failed', error: '该训练记录没有回放数据' }
    return this.upload(record.id, {
      content: JSON.stringify(record.replay),
      filename,
      contentType: 'application/json',
    })
  }

  /** 待补传条数：取本地队列真实长度（含断网期间攒下的采样）。 */
  async pendingCount(): Promise<number> {
    return this.queue.size()
  }

  /**
   * 补传本地待发队列，返回本轮成功上传的条数。
   * 只有直传成功的条目才从队列移除，失败的保留等待下次；重复调用共享同一轮任务。
   */
  async flush(): Promise<{ flushed: number }> {
    this.flushing ??= this.drain().finally(() => { this.flushing = null })
    return this.flushing
  }

  private async drain(): Promise<{ flushed: number }> {
    const pending = await this.queue.read()
    const uploaded: PendingSampleUpload[] = []
    for (const entry of pending) {
      const result = await this.dispatch(entry)
      if (result.ok) {
        uploaded.push(entry)
        continue
      }
      // 网络不可达时后续条目必然同样失败，本轮提前结束，未成功的条目全部保留。
      if (result.reason === 'network') break
    }
    await this.queue.removeUploaded(uploaded)
    return { flushed: uploaded.length }
  }

  /** 申请直传地址 + PUT 直传；任何异常都归一化为结果对象。 */
  private async dispatch(draft: PendingSampleDraft): Promise<DispatchResult> {
    let ticket: SampleUploadTicket
    try {
      ticket = await request<SampleUploadTicket>(
        `/api/v1/training/records/${encodeURIComponent(draft.recordId)}/samples/upload-url`,
        { method: 'POST', body: JSON.stringify({ filename: draft.filename, contentType: draft.contentType }) },
      )
    } catch (error) {
      // http.ts 约定：status 0 表示连不上服务器（与 4xx/5xx 区分开，便于决定是否继续补传）。
      const network = error instanceof ApiError && error.status === 0
      return { ok: false, reason: network ? 'network' : 'http', detail: `申请直传地址失败：${describeError(error)}` }
    }

    const uploadUrl = typeof ticket?.uploadUrl === 'string' ? ticket.uploadUrl.trim() : ''
    if (!uploadUrl) return { ok: false, reason: 'http', detail: '申请直传地址失败：响应缺少 uploadUrl' }

    let response: Response
    try {
      response = await this.put(uploadUrl, {
        method: typeof ticket.method === 'string' && ticket.method ? ticket.method : 'PUT',
        headers: { 'Content-Type': draft.contentType },
        body: decodeSampleBody(draft),
      })
    } catch (error) {
      return { ok: false, reason: 'network', detail: `采样直传失败：${describeError(error)}` }
    }
    if (!response.ok) return { ok: false, reason: 'http', detail: `采样直传失败：${response.status}` }
    return { ok: true, key: ticket.key, publicUrl: ticket.publicUrl }
  }
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
