/**
 * 内容（课程/教程 + 商城商品）类型定义。
 *
 * 数据来源二选一，与设备目录共用同一个开关，无需新增环境变量：
 *  - `VITE_CATALOG_MODE=mock`（默认）：内置 src/data/courses.ts / src/data/mall.ts，离线演示与后端兜底
 *  - `VITE_CATALOG_MODE=http`：Golang 后端 /api/v1/catalog/courses 与 /api/v1/catalog/goods
 * 后端地址复用 `VITE_API_BASE`（见 src/api/http.ts 的 API_BASE，未配置时默认本机 8080）。
 *
 * 结构说明：CourseItem / MallGoodsItem 以「后端契约字段」为主体，
 * 另带一组**归一化展示字段**（category / cover / steps / priceUnit 等）——
 * 远端不返回这些字段时，由内置详情（同 id 命中）或安全默认值补齐，
 * 保证 http 模式下的页面结构、离线时的界面呈现完全不变。
 */

/** 内容快照版本；服务端结构升级时递增（内置数据同步维护）。 */
export const CONTENT_SCHEMA_VERSION = 1

/** 上下架状态：后端只下发上架项（'on'），前端仍按此字段兜底过滤。 */
export type ContentStatus = 'on' | 'off'

/** 内容源来源标识（UI 可展示“内置内容 / 后端内容”）。 */
export type ContentSourceKind = 'mock' | 'http'

/** 内容源状态（含后端失败后回退内置的情形）。 */
export type ContentSourceStatusKind = ContentSourceKind | 'http-fallback-mock'

/** 单节动作：title 动作名，detail 动作要领说明。 */
export interface CourseStep {
  title: string
  detail: string
}

/** 课程项（源自 GET /api/v1/catalog/courses）。 */
export interface CourseItem {
  /* ── 后端契约字段 ────────────────────────────────────────── */
  courseId: string
  title: string
  summary: string
  /** 封面图地址（远端下发；为空时用内置 emoji 占位）。 */
  coverUrl: string
  /** 视频资源地址（契约字段，当前页面仍为图文跟练，不做播放）。 */
  videoUrl: string
  /** 展示用时长文案，如「约 8 分钟」。 */
  durationLabel: string
  level: string
  tags: string[]
  status: ContentStatus
  /** 服务端排序权重（升序）。 */
  sort: number

  /* ── 归一化展示字段（远端缺失时由内置详情/默认值补齐） ────── */
  /** 课程分类（首页筛选 chips 用）。 */
  category: string
  /** 预计时长（分钟）；0 表示未知。 */
  durationMin: number
  /** 内置 emoji 封面占位（coverUrl 为空时显示）。 */
  cover: string
  steps: CourseStep[]
  /** 本节专属运动提示；为空时不渲染该段。 */
  disclaimer: string
}

/** 商品品类（与 src/data/mall.ts 的 GoodsKind 同构）。 */
export type GoodsKind = 'hardware' | 'service'

/** 商城商品项（源自 GET /api/v1/catalog/goods）。 */
export interface MallGoodsItem {
  /* ── 后端契约字段 ────────────────────────────────────────── */
  goodsId: string
  name: string
  summary: string
  /** 价格（分）：前端展示一律由整数分换算，避免浮点误差。 */
  priceCents: number
  /** 展示用价格文案；后端为空时由 priceCents 换算。 */
  priceLabel: string
  coverUrl: string
  detailUrl: string
  specs: string[]
  status: ContentStatus
  sort: number

  /* ── 归一化展示字段（远端缺失时由内置详情/默认值补齐） ────── */
  kind: GoodsKind
  /**
   * 品类（硬件/服务）是否来自数据本身。
   * 远端契约未提供品类字段：为 false 时文案一律走中性表述，
   * 避免把「上门服务」误标成「消费级硬件」（合规口径）。
   */
  kindKnown: boolean
  category: string
  /** 计价单位，如 '台' / '次' / '套'。 */
  priceUnit: string
  /** 内置 emoji 封面占位（coverUrl 为空时显示）。 */
  cover: string
  badges: string[]
  detail: string[]
  /** 专属免责文案；为空时不渲染该段。 */
  disclaimer: string
  /** 是否需搭配「桌面力矩主动训练底座」使用（手柄配件等）。 */
  relatedHardware: boolean
}

/** 内容总体快照：课程 + 商品一次取齐（与 CatalogSnapshot 同构）。 */
export interface ContentSnapshot {
  schemaVersion: number
  courses: CourseItem[]
  goods: MallGoodsItem[]
}

/** 商城分类 tab（首页筛选用；后端不提供分类维度，属内置展示元数据）。 */
export interface GoodsCategoryOption {
  id: 'all' | 'hardware' | 'service'
  label: string
}
