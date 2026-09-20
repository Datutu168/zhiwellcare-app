import type { ISampleUploader, SampleUploadResult } from './SampleUpload'

/**
 * 未启用采样直传时的空实现（默认行为）：
 * 不读本地存储、不发任何请求、不产生副作用，行为与接入前完全一致。
 */
export class NoopSampleUploader implements ISampleUploader {
  readonly enabled = false

  async upload(): Promise<SampleUploadResult> {
    return { status: 'disabled' }
  }

  async uploadReplay(): Promise<SampleUploadResult> {
    return { status: 'disabled' }
  }

  async flush(): Promise<{ flushed: number }> {
    return { flushed: 0 }
  }

  async pendingCount(): Promise<number> {
    return 0
  }
}
