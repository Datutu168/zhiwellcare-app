import { courseCategories } from '../../data/courses'
import { mallCategories } from '../../data/mall'
import type { GoodsCategoryOption } from './ContentTypes'

/**
 * 内容分类元数据（纯展示维度，后端契约不提供）。
 *
 * 课程分类与商城分类沿用内置数据的口径，保证 http 模式下筛选 chips/tabs 与离线完全一致。
 * 远端新增内容若品类/分类未知（后端契约不提供该维度），则只出现在「全部」中，
 * 不会被错误塞进某个分类，也不会把服务误标成硬件。
 */

/** 首页课程分类 chips，首项为「全部」。 */
export function listCourseCategories(): string[] {
  return [...courseCategories]
}

/** 商城分类 tabs（全部 / 硬件器材 / 训练服务）。 */
export function listGoodsCategories(): GoodsCategoryOption[] {
  return mallCategories.map((category) => ({ ...category }))
}
