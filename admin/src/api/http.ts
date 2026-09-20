import { adminToken } from './token'

export interface Envelope<T = unknown> { code: number; message: string; data: T }

export class ApiError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  const token = adminToken.get()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  let response: Response
  try {
    response = await fetch(`/api/v1${path}`, { ...init, headers })
  } catch {
    throw new ApiError(0, '无法连接后端服务，请确认后端已启动')
  }
  let envelope: Envelope | null = null
  try { envelope = (await response.json()) as Envelope } catch { /* ignore */ }
  if (!response.ok || (envelope !== null && envelope.code !== 0)) {
    if (response.status === 401) adminToken.clear()
    throw new ApiError(response.status, envelope?.message || `请求失败（${response.status}）`)
  }
  // 兼容 204 / 空响应体（如 DELETE 成功无 body），避免 envelope 为 null 时抛 TypeError 导致页面白屏。
  if (envelope === null) return undefined as T
  return envelope.data as T
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) => request<T>(path, { method: 'POST', body: JSON.stringify(body ?? {}) }),
  put: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PUT', body: JSON.stringify(body ?? {}) }),
  patch: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PATCH', body: JSON.stringify(body ?? {}) }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
}
