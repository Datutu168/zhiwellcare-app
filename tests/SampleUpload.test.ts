import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tokenStorage } from '../src/api/token'
import { createSampleUploader } from '../src/core/reporting/createSampleUploader'
import { HttpSampleUploader } from '../src/core/reporting/HttpSampleUploader'
import { NoopSampleUploader } from '../src/core/reporting/NoopSampleUploader'
import {
  decodeSampleBody,
  LOCAL_SAMPLE_QUEUE_KEY,
  REPLAY_SAMPLE_FILENAME,
  resolveSampleContent,
} from '../src/core/reporting/SampleUpload'
import { SampleUploadQueue } from '../src/core/reporting/SampleUploadQueue'
import type { IKeyValueStore } from '../src/core/storage/IKeyValueStore'
import type { TrainingReplay } from '../src/core/replay/TrainingReplay'

const UPLOAD_URL = 'https://s3.example.com/bucket/samples/rec-1/samples.jsonl?X-Amz-Signature=abc'

/** vitest node 环境没有 localStorage，注入内存版键值存储。 */
function memoryStore(): IKeyValueStore {
  const data = new Map<string, string>()
  return {
    get: async (key) => data.get(key) ?? null,
    set: async (key, value) => { data.set(key, value) },
    remove: async (key) => { data.delete(key) },
  }
}

/** token 模块直接读 localStorage，需 polyfill。 */
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

/** 直传地址应答（统一信封）。 */
function ticketResponse(overrides: Record<string, unknown> = {}) {
  return new Response(JSON.stringify({
    code: 0,
    message: 'ok',
    data: { key: 'samples/user-1/rec-1/samples.jsonl', uploadUrl: UPLOAD_URL, method: 'PUT', expiresInSeconds: 900, ...overrides },
  }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

/** 按调用顺序返回应答的 fetch mock：第 1 次申请地址，第 2 次 PUT 直传。 */
function mockUploadFlow(ticketStatus = 200, putStatus = 200) {
  const mock = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === 'PUT' || url.startsWith('https://s3.example.com')) return new Response('', { status: putStatus })
    if (ticketStatus !== 200) {
      return new Response(JSON.stringify({ code: 500, message: '服务器内部错误', data: null }), { status: ticketStatus })
    }
    return ticketResponse()
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

async function queuedEntries(store: IKeyValueStore) {
  return JSON.parse((await store.get(LOCAL_SAMPLE_QUEUE_KEY)) ?? '[]') as Array<Record<string, unknown>>
}

beforeEach(() => {
  installLocalStorage()
  tokenStorage.clear()
})
afterEach(() => { vi.unstubAllGlobals(); vi.unstubAllEnvs() })

describe('采样直传开关（VITE_SAMPLES_MODE）', () => {
  it('默认（未配置）返回空实现：不发起任何请求，也不写本地队列', async () => {
    const store = memoryStore()
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const uploader = createSampleUploader(store)

    expect(uploader).toBeInstanceOf(NoopSampleUploader)
    expect(uploader.enabled).toBe(false)
    expect(await uploader.upload('rec-1', { content: '{"a":1}' })).toEqual({ status: 'disabled' })
    expect(await uploader.uploadReplay({ id: 'rec-1', replay: null })).toEqual({ status: 'disabled' })
    expect(await uploader.pendingCount()).toBe(0)
    expect(await uploader.flush()).toEqual({ flushed: 0 })
    expect(fetchMock).not.toHaveBeenCalled()
    expect(await store.get(LOCAL_SAMPLE_QUEUE_KEY)).toBeNull()
  })

  it('VITE_SAMPLES_MODE=http 但 VITE_API_BASE 为空时仍不启用', () => {
    vi.stubEnv('VITE_SAMPLES_MODE', 'http')
    vi.stubEnv('VITE_API_BASE', '')
    expect(createSampleUploader(memoryStore()).enabled).toBe(false)

    vi.stubEnv('VITE_API_BASE', '   ')
    expect(createSampleUploader(memoryStore()).enabled).toBe(false)
  })

  it('VITE_SAMPLES_MODE=http 且 VITE_API_BASE 有值时启用真实直传', () => {
    vi.stubEnv('VITE_SAMPLES_MODE', 'http')
    vi.stubEnv('VITE_API_BASE', 'https://api.example.com')
    expect(createSampleUploader(memoryStore()).enabled).toBe(true)
  })
})

describe('采样直传（upload-url → PUT）', () => {
  it('按顺序先申请直传地址（带 Authorization）再 PUT 直传，返回对象键', async () => {
    tokenStorage.save('access-s', 'refresh-s')
    const fetchMock = mockUploadFlow()
    const uploader = new HttpSampleUploader({ store: memoryStore() })

    const result = await uploader.upload('rec-1', { content: '{"t":1}', filename: 'samples.jsonl' })

    expect(result.status).toBe('uploaded')
    expect(result.key).toBe('samples/user-1/rec-1/samples.jsonl')
    expect(fetchMock).toHaveBeenCalledTimes(2)

    const [ticketUrl, ticketInit] = fetchMock.mock.calls[0]
    expect(ticketUrl).toContain('/api/v1/training/records/rec-1/samples/upload-url')
    expect(ticketInit?.method).toBe('POST')
    expect(JSON.parse(String(ticketInit?.body))).toEqual({ filename: 'samples.jsonl', contentType: 'application/json' })
    expect(new Headers(ticketInit?.headers).get('Authorization')).toBe('Bearer access-s')

    const [putUrl, putInit] = fetchMock.mock.calls[1]
    expect(putUrl).toBe(UPLOAD_URL)
    expect(putInit?.method).toBe('PUT')
    expect(putInit?.body).toBe('{"t":1}')
    // 预签名地址只带 Content-Type：额外携带 Authorization 会破坏 S3 签名校验。
    const putHeaders = new Headers(putInit?.headers)
    expect(putHeaders.get('Content-Type')).toBe('application/json')
    expect(putHeaders.has('Authorization')).toBe(false)
  })

  it('recordId 做 URL 转义，二进制内容按 base64 还原后 PUT', async () => {
    const fetchMock = mockUploadFlow()
    const uploader = new HttpSampleUploader({ store: memoryStore() })

    const result = await uploader.upload('rec/1 号', { content: new Uint8Array([1, 2, 3]), filename: 'block.bin', contentType: 'application/octet-stream' })

    expect(result.status).toBe('uploaded')
    expect(fetchMock.mock.calls[0][0]).toContain(`/api/v1/training/records/${encodeURIComponent('rec/1 号')}/samples/upload-url`)
    expect(new Headers(fetchMock.mock.calls[1][1]?.headers).get('Content-Type')).toBe('application/octet-stream')
    expect([...(fetchMock.mock.calls[1][1]?.body as Uint8Array)]).toEqual([1, 2, 3])
  })

  it('上传训练回放：序列化 replay 为 replay.json，无回放数据时不请求', async () => {
    const replay: TrainingReplay = { schemaVersion: 1, durationMs: 1000, sampleRateHz: 10, samples: [{ elapsedMs: 0, x: 1, y: 2 }], events: [] }
    const fetchMock = mockUploadFlow()
    const uploader = new HttpSampleUploader({ store: memoryStore() })

    const result = await uploader.uploadReplay({ id: 'rec-1', replay })

    expect(result.status).toBe('uploaded')
    expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toEqual(replay)
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({ filename: REPLAY_SAMPLE_FILENAME, contentType: 'application/json' })

    fetchMock.mockClear()
    const legacy = await uploader.uploadReplay({ id: 'rec-old', replay: null })
    expect(legacy.status).toBe('failed')
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('采样直传失败（不抛到调用方，进待发队列）', () => {
  it('PUT 直传失败（5xx）时进入本地待发队列且不抛错', async () => {
    mockUploadFlow(200, 500)
    const store = memoryStore()
    const uploader = new HttpSampleUploader({ store })

    const result = await uploader.upload('rec-1', { content: '{"t":1}', filename: 'samples.jsonl' })

    expect(result.status).toBe('queued')
    expect(result.error).toContain('500')
    expect(await uploader.pendingCount()).toBe(1)
    expect((await queuedEntries(store))[0]).toMatchObject({
      recordId: 'rec-1', filename: 'samples.jsonl', contentType: 'application/json', content: '{"t":1}', encoding: 'text', attempts: 1,
    })
  })

  it('申请直传地址失败（401/500/断网）同样入队且不抛错', async () => {
    const forbidden = mockUploadFlow(401)
    const store = memoryStore()
    const uploader = new HttpSampleUploader({ store })
    expect((await uploader.upload('rec-1', { content: 'x' })).status).toBe('queued')
    expect(forbidden).toHaveBeenCalledTimes(1)
    expect(await uploader.pendingCount()).toBe(1)

    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('fetch failed') }))
    const offline = new HttpSampleUploader({ store })
    const result = await offline.upload('rec-2', { content: 'y' })
    expect(result.status).toBe('queued')
    // src/api/http.ts 把断网归一化为「无法连接服务器」，这里仅确认失败原因被保留下来。
    expect(result.error).toContain('无法连接服务器')
    expect(await offline.pendingCount()).toBe(2)
  })

  it('同一记录同名文件重复失败只保留一条队列条目（attempts 累加）', async () => {
    mockUploadFlow(200, 500)
    const store = memoryStore()
    const uploader = new HttpSampleUploader({ store })

    await uploader.upload('rec-1', { content: 'a', filename: 'samples.jsonl' })
    await uploader.upload('rec-1', { content: 'b', filename: 'samples.jsonl' })

    const entries = await queuedEntries(store)
    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({ content: 'b', attempts: 2 })
  })

  it('二进制采样的队列内容以 base64 持久化并可还原', async () => {
    mockUploadFlow(200, 500)
    const store = memoryStore()
    const uploader = new HttpSampleUploader({ store })

    await uploader.upload('rec-1', { content: new Uint8Array([7, 8, 9]), filename: 'block.bin', contentType: 'application/octet-stream' })

    const entry = (await queuedEntries(store))[0] as { encoding: string; content: string }
    expect(entry.encoding).toBe('base64')
    expect([...(decodeSampleBody({ encoding: 'base64', content: entry.content }) as Uint8Array)]).toEqual([7, 8, 9])
  })
})

describe('采样补传（flush）', () => {
  it('flush 逐条补传并移除成功条目；重复 flush 不重复上传', async () => {
    const store = memoryStore()
    mockUploadFlow(200, 500)
    const uploader = new HttpSampleUploader({ store })
    await uploader.upload('rec-1', { content: 'a', filename: 'a.jsonl' })
    await uploader.upload('rec-2', { content: 'b', filename: 'b.jsonl' })
    expect(await uploader.pendingCount()).toBe(2)

    const fetchMock = mockUploadFlow()
    expect(await uploader.flush()).toEqual({ flushed: 2 })
    expect(fetchMock).toHaveBeenCalledTimes(4)
    expect(await uploader.pendingCount()).toBe(0)
    expect(await queuedEntries(store)).toEqual([])

    expect(await uploader.flush()).toEqual({ flushed: 0 })
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })

  it('断网时 flush 全部保留；恢复网络后下一次 flush 清空队列', async () => {
    const store = memoryStore()
    const queue = new SampleUploadQueue(store)
    await queue.append({ recordId: 'rec-1', filename: 'a.jsonl', contentType: 'application/json', content: 'a', encoding: 'text' })

    const offline = vi.fn(async () => { throw new TypeError('fetch failed') })
    vi.stubGlobal('fetch', offline)
    const uploader = new HttpSampleUploader({ store })
    expect(await uploader.flush()).toEqual({ flushed: 0 })
    expect(offline).toHaveBeenCalledTimes(1)
    expect(await uploader.pendingCount()).toBe(1)

    mockUploadFlow()
    expect(await uploader.flush()).toEqual({ flushed: 1 })
    expect(await uploader.pendingCount()).toBe(0)
  })

  it('并发 flush 只执行一轮，同一条不会被上传两次', async () => {
    const store = memoryStore()
    const queue = new SampleUploadQueue(store)
    await queue.append({ recordId: 'rec-1', filename: 'a.jsonl', contentType: 'application/json', content: 'a', encoding: 'text' })
    await queue.append({ recordId: 'rec-2', filename: 'b.jsonl', contentType: 'application/json', content: 'b', encoding: 'text' })

    const fetchMock = mockUploadFlow()
    const uploader = new HttpSampleUploader({ store })

    const [first, second] = await Promise.all([uploader.flush(), uploader.flush()])

    expect(first).toEqual({ flushed: 2 })
    expect(second).toEqual({ flushed: 2 })
    expect(fetchMock).toHaveBeenCalledTimes(4)
    expect(await uploader.pendingCount()).toBe(0)
  })

  it('并发 upload 互不覆盖（队列读改写互斥）', async () => {
    mockUploadFlow(200, 500)
    const store = memoryStore()
    const uploader = new HttpSampleUploader({ store })

    await Promise.all([
      uploader.upload('rec-1', { content: 'a', filename: 'a.jsonl' }),
      uploader.upload('rec-2', { content: 'b', filename: 'b.jsonl' }),
      uploader.upload('rec-3', { content: 'c', filename: 'c.jsonl' }),
    ])

    expect(await uploader.pendingCount()).toBe(3)
    expect((await queuedEntries(store)).map((item) => item.recordId)).toEqual(['rec-1', 'rec-2', 'rec-3'])
  })
})

describe('采样内容归一化与队列健壮性', () => {
  it('文本/二进制内容的文件名与 MIME 默认值符合约定', async () => {
    expect(await resolveSampleContent({ content: 'x' })).toEqual({ filename: 'samples.jsonl', contentType: 'application/json', content: 'x', encoding: 'text' })
    expect(await resolveSampleContent({ content: new Uint8Array([1]), contentType: 'application/octet-stream' })).toMatchObject({ filename: 'samples.bin', encoding: 'base64' })
    expect(await resolveSampleContent({ content: new Uint8Array([1]) })).toMatchObject({ filename: 'samples.bin', contentType: 'application/octet-stream' })
  })

  it('队列内容损坏（非 JSON 数组 / 坏条目）时安全降级，好条目仍可补传', async () => {
    const store = memoryStore()
    await store.set(LOCAL_SAMPLE_QUEUE_KEY, '{ 不是合法 JSON')
    const queue = new SampleUploadQueue(store)
    expect(await queue.read()).toEqual([])

    await store.set(LOCAL_SAMPLE_QUEUE_KEY, JSON.stringify([
      { recordId: 'rec-1', filename: 'a.jsonl', content: 'a', encoding: 'text' },
      null,
      { recordId: '', filename: 'b.jsonl', content: 'b', encoding: 'text' },
      { recordId: 'rec-3', filename: 'c.jsonl', content: 'c', encoding: 'utf8' },
    ]))
    const restored = await queue.read()
    expect(restored).toHaveLength(1)
    expect(restored[0]).toMatchObject({ recordId: 'rec-1', contentType: 'application/octet-stream', attempts: 1 })
  })

  it('待发队列键为固定契约值（跨版本补传依赖）', () => {
    expect(LOCAL_SAMPLE_QUEUE_KEY).toBe('zhiwellcare.training.samples.queue.v1')
  })
})
