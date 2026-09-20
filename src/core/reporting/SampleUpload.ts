import type { TrainingRecord } from '../training/TrainingRecord'

/**
 * 训练高频采样文件直传（对象存储）。
 * 流程：POST 申请直传地址 → PUT 直传（S3 预签名 / 后端本地签名端点，流程一致）。
 * 任何失败都只影响本次上传：内容落入本地待发队列，由 flush() 在下次启动/恢复网络后补传。
 */

/** 采样文件待发队列键：与训练摘要队列分开，避免高频采样拖慢摘要补传。 */
export const LOCAL_SAMPLE_QUEUE_KEY = 'zhiwellcare.training.samples.queue.v1'

/** 默认文本采样文件名（JSONL 高频采样）。 */
export const DEFAULT_SAMPLE_FILENAME = 'samples.jsonl'

/** 默认二进制采样文件名。 */
export const DEFAULT_BINARY_SAMPLE_FILENAME = 'samples.bin'

/** 训练回放（replay JSON）文件名。 */
export const REPLAY_SAMPLE_FILENAME = 'replay.json'

/** 内容编码方式：text 原样存文本，base64 存二进制（localStorage 只能存字符串）。 */
export type SampleContentEncoding = 'text' | 'base64'

/** 上传内容入参：文本（回放 JSON / JSONL）或二进制（高频采样块）。 */
export interface SampleUploadInput {
  content: string | Blob | ArrayBuffer | Uint8Array
  /** 文件名（会成为对象键的一部分）；缺省按内容类型推导。 */
  filename?: string
  /** MIME 类型；缺省按内容类型推导（json / octet-stream）。 */
  contentType?: string
}

/** 归一化后的采样内容：可直接 PUT，也可原样持久化进待发队列。 */
export interface ResolvedSampleContent {
  filename: string
  contentType: string
  content: string
  encoding: SampleContentEncoding
}

/** 待发队列条目身份：同一记录的同名文件视为同一条（重试时合并）。 */
export interface SampleUploadIdentity {
  recordId: string
  filename: string
}

/** 本地待发队列条目（纯 JSON 可持久化）。 */
export interface PendingSampleUpload extends SampleUploadIdentity {
  contentType: string
  content: string
  encoding: SampleContentEncoding
  queuedAt: number
  /** 尝试次数（每次因失败合并入队 +1，便于排查长期失败的采样）。 */
  attempts: number
}

/** 上传结果：uploaded 成功直传；queued 已落本地待发队列；disabled 未启用；failed 连队列也没写入。 */
export type SampleUploadStatus = 'uploaded' | 'queued' | 'disabled' | 'failed'

export interface SampleUploadResult {
  status: SampleUploadStatus
  /** 对象键（后端返回，排障用）。 */
  key?: string
  /** 公开下载地址（后端配置 CDN 时返回）。 */
  publicUrl?: string
  /** 未成功时的原因摘要（仅日志/诊断用，不面向患者展示）。 */
  error?: string
}

/**
 * 采样上传器接口。未配置时由 NoopSampleUploader 实现（完全不发请求）。
 * upload/flush 均不抛错：训练主流程不因采样上报失败而中断。
 */
export interface ISampleUploader {
  /** 是否已启用真实直传（未启用时调用方无需准备采样内容）。 */
  readonly enabled: boolean
  upload(recordId: string, input: SampleUploadInput): Promise<SampleUploadResult>
  /** 上传训练回放（replay JSON 序列化）；无回放数据时返回 failed，不入队。 */
  uploadReplay(record: Pick<TrainingRecord, 'id' | 'replay'>, filename?: string): Promise<SampleUploadResult>
  /** 补传本地待发队列，返回本轮成功条数。 */
  flush(): Promise<{ flushed: number }>
  /** 待补传条数。 */
  pendingCount(): Promise<number>
}

/** 归一化采样内容：文本原样、二进制转 base64（保证可持久化与重试）。 */
export async function resolveSampleContent(input: SampleUploadInput): Promise<ResolvedSampleContent> {
  if (typeof input.content === 'string') {
    return {
      filename: input.filename?.trim() || DEFAULT_SAMPLE_FILENAME,
      contentType: input.contentType?.trim() || 'application/json',
      content: input.content,
      encoding: 'text',
    }
  }
  const blobType = isBlob(input.content) ? input.content.type : ''
  return {
    filename: input.filename?.trim() || DEFAULT_BINARY_SAMPLE_FILENAME,
    contentType: input.contentType?.trim() || blobType || 'application/octet-stream',
    content: bytesToBase64(await toBytes(input.content)),
    encoding: 'base64',
  }
}

/** 还原可直接作为 PUT body 的内容（待发队列条目与归一化内容都可传入）。 */
export function decodeSampleBody(entry: Pick<PendingSampleUpload, 'encoding' | 'content'>): string | Uint8Array {
  return entry.encoding === 'text' ? entry.content : base64ToBytes(entry.content)
}

async function toBytes(content: Blob | ArrayBuffer | Uint8Array): Promise<Uint8Array> {
  if (isBlob(content)) return new Uint8Array(await content.arrayBuffer())
  if (content instanceof Uint8Array) return content
  return new Uint8Array(content)
}

function isBlob(content: unknown): content is Blob {
  return typeof Blob !== 'undefined' && content instanceof Blob
}

/** 二进制转 base64（分块避免超长参数列表）。 */
function bytesToBase64(bytes: Uint8Array): string {
  if (typeof btoa !== 'function') throw new Error('当前环境不支持 base64 编码，无法持久化二进制采样')
  let binary = ''
  const chunkSize = 0x8000
  for (let index = 0; index < bytes.length; index += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize))
  }
  return btoa(binary)
}

/** base64 还原为字节（PUT 的 body 用）。 */
function base64ToBytes(encoded: string): Uint8Array {
  const binary = atob(encoded)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index)
  return bytes
}
