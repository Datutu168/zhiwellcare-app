import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, API_BASE } from '../src/api/http'
import { tokenStorage } from '../src/api/token'
import * as authApi from '../src/api/auth'
import {
  CHANGE_PASSWORD_FAILURE_MESSAGE,
  CHANGE_PASSWORD_MAX_LENGTH,
  CHANGE_PASSWORD_MIN_LENGTH,
  CHANGE_PASSWORD_SUCCESS_MESSAGE,
  ChangePasswordForm,
  validateChangePassword,
} from '../src/core/account/ChangePasswordForm'
import type { ChangePasswordFields, ChangePasswordRequester } from '../src/core/account/ChangePasswordForm'

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

function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), { status, headers: { 'Content-Type': 'application/json' } })
}

function mockFetchOnce(payload: unknown, status = 200): void {
  vi.stubGlobal('fetch', vi.fn(async () => jsonResponse(payload, status)))
}

/** 合法输入：新密码长度在 6-64 之间，两次输入一致，且与原密码不同。 */
const validFields: ChangePasswordFields = {
  oldPassword: 'old-secret',
  newPassword: 'new-secret-1',
  confirmPassword: 'new-secret-1',
}

/** 已填好三个输入框的表单实例。 */
function filledForm(send: ChangePasswordRequester): ChangePasswordForm {
  const form = new ChangePasswordForm(send)
  form.oldPassword = validFields.oldPassword
  form.newPassword = validFields.newPassword
  form.confirmPassword = validFields.confirmPassword
  return form
}

beforeEach(() => {
  installLocalStorage()
  tokenStorage.clear()
})

afterEach(() => { vi.unstubAllGlobals() })

describe('修改密码接口（PUT /api/v1/me/password）', () => {
  it('向契约路径发送 PUT，载荷只含 oldPassword / newPassword，并携带登录令牌', async () => {
    tokenStorage.save('access-cp', 'refresh-cp')
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ code: 0, message: 'ok', data: { updated: true } }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await authApi.changePassword('old-secret', 'new-secret-1')

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`${API_BASE}/api/v1/me/password`)
    expect(init.method).toBe('PUT')
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer access-cp')
    const body = JSON.parse(String(init.body)) as Record<string, unknown>
    expect(body).toEqual({ oldPassword: 'old-secret', newPassword: 'new-secret-1' })
    // 契约字段名固定：不得混入确认密码等额外字段
    expect(Object.keys(body).sort()).toEqual(['newPassword', 'oldPassword'])
    expect(result.updated).toBe(true)
  })

  it('后端 401（令牌失效）以 ApiError 抛出并保留中文提示', async () => {
    mockFetchOnce({ code: 401, message: '原密码不正确', data: null }, 401)

    const error = await authApi.changePassword('wrong-old', 'new-secret-1').catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({ status: 401, message: '原密码不正确' })
  })

  it('后端 400（当前实现：原密码填错用 400 而非 401）同样抛出并保留中文提示', async () => {
    mockFetchOnce({ code: 400, message: '原密码不正确', data: null }, 400)

    const error = await authApi.changePassword('wrong-old', 'new-secret-1').catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({ status: 400, message: '原密码不正确' })
  })
})

describe('修改密码客户端校验（与后端 6-64 位规则对齐）', () => {
  it('合法输入通过校验（含 6 位与 64 位边界）', () => {
    expect(validateChangePassword(validFields)).toBe('')
    const min = 'x'.repeat(CHANGE_PASSWORD_MIN_LENGTH)
    const max = 'y'.repeat(CHANGE_PASSWORD_MAX_LENGTH)
    expect(validateChangePassword({ oldPassword: 'old-secret', newPassword: min, confirmPassword: min })).toBe('')
    expect(validateChangePassword({ oldPassword: 'old-secret', newPassword: max, confirmPassword: max })).toBe('')
  })

  it('三个输入框都必填', () => {
    expect(validateChangePassword({ ...validFields, oldPassword: '' })).toBe('请输入原密码')
    expect(validateChangePassword({ ...validFields, newPassword: '' })).toBe('请输入新密码')
    expect(validateChangePassword({ ...validFields, confirmPassword: '' })).toBe('请再次输入新密码')
  })

  it('空表单提交时被本地校验挡住，不会发出请求', async () => {
    const send = vi.fn<ChangePasswordRequester>(async () => ({ updated: true }))
    const form = new ChangePasswordForm(send)

    expect(await form.submit()).toBe(false)

    expect(send).not.toHaveBeenCalled()
    expect(form.errorMessage).toBe('请输入原密码')
  })

  it('新密码过短或过长被拒绝（文案与后端 400 message 一致）', () => {
    const short = 'x'.repeat(CHANGE_PASSWORD_MIN_LENGTH - 1)
    const long = 'x'.repeat(CHANGE_PASSWORD_MAX_LENGTH + 1)
    expect(validateChangePassword({ ...validFields, newPassword: short, confirmPassword: short }))
      .toBe(`密码至少 ${CHANGE_PASSWORD_MIN_LENGTH} 位`)
    expect(validateChangePassword({ ...validFields, newPassword: long, confirmPassword: long }))
      .toBe(`密码最长 ${CHANGE_PASSWORD_MAX_LENGTH} 位`)
  })

  it('确认密码不一致被拒绝', () => {
    expect(validateChangePassword({ ...validFields, confirmPassword: 'another-secret' })).toBe('两次输入的密码不一致')
  })

  it('新密码与原密码相同被拒绝（与后端 400 文案一致）', () => {
    expect(validateChangePassword({
      oldPassword: 'old-secret',
      newPassword: 'old-secret',
      confirmPassword: 'old-secret',
    })).toBe('新密码不能与原密码相同')
  })
})

describe('修改密码表单提交（清空输入 + 提示 + 防重复提交）', () => {
  it('请求成功：按契约字段提交、给出成功提示并清空三个输入框', async () => {
    const send = vi.fn<ChangePasswordRequester>(async () => ({ updated: true }))
    const form = filledForm(send)

    expect(await form.submit()).toBe(true)

    expect(send).toHaveBeenCalledWith('old-secret', 'new-secret-1')
    expect(send).toHaveBeenCalledTimes(1)
    expect(form.successMessage).toBe(CHANGE_PASSWORD_SUCCESS_MESSAGE)
    expect(form.errorMessage).toBe('')
    expect(form.lastErrorStatus).toBe(null)
    expect(form.busy).toBe(false)
    expect([form.oldPassword, form.newPassword, form.confirmPassword]).toEqual(['', '', ''])
  })

  it('400 失败（当前后端：原密码填错）：展示「原密码不正确」而非通用文案，并同样清空输入', async () => {
    tokenStorage.save('access-cp', 'refresh-cp')
    mockFetchOnce({ code: 400, message: '原密码不正确', data: null }, 400)
    const form = filledForm(authApi.changePassword)

    expect(await form.submit()).toBe(false)

    expect(form.errorMessage).toBe('原密码不正确')
    expect(form.errorMessage).not.toBe(CHANGE_PASSWORD_FAILURE_MESSAGE)
    expect(form.lastErrorStatus).toBe(400)
    // 400 不是登录态失效：共享客户端不会因此清空本地令牌。
    expect(tokenStorage.accessToken).toBe('access-cp')
    expect(form.successMessage).toBe('')
    expect(form.busy).toBe(false)
    expect([form.oldPassword, form.newPassword, form.confirmPassword]).toEqual(['', '', ''])
  })

  it('401 失败：同样展示后端「原密码不正确」而非通用文案，并标记为登录态失效', async () => {
    tokenStorage.save('access-cp', 'refresh-cp')
    mockFetchOnce({ code: 401, message: '原密码不正确', data: null }, 401)
    const form = filledForm(authApi.changePassword)

    expect(await form.submit()).toBe(false)

    expect(form.errorMessage).toBe('原密码不正确')
    expect(form.errorMessage).not.toBe(CHANGE_PASSWORD_FAILURE_MESSAGE)
    expect(form.lastErrorStatus).toBe(401)
    // 现有共享客户端对 401 的既有行为：清空本地令牌（页面据此回到登录引导）。
    expect(tokenStorage.accessToken).toBe('')
    expect(form.successMessage).toBe('')
    expect(form.busy).toBe(false)
    expect([form.oldPassword, form.newPassword, form.confirmPassword]).toEqual(['', '', ''])
  })

  it('其他失败（网络异常等）同样清空输入，非 Error 抛出时退回通用文案', async () => {
    const failing: ChangePasswordRequester = async () => { throw new Error('无法连接服务器，请确认网络或稍后重试') }
    const networkForm = filledForm(failing)
    expect(await networkForm.submit()).toBe(false)
    expect(networkForm.errorMessage).toBe('无法连接服务器，请确认网络或稍后重试')
    expect(networkForm.lastErrorStatus).toBe(null)
    expect([networkForm.oldPassword, networkForm.newPassword, networkForm.confirmPassword]).toEqual(['', '', ''])

    const throwing: ChangePasswordRequester = async () => { throw 'boom' }
    const oddForm = filledForm(throwing)
    expect(await oddForm.submit()).toBe(false)
    expect(oddForm.errorMessage).toBe(CHANGE_PASSWORD_FAILURE_MESSAGE)
    expect(oddForm.lastErrorStatus).toBe(null)
  })

  it('本地校验不通过时不发请求，且保留已填内容便于修正', async () => {
    const send = vi.fn<ChangePasswordRequester>(async () => ({ updated: true }))
    const form = filledForm(send)
    form.confirmPassword = 'another-secret'

    expect(await form.submit()).toBe(false)

    expect(send).not.toHaveBeenCalled()
    expect(form.errorMessage).toBe('两次输入的密码不一致')
    expect(form.oldPassword).toBe('old-secret')
    expect(form.newPassword).toBe('new-secret-1')
    expect(form.confirmPassword).toBe('another-secret')
  })

  it('请求进行中重复提交被挡住，只发出一次请求', async () => {
    let release: (value: { updated: boolean }) => void = () => {}
    const send = vi.fn<ChangePasswordRequester>(() => new Promise<{ updated: boolean }>((resolve) => { release = resolve }))
    const form = filledForm(send)

    const pending = form.submit()
    expect(form.busy).toBe(true)
    expect(await form.submit()).toBe(false)
    expect(send).toHaveBeenCalledTimes(1)

    release({ updated: true })
    expect(await pending).toBe(true)
    expect(send).toHaveBeenCalledTimes(1)
  })
})
