import { courses as builtinCourses } from '../../data/courses'
import { mallGoods as builtinGoods } from '../../data/mall'
import type { ContentStatus, CourseItem, CourseStep, GoodsKind, MallGoodsItem } from './ContentTypes'

/**
 * 内容归一化：把「内置数据」与「后端接口返回」统一收敛成页面可直接渲染的结构。
 *
 * 容错原则（与 CatalogService 一致：宁可降级也不崩）：
 *  - 缺 id/标题的条目直接丢弃，不影响其余内容；
 *  - 字段类型异常（数字给成字符串、数组给成 null 等）一律取安全默认值；
 *  - 远端未提供的展示字段（分类、emoji 封面、步骤明细等）优先用同 id 的内置详情补齐，
 *    这样后端只下发契约字段时页面依然完整。
 */

/** 课程 emoji 封面兜底（内置数据总有封面；此处仅兜远端新增课程）。 */
const DEFAULT_COURSE_COVER = '📚'
/** 远端课程未给分类时的展示分类（首页筛选 chips 无该项，只在「全部」下出现）。 */
const DEFAULT_COURSE_CATEGORY = '其他'
/** 远端课程未给任何时长信息时的展示文案。 */
const DEFAULT_DURATION_LABEL = '时长待定'
/** 商品 emoji 封面兜底。 */
const DEFAULT_GOODS_COVER = '📦'
/** 价格缺失（既无 priceLabel 也无 priceCents）时的展示文案。 */
const DEFAULT_PRICE_LABEL = '价格待定'

const builtinCourseById = new Map(builtinCourses.map((course) => [course.id, course] as const))
const builtinGoodsById = new Map(builtinGoods.map((goods) => [goods.id, goods] as const))

/**
 * 分 → 展示价格文案（纯整数运算，避免 0.1+0.2 类浮点误差）。
 * 入参先四舍五入到整数分（容忍 8.7*100 = 869.999… 这类中间结果），再做整数除/取余。
 * 例：39900 → '¥399'，39950 → '¥399.50'，1 → '¥0.01'，0 → '¥0'。
 */
export function formatPriceCents(cents: number): string {
  const safe = Number.isFinite(cents) ? Math.round(cents) : 0
  const sign = safe < 0 ? '-' : ''
  const abs = Math.abs(safe)
  const yuan = Math.floor(abs / 100)
  const fen = abs % 100
  return fen === 0 ? `${sign}¥${yuan}` : `${sign}¥${yuan}.${`${fen}`.padStart(2, '0')}`
}

/** 价格文案：优先用后端 priceLabel；为空时由 priceCents（分）换算；都不可用时给「价格待定」。 */
export function resolvePriceLabel(label: unknown, cents: number | null): string {
  const text = asText(label)
  if (text) return text
  if (cents === null || !Number.isFinite(cents) || cents < 0) return DEFAULT_PRICE_LABEL
  return formatPriceCents(cents)
}

/** 单条课程归一化；缺少 courseId 或标题时返回 null（该条被丢弃，不影响其余数据）。 */
export function normalizeCourse(raw: unknown, fallbackSort = 0): CourseItem | null {
  const item = asRecord(raw)
  if (!item) return null
  const courseId = asId(item.courseId) || asId(item.id)
  const title = asText(item.title)
  if (!courseId || !title) return null

  const builtin = builtinCourseById.get(courseId)
  const durationMin = asInt(item.durationMin) ?? builtin?.durationMin ?? 0
  return {
    courseId,
    title,
    summary: asText(item.summary) || builtin?.summary || '',
    coverUrl: asText(item.coverUrl),
    videoUrl: asText(item.videoUrl),
    durationLabel: asText(item.durationLabel) || (durationMin > 0 ? `约 ${durationMin} 分钟` : DEFAULT_DURATION_LABEL),
    level: asText(item.level) || builtin?.level || '',
    tags: asTextList(item.tags, builtin?.tags ?? []),
    status: asStatus(item.status),
    sort: asInt(item.sort) ?? fallbackSort,
    category: asText(item.category) || builtin?.category || DEFAULT_COURSE_CATEGORY,
    durationMin,
    cover: asText(item.cover) || builtin?.cover || DEFAULT_COURSE_COVER,
    steps: asSteps(item.steps) ?? (builtin ? builtin.steps.map((step) => ({ ...step })) : []),
    disclaimer: asText(item.disclaimer) || builtin?.disclaimer || '',
  }
}

/** 课程列表归一化；data 不是数组时返回空列表（不抛错）。 */
export function normalizeCourseList(data: unknown): CourseItem[] {
  if (!Array.isArray(data)) return []
  return data
    .map((raw, index) => normalizeCourse(raw, index))
    .filter((course): course is CourseItem => course !== null)
}

/** 单条商品归一化；缺少 goodsId 或名称时返回 null。 */
export function normalizeGoods(raw: unknown, fallbackSort = 0): MallGoodsItem | null {
  const item = asRecord(raw)
  if (!item) return null
  const goodsId = asId(item.goodsId) || asId(item.id)
  const name = asText(item.name)
  if (!goodsId || !name) return null

  const builtin = builtinGoodsById.get(goodsId)
  const kindFromData = asKind(item.kind) ?? builtin?.kind ?? null
  const kind: GoodsKind = kindFromData ?? 'hardware'
  const priceCents = asInt(item.priceCents)
  return {
    goodsId,
    name,
    summary: asText(item.summary) || builtin?.summary || '',
    priceCents: priceCents ?? 0,
    priceLabel: resolvePriceLabel(item.priceLabel, priceCents),
    coverUrl: asText(item.coverUrl),
    detailUrl: asText(item.detailUrl),
    specs: asTextList(item.specs, builtin?.spec ?? []),
    status: asStatus(item.status),
    sort: asInt(item.sort) ?? fallbackSort,
    kind,
    // 品类未知时用中性文案：分类「商品」、单位「件」，页面不写成硬件/服务。
    kindKnown: kindFromData !== null,
    category: asText(item.category) || builtin?.category || (kindFromData ? defaultCategory(kind) : '商品'),
    priceUnit: asText(item.priceUnit) || builtin?.priceUnit || (kindFromData ? defaultPriceUnit(kind) : '件'),
    cover: asText(item.cover) || builtin?.cover || DEFAULT_GOODS_COVER,
    badges: asTextList(item.badges, builtin?.badges ?? []),
    detail: asTextList(item.detail, builtin?.detail ?? []),
    disclaimer: asText(item.disclaimer) || builtin?.disclaimer || '',
    relatedHardware: asBool(item.relatedHardware) ?? builtin?.relatedHardware ?? false,
  }
}

/** 商品列表归一化；data 不是数组时返回空列表（不抛错）。 */
export function normalizeGoodsList(data: unknown): MallGoodsItem[] {
  if (!Array.isArray(data)) return []
  return data
    .map((raw, index) => normalizeGoods(raw, index))
    .filter((goods): goods is MallGoodsItem => goods !== null)
}

/** 内置商品价格（元）→ 分；内置价为整数演示价，四舍五入消除乘法误差。 */
export function yuanToCents(price: number): number {
  return Number.isFinite(price) ? Math.round(price * 100) : 0
}

function defaultCategory(kind: GoodsKind): string {
  return kind === 'service' ? '训练服务' : '硬件器材'
}

function defaultPriceUnit(kind: GoodsKind): string {
  return kind === 'service' ? '次' : '台'
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : null
}

function asText(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

/** id 允许字符串或数字（后端 JSON 编号为数字时同样可用）。 */
function asId(value: unknown): string {
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  return ''
}

/** 整数字段：数字取整，纯数字字符串解析；其余（null/NaN/对象）视为缺失。 */
function asInt(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return Math.trunc(value)
  if (typeof value === 'string' && /^-?\d+$/.test(value.trim())) return Number.parseInt(value.trim(), 10)
  return null
}

function asBool(value: unknown): boolean | null {
  return typeof value === 'boolean' ? value : null
}

/** 上架状态：只认 'off'/false/0 为下架，缺失或异常一律按上架（服务端本就只下发上架项）。 */
function asStatus(value: unknown): ContentStatus {
  return value === 'off' || value === false || value === 0 ? 'off' : 'on'
}

function asKind(value: unknown): GoodsKind | null {
  return value === 'hardware' || value === 'service' ? value : null
}

/** 字符串数组：过滤非字符串与空串，去空白；非数组时用内置兜底。 */
function asTextList(value: unknown, fallback: readonly string[]): string[] {
  if (!Array.isArray(value)) return [...fallback]
  return value
    .filter((entry): entry is string => typeof entry === 'string')
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0)
}

/** 动作步骤：只保留 title/detail 均为字符串的条目；非数组时返回 null 交由内置兜底。 */
function asSteps(value: unknown): CourseStep[] | null {
  if (!Array.isArray(value)) return null
  return value.flatMap((entry) => {
    const step = asRecord(entry)
    if (!step) return []
    const title = asText(step.title)
    const detail = asText(step.detail)
    return title || detail ? [{ title, detail }] : []
  })
}
