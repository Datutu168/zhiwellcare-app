import { ref } from 'vue'
import type { IContentSource } from './IContentSource'
import { HttpContentSource } from './HttpContentSource'
import { LocalContentSource } from './LocalContentSource'
import { CONTENT_SCHEMA_VERSION } from './ContentTypes'
import type {
  ContentSnapshot,
  ContentSourceStatusKind,
  CourseItem,
  MallGoodsItem,
} from './ContentTypes'

/** 内容源当前状态：UI 可据此展示“内置内容 / 后端内容 / 后端不可用已回退内置”。 */
export const contentSourceStatus = ref<{ kind: ContentSourceStatusKind; message: string }>({
  kind: 'mock',
  message: '内置内容',
})

/**
 * 运行时内容源工厂：VITE_CATALOG_MODE=http 时连接 Golang 后端内容接口，失败自动回退内置内容。
 * 后端地址复用 VITE_API_BASE（src/api/http.ts），mock 模式下不发起任何网络请求。
 */
export function createContentSource(): IContentSource {
  const mode = import.meta.env.VITE_CATALOG_MODE ?? 'mock'
  return mode === 'http' ? new HttpContentSource() : new LocalContentSource()
}

/**
 * 内容服务：课程/商城页面唯一的内容访问入口。
 * 与 CatalogService 同构 —— 缓存快照、优先远端、失败静默回退内置内容并暴露来源状态。
 */
export class ContentService {
  private snapshotPromise: Promise<ContentSnapshot> | null = null

  constructor(private readonly source: IContentSource = createContentSource()) {}

  async loadSnapshot(): Promise<ContentSnapshot> {
    this.snapshotPromise ??= this.fetchSnapshotWithFallback()
    return this.snapshotPromise
  }

  /** 上架课程（按 sort 升序）。 */
  async listCourses(): Promise<CourseItem[]> {
    return (await this.loadSnapshot()).courses.filter((course) => course.status === 'on')
  }

  /** 上架商品（按 sort 升序）。 */
  async listGoods(): Promise<MallGoodsItem[]> {
    return (await this.loadSnapshot()).goods.filter((goods) => goods.status === 'on')
  }

  async findCourse(courseId: string): Promise<CourseItem | null> {
    if (!courseId) return null
    return (await this.listCourses()).find((course) => course.courseId === courseId) ?? null
  }

  async findGoods(goodsId: string): Promise<MallGoodsItem | null> {
    if (!goodsId) return null
    return (await this.listGoods()).find((goods) => goods.goodsId === goodsId) ?? null
  }

  /** 清缓存（内容刷新场景）。 */
  invalidate(): void {
    this.snapshotPromise = null
  }

  private async fetchSnapshotWithFallback(): Promise<ContentSnapshot> {
    if (this.source.kind === 'mock') {
      const [courses, goods] = await Promise.all([this.source.loadCourses(), this.source.loadGoods()])
      contentSourceStatus.value = { kind: 'mock', message: '内置内容' }
      return buildSnapshot(courses, goods)
    }

    // 后端未就绪/网络失败时逐资源回退内置内容，保证课程页与商城页永远有内容可看。
    const fallback = new LocalContentSource()
    const failures: string[] = []
    const withFallback = async <T>(load: () => Promise<T[]>, fallbackLoad: () => Promise<T[]>): Promise<T[]> => {
      try {
        return await load()
      } catch (error) {
        failures.push(error instanceof Error ? error.message : String(error))
        return fallbackLoad()
      }
    }

    const [courses, goods] = await Promise.all([
      withFallback<CourseItem>(() => this.source.loadCourses(), () => fallback.loadCourses()),
      withFallback<MallGoodsItem>(() => this.source.loadGoods(), () => fallback.loadGoods()),
    ])
    contentSourceStatus.value = failures.length
      ? { kind: 'http-fallback-mock', message: `后端内容不可用，已回退内置内容：${failures.join('；')}` }
      : { kind: 'http', message: '后端内容' }
    return buildSnapshot(courses, goods)
  }
}

/** 快照统一按 sort 升序（服务端已排序；此处保证内置/回退/混合来源顺序一致）。 */
function buildSnapshot(courses: CourseItem[], goods: MallGoodsItem[]): ContentSnapshot {
  return {
    schemaVersion: CONTENT_SCHEMA_VERSION,
    courses: sortByOrder(courses),
    goods: sortByOrder(goods),
  }
}

function sortByOrder<T extends { sort: number }>(items: T[]): T[] {
  return [...items].sort((a, b) => a.sort - b.sort)
}
