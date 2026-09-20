import { afterEach, describe, expect, it, vi } from 'vitest'
import { FirmwareUpdateService } from '../src/platform/update/FirmwareUpdateService'

/** 统一信封应答 mock。 */
function mockEnvelope(data: unknown, status = 200, code = 0) {
  const mock = vi.fn(async (_url: string, _init?: RequestInit) => new Response(
    JSON.stringify({ code, message: code === 0 ? 'ok' : '服务端错误', data }),
    { status, headers: { 'Content-Type': 'application/json' } },
  ))
  vi.stubGlobal('fetch', mock)
  return mock
}

afterEach(() => { vi.unstubAllGlobals() })

describe('固件更新检查（只读）', () => {
  it('有更新：返回 available + 版本 + 说明，并按 currentVersion 查询', async () => {
    const fetchMock = mockEnvelope({
      upToDate: false,
      version: '1.4.0',
      url: 'https://cdn.example.com/firmware/wobble-wrist-band/1.4.0/fw.bin',
      sha256: 'abc',
      size: 4096,
      notes: ['优化蓝牙稳定性', ''],
      publishedAt: '2025-01-01T00:00:00Z',
    })
    const service = new FirmwareUpdateService()

    const result = await service.checkFirmware('wobble-wrist-band', '1.0.0')

    expect(result.available).toBe(true)
    expect(result.version).toBe('1.4.0')
    expect(result.url).toContain('/firmware/wobble-wrist-band/1.4.0/')
    expect(result.size).toBe(4096)
    // 空说明被过滤，避免设置页出现空行。
    expect(result.notes).toEqual(['优化蓝牙稳定性'])
    expect(result.error).toBeUndefined()

    const url = fetchMock.mock.calls[0][0]
    expect(url).toContain('/api/v1/device/wobble-wrist-band/firmware?currentVersion=1.0.0')
  })

  it('无更新：available=false 且带上后端返回的最新版本', async () => {
    mockEnvelope({ upToDate: true, version: '1.0.0' })
    const service = new FirmwareUpdateService()

    const result = await service.checkFirmware('wobble-wrist-band', '1.0.0')

    expect(result).toEqual({ available: false, version: '1.0.0' })
  })

  it('网络异常：不抛错，返回 error（UI 展示「暂时无法检查」）', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('fetch failed') }))
    const service = new FirmwareUpdateService()

    const result = await service.checkFirmware('wobble-wrist-band', '1.0.0')

    expect(result.available).toBe(false)
    expect(result.error).toBeTruthy()
  })

  it('接口报错（500 / 业务码非 0 / 结构异常）都收敛为 error，不抛到 UI', async () => {
    const service = new FirmwareUpdateService()

    mockEnvelope(null, 500)
    expect((await service.checkFirmware('m1', '1.0.0')).error).toBeTruthy()

    mockEnvelope({ upToDate: false }, 200, 4001)
    expect((await service.checkFirmware('m1', '1.0.0')).error).toBeTruthy()

    mockEnvelope({ foo: 'bar' })
    expect((await service.checkFirmware('m1', '1.0.0')).error).toBe('固件接口返回结构异常')
  })

  it('机型 id 取不到（未连接设备）时不发请求，直接返回提示', async () => {
    const fetchMock = mockEnvelope({ upToDate: true, version: '1.0.0' })
    const service = new FirmwareUpdateService()

    const result = await service.checkFirmware('', '1.0.0')

    expect(result).toEqual({ available: false, error: '未连接设备' })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('本机固件版本未知（空 currentVersion）时不带查询参数，仍可检查', async () => {
    const fetchMock = mockEnvelope({ upToDate: false, version: '1.4.0' })
    const service = new FirmwareUpdateService()

    const result = await service.checkFirmware('wobble-wrist-band', '  ')

    expect(result.available).toBe(true)
    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/device/wobble-wrist-band/firmware')
    expect(String(fetchMock.mock.calls[0][0])).not.toContain('currentVersion')
  })

  it('复用统一请求封装（同一 host、Accept: application/json）', async () => {
    const fetchMock = mockEnvelope({ upToDate: true, version: '1.0.0' })
    const service = new FirmwareUpdateService()

    await service.checkFirmware('wobble-wrist-band', '1.0.0')

    expect(new Headers(fetchMock.mock.calls[0][1]?.headers).get('Accept')).toBe('application/json')
  })
})
