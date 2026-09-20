import { request } from '../../api/http'
import type { IContentSource } from './IContentSource'
import type { CourseItem, MallGoodsItem } from './ContentTypes'
import { normalizeCourseList, normalizeGoodsList } from './normalize'

/** 课程/商品接口路径（相对于 VITE_API_BASE，由 src/api/http.ts 的 request 拼装）。 */
export const CONTENT_COURSES_PATH = '/api/v1/catalog/courses'
export const CONTENT_GOODS_PATH = '/api/v1/catalog/goods'

/**
 * 内容接口请求器：默认复用 src/api/http.ts 的 request ——
 * 同一 VITE_API_BASE、统一信封 `{code,message,data}` 解析、非 2xx 抛 ApiError。
 * 注入点仅用于测试（断言请求路径与模拟失败），生产代码不要传入。
 */
export type ContentRequester = <T>(path: string) => Promise<T>

const defaultRequester: ContentRequester = <T>(path: string) => request<T>(path)

/**
 * Golang 后端内容源（HTTP，公开接口，无需登录）：
 * - GET {API_BASE}/api/v1/catalog/courses
 * - GET {API_BASE}/api/v1/catalog/goods
 * 服务端只返回上架项并按 sort 升序；返回体一律经归一化容错处理。
 */
export class HttpContentSource implements IContentSource {
  readonly kind = 'http' as const

  constructor(private readonly send: ContentRequester = defaultRequester) {}

  async loadCourses(): Promise<CourseItem[]> {
    return normalizeCourseList(await this.send<unknown>(CONTENT_COURSES_PATH))
  }

  async loadGoods(): Promise<MallGoodsItem[]> {
    return normalizeGoodsList(await this.send<unknown>(CONTENT_GOODS_PATH))
  }
}
