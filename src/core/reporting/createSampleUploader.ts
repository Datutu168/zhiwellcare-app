import { LocalStorageStore } from '../storage/LocalStorageStore'
import type { IKeyValueStore } from '../storage/IKeyValueStore'
import { HttpSampleUploader } from './HttpSampleUploader'
import { NoopSampleUploader } from './NoopSampleUploader'
import type { ISampleUploader } from './SampleUpload'

/**
 * 采样直传工厂：VITE_SAMPLES_MODE=http 且配置了 VITE_API_BASE 时才启用真实直传；
 * 默认（未配置/其它取值）返回空实现 —— 完全不发请求，行为与接入前一致。
 */
export function createSampleUploader(store: IKeyValueStore = new LocalStorageStore()): ISampleUploader {
  const mode = import.meta.env.VITE_SAMPLES_MODE ?? 'off'
  const base = (import.meta.env.VITE_API_BASE ?? '').trim()
  if (mode === 'http' && base) return new HttpSampleUploader({ store })
  return new NoopSampleUploader()
}
