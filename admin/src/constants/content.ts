/**
 * 「教程课程」与「商品」管理页共用的常量与纯函数。
 *
 * 两条口径：
 * 1) 后台不做文件上传：封面/视频/详情地址一律由运营把 COS 地址粘进来，这里只做
 *    `https://` 前缀 + 空格的基础校验（真实可访问性由后端与客户端保证）。
 * 2) 价格以后端 `priceCents`（分，整数）为权威字段，界面按「元」输入，
 *    换算全程走整数运算，避免 19.9 * 100 = 1989.9999… 这类浮点误差。
 */
import type { FormItemRule } from 'element-plus'

/** 课程难度建议项（后端 level 为自由字符串，故下拉允许自定义输入）。 */
export const COURSE_LEVEL_SUGGESTIONS: string[] = ['入门', '进阶', '强化', '康复基础']

const LEVEL_LABELS: Record<string, string> = {
  beginner: '入门',
  basic: '入门',
  elementary: '入门',
  intermediate: '进阶',
  medium: '进阶',
  advanced: '强化',
  high: '强化',
}

/** 难度展示：已知英文码翻译成中文，未知值原样展示。 */
export function levelLabel(level?: string): string {
  const value = (level ?? '').trim()
  if (!value) return '—'
  return LEVEL_LABELS[value.toLowerCase()] ?? value
}

/** 上下架状态展示。 */
export function contentStatusLabel(status: string): string {
  return status === 'on' ? '已上架' : status === 'off' ? '已下架' : status || '—'
}

/**
 * URL 基础校验：必须是非空、无空格的 `https://` 地址。
 * 后台不做上传，允许留空（由调用方通过 required 决定是否必填）。
 */
export function isHttpsUrl(value: string): boolean {
  const text = (value ?? '').trim()
  return /^https:\/\/[^\s]+$/i.test(text)
}

/**
 * URL 字段的中文错误提示：合法（或留空且非必填）返回 ''。
 * 规则校验（失焦即时提示）与提交前的兜底校验共用同一份文案，避免两处写歪。
 */
export function urlError(value: string, label: string, required = false): string {
  const text = (value ?? '').trim()
  if (!text) return required ? `请填写${label}` : ''
  if (!/^https:\/\//i.test(text)) return `${label}需以 https:// 开头（如 https://cos.example.com/a.png）`
  if (/\s/.test(text)) return `${label}不能包含空格，请检查是否粘贴了多余字符`
  return ''
}

/** 必填文本的中文错误提示。 */
export function requiredError(value: string, label: string): string {
  return (value ?? '').trim() ? '' : `请填写${label}`
}

/** 内容主键（courseId / goodsId）的格式约束与提示。 */
export const CONTENT_ID_PATTERN = /^[a-z0-9][a-z0-9-]{1,63}$/
export const CONTENT_ID_HINT = '以小写字母或数字开头，仅含小写字母/数字/连字符，共 2-64 位'

export function contentIdError(value: string, label: string): string {
  const text = (value ?? '').trim()
  if (!text) return `请输入${label}`
  if (!CONTENT_ID_PATTERN.test(text)) return `${label}需${CONTENT_ID_HINT}`
  return ''
}

/** 排序字段（非负整数）的中文错误提示。 */
export function sortError(value: number, label = '排序'): string {
  return Number.isInteger(value) && value >= 0 ? '' : `${label}需为不小于 0 的整数`
}

/**
 * Element Plus 表单校验器工厂：URL 字段的 `https://` 前缀校验 + 中文提示。
 * 用法：`{ coverUrl: [httpsUrlRule({ label: '封面地址' })] }`
 *
 * 注意：本套 UI 在提交前另有一层同步校验（见各页面的 validateDialog），
 * 因为 el-form 的 validate() 在某些 Element Plus 版本/环境下会静默放行，
 * 不能作为唯一防线。
 */
export function httpsUrlRule(options: { label: string; required?: boolean }): FormItemRule {
  const { label, required = false } = options
  return {
    trigger: 'blur',
    validator: (_rule: unknown, value: unknown, callback: (error?: string | Error) => void) => {
      const message = urlError(typeof value === 'string' ? value : '', label, required)
      if (message) callback(new Error(message))
      else callback()
    },
  }
}

/**
 * 「元」输入校验：非负、最多两位小数（最多 9 位整数，够用且不溢出）。
 * 第 1 个捕获组是整数部分、第 2 个是小数部分（parseYuanToCents 依赖该分组）。
 */
export const YUAN_PATTERN = /^(\d{1,9})(?:\.(\d{1,2}))?$/

/** 「元」字段的中文错误提示：合法返回 ''。 */
export function yuanError(value: string, label = '价格'): string {
  const text = (value ?? '').trim()
  if (!text) return `请填写${label}（单位：元）`
  if (!YUAN_PATTERN.test(text)) return `${label}只能是数字，最多两位小数（如 19.90）`
  return ''
}

export function yuanRule(options: { label?: string; required?: boolean } = {}): FormItemRule {
  const { label = '价格', required = true } = options
  return {
    trigger: 'blur',
    validator: (_rule: unknown, value: unknown, callback: (error?: string | Error) => void) => {
      const text = typeof value === 'string' ? value : String(value ?? '')
      const message = required || text.trim() ? yuanError(text, label) : ''
      if (message) callback(new Error(message))
      else callback()
    },
  }
}

/**
 * 「元」字符串 → 「分」整数。非法输入返回 null。
 * 19.90 → 1990，0.07 → 7，1.1 → 110；全程字符串取位 + 整数乘法，无浮点误差。
 */
export function parseYuanToCents(input: string): number | null {
  const text = String(input ?? '').trim()
  if (!text) return null
  const matched = YUAN_PATTERN.exec(text)
  if (!matched) return null
  const yuan = Number(matched[1])
  const cents = Number((matched[2] ?? '').padEnd(2, '0'))
  return yuan * 100 + cents
}

/** 「分」整数 → 编辑框回填用的「元」字符串（1990 → "19.90"）。 */
export function centsToYuanText(cents: number | null | undefined): string {
  if (cents === null || cents === undefined || !Number.isFinite(Number(cents))) return ''
  const value = Math.trunc(Number(cents))
  const sign = value < 0 ? '-' : ''
  const abs = Math.abs(value)
  return `${sign}${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, '0')}`
}

/** 「分」整数 → 展示文案（1234 → "¥12.34"；空值 → "—"）。 */
export function formatCents(cents: number | null | undefined): string {
  const text = centsToYuanText(cents)
  return text ? `¥${text}` : '—'
}

/**
 * 列表价格展示：优先后端 `priceLabel`，为空时按 `priceCents` 自行换算。
 * 后端两个字段都缺失时返回「—」，不会显示 NaN。
 */
export function priceText(priceLabel?: string, priceCents?: number | null): string {
  const label = (priceLabel ?? '').trim()
  if (label) return label
  return formatCents(priceCents)
}
