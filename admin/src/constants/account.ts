/**
 * 「修改密码」等个人账号操作的常量与纯函数。
 *
 * 口径与后端 `PUT /me/password` 契约保持一致：
 * - 新密码长度 6-64 位（后端会返回 400 + 中文长度提示）；
 * - 新密码不得与原密码相同（后端返回 400「新密码不能与原密码相同」）。
 *
 * 这里的同步校验只是为了省掉一次往返、让用户在弹窗里就得到提示，
 * 权威校验始终在后端（前端提示不改变后端判定）。
 */
import type { FormItemRule } from 'element-plus'
import { requiredError } from './content'

/** 新密码长度区间（与后端一致，改动需同步后端）。 */
export const PASSWORD_MIN_LENGTH = 6
export const PASSWORD_MAX_LENGTH = 64

/** 改密表单的三个口令字段（口令不 trim：空格也是合法字符）。 */
export interface PasswordChangeForm {
  oldPassword: string
  newPassword: string
  confirmPassword: string
}

/** 空改密表单（弹窗每次打开 / 关闭时重置，避免明文口令留在页面上）。 */
export function emptyPasswordChangeForm(): PasswordChangeForm {
  return { oldPassword: '', newPassword: '', confirmPassword: '' }
}

/** 新密码长度提示：合法返回 ''。 */
export function newPasswordError(value: string, label = '新密码'): string {
  const text = value ?? ''
  if (text.length < PASSWORD_MIN_LENGTH || text.length > PASSWORD_MAX_LENGTH) {
    return `${label}长度需为 ${PASSWORD_MIN_LENGTH}-${PASSWORD_MAX_LENGTH} 位`
  }
  return ''
}

/** 确认新密码与新密码的一致性提示：合法返回 ''。 */
export function confirmPasswordError(newPassword: string, confirmPassword: string): string {
  if (!(confirmPassword ?? '').trim()) return '请填写确认新密码'
  return newPassword === confirmPassword ? '' : '两次输入的确认新密码与新密码不一致'
}

/**
 * 改密提交前的同步校验：返回第一条中文提示，合法返回 ''。
 * 顺序：必填 → 新密码长度 → 新旧必须不同 → 两次输入一致。
 */
export function passwordChangeError(form: PasswordChangeForm): string {
  const oldPassword = form?.oldPassword ?? ''
  const newPassword = form?.newPassword ?? ''
  const confirmPassword = form?.confirmPassword ?? ''
  return (
    requiredError(oldPassword, '原密码') ||
    requiredError(newPassword, '新密码') ||
    requiredError(confirmPassword, '确认新密码') ||
    newPasswordError(newPassword) ||
    // 长度合法后再比对新旧：先报「太短」比先报「与原密码相同」更有指导性
    (newPassword === oldPassword ? '新密码不能与原密码相同' : '') ||
    confirmPasswordError(newPassword, confirmPassword) ||
    ''
  )
}

/**
 * el-form 单字段规则工厂：与 `passwordChangeError` 共用同一份中文文案。
 * 失焦时只提示与该字段相关的错误（其余字段为空产生的必填提示留到提交时统一给）。
 *
 * 用法：`{ newPassword: [passwordRule('newPassword', form)] }`（form 为 reactive 对象，按引用读取当前值）
 */
export function passwordRule(field: keyof PasswordChangeForm, form: PasswordChangeForm): FormItemRule {
  const messageOf = (): string => {
    if (field === 'oldPassword') return requiredError(form.oldPassword, '原密码')
    if (field === 'newPassword') return requiredError(form.newPassword, '新密码') || newPasswordError(form.newPassword)
    return confirmPasswordError(form.newPassword, form.confirmPassword)
  }
  return {
    trigger: 'blur',
    validator: (_rule: unknown, _value: unknown, callback: (error?: string | Error) => void) => {
      const message = messageOf()
      if (message) callback(new Error(message))
      else callback()
    },
  }
}
