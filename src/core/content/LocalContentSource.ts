import { courses as builtinCourses } from '../../data/courses'
import type { Course } from '../../data/courses'
import { mallGoods as builtinGoods } from '../../data/mall'
import type { Goods } from '../../data/mall'
import type { IContentSource } from './IContentSource'
import type { CourseItem, MallGoodsItem } from './ContentTypes'
import { normalizeCourseList, normalizeGoodsList, yuanToCents } from './normalize'

/**
 * 内置内容源：数据源位于 src/data/courses.ts 与 src/data/mall.ts。
 *
 * 定位：离线演示 + 后端不可用时的兜底。内置数据先被改写成「与后端契约同构」的原始对象，
 * 再走与远端数据完全相同的归一化链路，保证两条来源产出的结构、文案口径一致。
 */
export class LocalContentSource implements IContentSource {
  readonly kind = 'mock' as const

  async loadCourses(): Promise<CourseItem[]> {
    return normalizeCourseList(builtinCourses.map((course, index) => courseToRaw(course, index)))
  }

  async loadGoods(): Promise<MallGoodsItem[]> {
    return normalizeGoodsList(builtinGoods.map((goods, index) => goodsToRaw(goods, index)))
  }
}

/** 内置课程 → 后端契约同构的原始对象（含展示字段，供归一化直接读取）。 */
function courseToRaw(course: Course, sort: number): Record<string, unknown> {
  return {
    courseId: course.id,
    title: course.title,
    summary: course.summary,
    coverUrl: '',
    videoUrl: '',
    durationLabel: `约 ${course.durationMin} 分钟`,
    level: course.level,
    tags: [...course.tags],
    status: 'on',
    sort,
    category: course.category,
    durationMin: course.durationMin,
    cover: course.cover,
    steps: course.steps.map((step) => ({ ...step })),
    disclaimer: course.disclaimer ?? '',
  }
}

/** 内置商品 → 后端契约同构的原始对象；演示价（整数元）换算为分。 */
function goodsToRaw(goods: Goods, sort: number): Record<string, unknown> {
  return {
    goodsId: goods.id,
    name: goods.name,
    summary: goods.summary,
    priceCents: yuanToCents(goods.price),
    priceLabel: '',
    coverUrl: '',
    detailUrl: '',
    specs: [...goods.spec],
    status: 'on',
    sort,
    kind: goods.kind,
    category: goods.category,
    priceUnit: goods.priceUnit,
    cover: goods.cover,
    badges: [...goods.badges],
    detail: [...goods.detail],
    disclaimer: goods.disclaimer,
    relatedHardware: goods.relatedHardware ?? false,
  }
}
