import { api } from './http'

/** 登录用户视图（与后端 model.SafeUser 对齐）。 */
export interface SafeUser {
  id: string
  phoneMasked: string
  nickname: string
  avatarUrl?: string
  role: string
  createdAt: string
}

export interface TokenPair {
  accessToken: string
  refreshToken: string
  expiresInSeconds: number
}

export interface AuthPayload {
  user: SafeUser
  tokens: TokenPair
}

export function register(phone: string, password: string, nickname: string): Promise<AuthPayload> {
  return api.post<AuthPayload>('/api/v1/auth/register', { phone, password, nickname })
}

export function login(phone: string, password: string): Promise<AuthPayload> {
  return api.post<AuthPayload>('/api/v1/auth/login', { phone, password })
}

export function refresh(refreshToken: string): Promise<AuthPayload> {
  return api.post<AuthPayload>('/api/v1/auth/refresh', { refreshToken })
}

export function logout(refreshToken: string): Promise<{ loggedOut: boolean }> {
  return api.post<{ loggedOut: boolean }>('/api/v1/auth/logout', { refreshToken })
}

export function fetchMe(): Promise<SafeUser> {
  return api.get<SafeUser>('/api/v1/me')
}

export function updateMe(nickname: string): Promise<SafeUser> {
  return api.patch<SafeUser>('/api/v1/me', { nickname })
}

/** 修改密码结果（后端统一信封 data）。 */
export interface PasswordChangeResult {
  updated: boolean
}

/**
 * 修改本人密码：需要登录令牌（与 fetchMe 同一套鉴权）。
 * 后端限制：改密成功后不会撤销已签发的 access/refresh 令牌（JWT claims 里没有密码版本），
 * 其他已登录设备不会被强制下线，需等令牌自然过期后才失效；界面文案与此保持一致。
 * 原密码错误时后端返回 400 与中文 message「原密码不正确」（刻意不用 401，避免客户端把 401
 * 当成登录态失效而清掉本地令牌）；调用方统一取 ApiError.message 作为提示。
 * 新密码长度按注册口令规则校验（6-64 位）。
 */
export function changePassword(oldPassword: string, newPassword: string): Promise<PasswordChangeResult> {
  return api.put<PasswordChangeResult>('/api/v1/me/password', { oldPassword, newPassword })
}
