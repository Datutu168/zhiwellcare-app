import type { CourseItem, MallGoodsItem } from './ContentTypes'

/**
 * 内容源抽象：统一「内置内容」与「Golang 后端内容接口」两种来源。
 * 页面只依赖 ContentService，后端就绪后切换 VITE_CATALOG_MODE=http 即可，无需改任何页面。
 */
export interface IContentSource {
  readonly kind: 'mock' | 'http'

  /** 课程/教程列表（服务端已只返回上架项并按 sort 升序）。 */
  loadCourses(): Promise<CourseItem[]>

  /** 商城商品列表（服务端已只返回上架项并按 sort 升序）。 */
  loadGoods(): Promise<MallGoodsItem[]>
}
