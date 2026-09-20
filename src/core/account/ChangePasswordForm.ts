/**
 * 修改密码表单：字段、校验与提交编排集中在这里，页面只负责绑定与展示。
 * 不含任何 DOM 依赖，便于单测直接驱动「校验失败 / 请求成功 / 请求失败」三条路径。
 */
import { ApiError } from '../../api/http'

/** 与后端注册口令规则一致：新密码 6-64 位。 */
export const CHANGE_PASSWORD_MIN_LENGTH = 6
export const CHANGE_PASSWORD_MAX_LENGTH = 64

/** 成功提示：后端不会撤销已签发的令牌，这里如实告知用户其他设备的登录状态。 */
export const CHANGE_PASSWORD_SUCCESS_MESSAGE = '密码修改成功。其他已登录设备不会立即退出，将在登录令牌过期后失效。'

/** 通用失败提示：拿不到后端中文 message 时的兜底文案。 */
export const CHANGE_PASSWORD_FAILURE_MESSAGE = '修改密码失败，请稍后重试'

/** 修改密码请求的入参（与 PUT /api/v1/me/password 的字段名一致）。 */
export interface ChangePasswordFields {
  oldPassword: string
  newPassword: string
  confirmPassword: string
}

/** 提交函数签名：与 src/api/auth.ts 的 changePassword 对齐，便于单测注入桩。 */
export type ChangePasswordRequester = (
  oldPassword: string,
  newPassword: string,
) => Promise<{ updated: boolean }>

/** 客户端校验：通过返回空串，不通过返回中文提示（文案与后端 400 错误的 message 完全一致）。 */
export function validateChangePassword(fields: ChangePasswordFields): string {
  if (!fields.oldPassword) return '请输入原密码'
  if (!fields.newPassword) return '请输入新密码'
  if (fields.newPassword.length < CHANGE_PASSWORD_MIN_LENGTH) return `密码至少 ${CHANGE_PASSWORD_MIN_LENGTH} 位`
  if (fields.newPassword.length > CHANGE_PASSWORD_MAX_LENGTH) return `密码最长 ${CHANGE_PASSWORD_MAX_LENGTH} 位`
  if (!fields.confirmPassword) return '请再次输入新密码'
  if (fields.newPassword !== fields.confirmPassword) return '两次输入的密码不一致'
  if (fields.newPassword === fields.oldPassword) return '新密码不能与原密码相同'
  return ''
}

/** 失败提示：后端返回的中文 message 优先，取不到时退回通用文案。 */
export function resolveChangePasswordError(error: unknown): string {
  if (error instanceof Error && error.message.trim() !== '') return error.message
  return CHANGE_PASSWORD_FAILURE_MESSAGE
}

/**
 * 表单状态机：busy 在请求期间置位（禁用按钮并挡住重复提交），
 * 无论请求成功还是失败都清空三个输入框，避免密码明文长时间停留在界面上。
 */
export class ChangePasswordForm {
  oldPassword = ''
  newPassword = ''
  confirmPassword = ''
  /** 请求进行中：提交按钮据此禁用，同时用于阻止重复提交。 */
  busy = false
  /** 上一次提交的失败提示（后端中文 message 优先，无则通用文案）。 */
  errorMessage = ''
  /** 上一次提交的成功提示。 */
  successMessage = ''
  /**
   * 上一次失败的 HTTP 状态码（网络不可达为 0，未失败或非 ApiError 为 null）；
   * 页面据此判断是否为登录态失效（401）并交给 store 现有逻辑清理会话。
   */
  lastErrorStatus: number | null = null

  constructor(private readonly send: ChangePasswordRequester) {}

  /** 提交：先本地校验，再请求后端；返回是否成功（供页面决定是否返回「我的」）。 */
  async submit(): Promise<boolean> {
    if (this.busy) return false
    this.errorMessage = ''
    this.successMessage = ''
    this.lastErrorStatus = null
    const invalid = validateChangePassword(this)
    if (invalid) {
      // 校验没过时保留已填内容，方便用户就地修正。
      this.errorMessage = invalid
      return false
    }
    this.busy = true
    try {
      await this.send(this.oldPassword, this.newPassword)
      this.successMessage = CHANGE_PASSWORD_SUCCESS_MESSAGE
      return true
    } catch (error) {
      this.errorMessage = resolveChangePasswordError(error)
      this.lastErrorStatus = error instanceof ApiError ? error.status : null
      return false
    } finally {
      this.clearFields()
      this.busy = false
    }
  }

  /** 清空原密码 / 新密码 / 确认新密码三个输入框。 */
  clearFields(): void {
    this.oldPassword = ''
    this.newPassword = ''
    this.confirmPassword = ''
  }
}
