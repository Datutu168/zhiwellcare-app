import { tokenStorage } from './token'

/** 后端统一返回结构：code=0 成功。 */
export interface ApiEnvelope<T = unknown> {
  code: number
  message: string
  data: T
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

/** 后端地址：VITE_API_BASE 优先，默认本地开发后端。 */
export const API_BASE = (import.meta.env.VITE_API_BASE ?? 'http://127.0.0.1:8080').replace(/\/$/, '')

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  const accessToken = tokenStorage.accessToken
  if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`)

  let response: Response
  try {
    response = await fetch(`${API_BASE}${path}`, { ...init, headers })
  } catch {
    throw new ApiError(0, '无法连接服务器，请确认网络或稍后重试')
  }

  let envelope: ApiEnvelope | null = null
  try { envelope = (await response.json()) as ApiEnvelope } catch { /* 非 JSON 响应 */ }

  if (!response.ok || (envelope !== null && envelope.code !== 0)) {
    const message = envelope?.message || `请求失败（${response.status}）`
    if (response.status === 401) tokenStorage.clear()
    throw new ApiError(response.status, message)
  }
  return envelope!.data as T
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'POST', body: JSON.stringify(body ?? {}) }),
  patch: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PATCH', body: JSON.stringify(body ?? {}) }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PUT', body: JSON.stringify(body ?? {}) }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
}
