import { LocalStorageStore } from '../storage/LocalStorageStore'
import { HttpTrainingReportTransport } from './HttpTrainingReportTransport'
import { LocalTrainingReportTransport } from './LocalTrainingReportTransport'
import type { ITrainingReportTransport } from './TrainingReport'

/** 上报传输工厂：VITE_REPORT_MODE=http 且配置 VITE_API_BASE 时直传后端，否则落本地队列。 */
export function createTrainingReportTransport(): ITrainingReportTransport {
  const mode = import.meta.env.VITE_REPORT_MODE ?? 'local'
  const base = import.meta.env.VITE_API_BASE ?? ''
  const store = new LocalStorageStore()
  if (mode === 'http' && base) {
    // 与 local 模式共用同一个待发队列：断网期间攒下的记录切到 http 后由 flush() 补传。
    return new HttpTrainingReportTransport(base.replace(/\/$/, ''), store)
  }
  return new LocalTrainingReportTransport(store)
}
