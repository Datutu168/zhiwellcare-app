import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tokenStorage } from '../src/api/token'
import { HttpTrainingReportTransport } from '../src/core/reporting/HttpTrainingReportTransport'
import {
  LOCAL_REPORT_QUEUE_KEY as RE_EXPORTED_QUEUE_KEY,
  LocalTrainingReportTransport,
} from '../src/core/reporting/LocalTrainingReportTransport'
import { TrainingReportService } from '../src/core/reporting/TrainingReportService'
import { LOCAL_REPORT_QUEUE_KEY } from '../src/core/reporting/TrainingReportQueue'
import type { ITrainingReportTransport, TrainingReportPayload } from '../src/core/reporting/TrainingReport'
import type { IKeyValueStore } from '../src/core/storage/IKeyValueStore'
import type { TrainingRecord } from '../src/core/training/TrainingRecord'

const BASE = 'https://api.example.com'

/** vitest node 环境没有 localStorage，注入内存版键值存储。 */
function memoryStore(): IKeyValueStore {
  const data = new Map<string, string>()
  return {
    get: async (key) => data.get(key) ?? null,
    set: async (key, value) => { data.set(key, value) },
    remove: async (key) => { data.delete(key) },
  }
}

/** node 环境 localStorage polyfill（token 模块按需读取）。 */
function installLocalStorage(): void {
  const data = new Map<string, string>()
  const storage: Storage = {
    get length() { return data.size },
    clear: () => data.clear(),
    getItem: (key) => data.get(key) ?? null,
    key: (index) => [...data.keys()][index] ?? null,
    removeItem: (key) => { data.delete(key) },
    setItem: (key, value) => { data.set(key, String(value)) },
  }
  ;(globalThis as Record<string, unknown>).localStorage = storage
}

function payload(recordId: string): TrainingReportPayload {
  return {
    recordId,
    schemaVersion: 2,
    gameId: 'target-reach',
    gameName: '四方挥腕挑战',
    completedAt: 1700000000000,
    device: { modelId: 'wobble-wrist-band', modelName: '不倒翁手腕训练仪', capabilityTags: ['posture-sensor'] },
    statistics: { successRate: 0.85 },
    clientVersion: '0.1.0',
    uploadedAt: 1,
  }
}

/** 按调用顺序返回状态的 fetch mock；状态用尽后默认 200。 */
function mockFetchSequence(statuses: number[]) {
  let index = 0
  const mock = vi.fn(async (_url: string, _init?: RequestInit) => {
    const status = statuses[index++] ?? 200
    return new Response('', { status })
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

/** 从 fetch 调用参数中取出请求体对应的 recordId。 */
function sentRecordId(call: [string, (RequestInit | undefined)?]): string {
  return (JSON.parse(String(call[1]?.body)) as { recordId: string }).recordId
}

async function queuedIds(store: IKeyValueStore): Promise<string[]> {
  const raw = await store.get(LOCAL_REPORT_QUEUE_KEY)
  return (JSON.parse(raw ?? '[]') as { recordId: string }[]).map((item) => item.recordId)
}

beforeEach(() => {
  installLocalStorage()
  tokenStorage.clear()
})

afterEach(() => { vi.unstubAllGlobals() })

describe('HTTP 上报传输（Authorization 令牌）', () => {
  it('已登录时上报请求携带 Authorization: Bearer <accessToken>', async () => {
    tokenStorage.save('access-t', 'refresh-t')
    const fetchMock = mockFetchSequence([])
    const transport = new HttpTrainingReportTransport(BASE, memoryStore())

    const result = await transport.submit(payload('r1'))

    expect(result.accepted).toBe(true)
    const call = fetchMock.mock.calls[0]
    expect(call[0]).toBe(`${BASE}/api/v1/training/records`)
    const headers = new Headers(call[1]?.headers)
    expect(headers.get('Authorization')).toBe('Bearer access-t')
    expect(headers.get('Content-Type')).toBe('application/json')
    expect(call[1]?.method).toBe('POST')
  })

  it('没有令牌时仍照常发出上报请求（兼容公开上报部署）', async () => {
    const fetchMock = mockFetchSequence([])
    const transport = new HttpTrainingReportTransport(BASE, memoryStore())

    const result = await transport.submit(payload('r1'))

    expect(result.accepted).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const headers = new Headers(fetchMock.mock.calls[0][1]?.headers)
    expect(headers.has('Authorization')).toBe(false)
    expect(fetchMock.mock.calls[0][0]).toBe(`${BASE}/api/v1/training/records`)
  })
})

describe('HTTP 上报传输（失败不丢记录）', () => {
  it('非 2xx（500）时记录落入本地待发队列而非丢弃', async () => {
    mockFetchSequence([500])
    const store = memoryStore()
    const transport = new HttpTrainingReportTransport(BASE, store)

    await expect(transport.submit(payload('r1'))).rejects.toThrow('500')

    expect(await transport.pendingCount()).toBe(1)
    expect(await queuedIds(store)).toEqual(['r1'])
  })

  it('401 未授权时同样保留记录，等待登录后补传', async () => {
    mockFetchSequence([401])
    const store = memoryStore()
    const transport = new HttpTrainingReportTransport(BASE, store)

    await expect(transport.submit(payload('r1'))).rejects.toThrow('401')

    expect(await transport.pendingCount()).toBe(1)
  })

  it('网络异常（fetch 抛出）时保留记录', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('fetch failed') }))
    const store = memoryStore()
    const transport = new HttpTrainingReportTransport(BASE, store)

    await expect(transport.submit(payload('r1'))).rejects.toThrow('fetch failed')

    expect(await transport.pendingCount()).toBe(1)
    expect(await queuedIds(store)).toEqual(['r1'])
  })

  it('同一 recordId 重复上报失败不会产生重复队列条目', async () => {
    mockFetchSequence([500, 500])
    const store = memoryStore()
    const transport = new HttpTrainingReportTransport(BASE, store)

    await transport.submit(payload('r1')).catch(() => { /* 预期失败 */ })
    await transport.submit(payload('r1')).catch(() => { /* 预期失败 */ })

    expect(await transport.pendingCount()).toBe(1)
  })
})

describe('HTTP 上报传输（本地队列补传）', () => {
  it('flush 把队列记录逐条上传并移除，pendingCount 相应减少；重复 flush 不重复上传', async () => {
    const store = memoryStore()
    const local = new LocalTrainingReportTransport(store)
    await local.submit(payload('r1'))
    await local.submit(payload('r2'))
    expect(await local.pendingCount()).toBe(2)

    const fetchMock = mockFetchSequence([])
    const http = new HttpTrainingReportTransport(BASE, store)
    expect(await http.pendingCount()).toBe(2)

    expect(await http.flush()).toEqual({ flushed: 2 })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls.map(sentRecordId)).toEqual(['r1', 'r2'])
    expect(await http.pendingCount()).toBe(0)
    expect(await queuedIds(store)).toEqual([])

    // 幂等：队列已清空，再次 flush 不再发请求（后端 ON CONFLICT 亦不会产生重复记录）。
    expect(await http.flush()).toEqual({ flushed: 0 })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('flush 时补传请求同样携带 Authorization', async () => {
    const store = memoryStore()
    await new LocalTrainingReportTransport(store).submit(payload('r1'))
    tokenStorage.save('access-flush', 'refresh-flush')
    const fetchMock = mockFetchSequence([])

    await new HttpTrainingReportTransport(BASE, store).flush()

    expect(new Headers(fetchMock.mock.calls[0][1]?.headers).get('Authorization')).toBe('Bearer access-flush')
  })

  it('flush 只移除上传成功的记录，失败记录保留且 pendingCount 不减少到 0', async () => {
    const store = memoryStore()
    const local = new LocalTrainingReportTransport(store)
    await local.submit(payload('r1'))
    await local.submit(payload('r2'))

    mockFetchSequence([200, 500])
    const http = new HttpTrainingReportTransport(BASE, store)

    expect(await http.flush()).toEqual({ flushed: 1 })
    expect(await http.pendingCount()).toBe(1)
    expect(await queuedIds(store)).toEqual(['r2'])

    // 下一次 flush 重试成功后队列才彻底清空（同一 record_id 后端幂等，不会重复入库）。
    mockFetchSequence([])
    expect(await http.flush()).toEqual({ flushed: 1 })
    expect(await http.pendingCount()).toBe(0)
  })

  it('断网时 flush 全部失败，队列原样保留', async () => {
    const store = memoryStore()
    const local = new LocalTrainingReportTransport(store)
    await local.submit(payload('r1'))
    await local.submit(payload('r2'))

    const fetchMock = vi.fn(async () => { throw new TypeError('fetch failed') })
    vi.stubGlobal('fetch', fetchMock)
    const http = new HttpTrainingReportTransport(BASE, store)

    expect(await http.flush()).toEqual({ flushed: 0 })
    expect(await http.pendingCount()).toBe(2)
    // 网络不可达时后续记录必然失败，本轮提前结束不再重复尝试。
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('并发调用 flush 只执行一轮，同一条不会被上传两次或移除两次', async () => {
    const store = memoryStore()
    const local = new LocalTrainingReportTransport(store)
    await local.submit(payload('r1'))
    await local.submit(payload('r2'))

    const fetchMock = mockFetchSequence([])
    const http = new HttpTrainingReportTransport(BASE, store)

    const [first, second] = await Promise.all([http.flush(), http.flush()])

    expect(first).toEqual({ flushed: 2 })
    expect(second).toEqual({ flushed: 2 })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(await http.pendingCount()).toBe(0)
  })

  it('兼容已持久化的 v1 队列格式（纯文本 JSON 数组，键名不变）', async () => {
    expect(LOCAL_REPORT_QUEUE_KEY).toBe('zhiwellcare.training.report.queue.v1')
    expect(RE_EXPORTED_QUEUE_KEY).toBe(LOCAL_REPORT_QUEUE_KEY)

    const store = memoryStore()
    await store.set(LOCAL_REPORT_QUEUE_KEY, JSON.stringify([payload('legacy-1'), payload('legacy-2')]))
    const fetchMock = mockFetchSequence([])
    const http = new HttpTrainingReportTransport(BASE, store)

    expect(await http.pendingCount()).toBe(2)
    expect(await http.flush()).toEqual({ flushed: 2 })
    expect(fetchMock.mock.calls.map(sentRecordId)).toEqual(['legacy-1', 'legacy-2'])
    expect(await http.pendingCount()).toBe(0)
  })

  it('队列内容损坏（非 JSON 数组）时安全降级为空队列，不抛错', async () => {
    const store = memoryStore()
    await store.set(LOCAL_REPORT_QUEUE_KEY, '{ 不是合法 JSON')
    const http = new HttpTrainingReportTransport(BASE, store)

    expect(await http.pendingCount()).toBe(0)
    expect(await http.flush()).toEqual({ flushed: 0 })
  })

  it('本地模式切换为 http 模式后，http 传输读取同一队列完成补传', async () => {
    const store = memoryStore()
    await new LocalTrainingReportTransport(store).submit(payload('offline-1'))
    const fetchMock = mockFetchSequence([])

    const http = new HttpTrainingReportTransport(BASE, store)
    await http.flush()

    expect(fetchMock.mock.calls.map(sentRecordId)).toEqual(['offline-1'])
    expect(await http.pendingCount()).toBe(0)
  })
})

describe('上报服务初始化补传', () => {
  it('initialize 先补传本地队列再刷新待发条数', async () => {
    const calls: string[] = []
    const transport: ITrainingReportTransport = {
      kind: 'http',
      submit: async () => ({ accepted: true }),
      flush: async () => { calls.push('flush'); return { flushed: 0 } },
      pendingCount: async () => { calls.push('pendingCount'); return 0 },
    }

    await new TrainingReportService(transport).initialize()

    expect(calls).toEqual(['flush', 'pendingCount'])
  })

  it('补传失败（断网）不阻塞应用初始化', async () => {
    const transport: ITrainingReportTransport = {
      kind: 'http',
      submit: async () => ({ accepted: true }),
      flush: async () => { throw new Error('断网') },
      pendingCount: async () => 3,
    }
    const service = new TrainingReportService(transport)

    await expect(service.initialize()).resolves.toBeUndefined()
    expect(service.pendingCount.value).toBe(3)
  })

  it('直传失败后刷新待发条数，UI 可见“待上报 N 条”', async () => {
    const transport: ITrainingReportTransport = {
      kind: 'http',
      submit: async () => { throw new Error('训练记录上报失败：401（已存入本地待发队列，待补传）') },
      flush: async () => ({ flushed: 0 }),
      pendingCount: async () => 1,
    }
    const service = new TrainingReportService(transport)
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => { /* 静默预期告警 */ })

    await service.report({ id: 'r1' } as unknown as TrainingRecord)

    expect(service.pendingCount.value).toBe(1)
    expect(warn).toHaveBeenCalled()
    warn.mockRestore()
  })
})
