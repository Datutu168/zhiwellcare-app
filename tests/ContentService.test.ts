import { afterEach, describe, expect, it, vi } from 'vitest'
import { API_BASE } from '../src/api/http'
import { CONTENT_COURSES_PATH, CONTENT_GOODS_PATH, HttpContentSource } from '../src/core/content/HttpContentSource'
import { LocalContentSource } from '../src/core/content/LocalContentSource'
import { ContentService, contentSourceStatus } from '../src/core/content/ContentService'
import type { IContentSource } from '../src/core/content/IContentSource'
import type { CourseItem, MallGoodsItem } from '../src/core/content/ContentTypes'
import { normalizeCourse, normalizeGoods } from '../src/core/content/normalize'
import { courses as builtinCourses } from '../src/data/courses'
import { mallGoods as builtinGoods } from '../src/data/mall'

/** 远端课程条目（契约字段 + 归一化展示字段），等价于 HttpContentSource 的产出。 */
function remoteCourse(overrides: Record<string, unknown> = {}): CourseItem {
  return normalizeCourse({
    courseId: 'remote-course',
    title: '远端课程',
    summary: '远端课程简介',
    coverUrl: '',
    videoUrl: '',
    durationLabel: '约 5 分钟',
    level: '入门',
    tags: ['远端'],
    status: 'on',
    sort: 0,
    ...overrides,
  })!
}

function remoteGoods(overrides: Record<string, unknown> = {}): MallGoodsItem {
  return normalizeGoods({
    goodsId: 'remote-goods',
    name: '远端商品',
    summary: '远端商品简介',
    priceCents: 12345,
    priceLabel: '',
    coverUrl: '',
    detailUrl: '',
    specs: ['规格一'],
    status: 'on',
    sort: 0,
    ...overrides,
  })!
}

/** 桩内容源：可指定 kind，并统计各接口调用次数。 */
function stubSource(kind: 'mock' | 'http', payload: { courses?: CourseItem[]; goods?: MallGoodsItem[] } = {}) {
  const calls = { courses: 0, goods: 0 }
  const source: IContentSource = {
    kind,
    loadCourses: async () => {
      calls.courses += 1
      return payload.courses ?? []
    },
    loadGoods: async () => {
      calls.goods += 1
      return payload.goods ?? []
    },
  }
  return { source, calls }
}

/** 恒定失败的源。 */
function failingSource(kind: 'mock' | 'http', message: string): IContentSource {
  return {
    kind,
    loadCourses: async () => { throw new Error(message) },
    loadGoods: async () => { throw new Error(message) },
  }
}

/** 只让课程接口失败的源（商品接口仍返回远端数据）。 */
function halfFailingSource(goods: MallGoodsItem[]): IContentSource {
  return {
    kind: 'http',
    loadCourses: async () => { throw new Error('目录接口请求失败：500') },
    loadGoods: async () => goods,
  }
}

afterEach(() => { vi.unstubAllGlobals() })

describe('内容服务（HTTP 成功）', () => {
  it('使用远端课程与商品，并把来源状态标记为后端内容', async () => {
    const { source } = stubSource('http', { courses: [remoteCourse()], goods: [remoteGoods()] })
    const service = new ContentService(source)

    const snapshot = await service.loadSnapshot()

    expect(snapshot.courses.map((course) => course.courseId)).toEqual(['remote-course'])
    expect(snapshot.goods.map((goods) => goods.goodsId)).toEqual(['remote-goods'])
    expect(snapshot.schemaVersion).toBe(1)
    expect(contentSourceStatus.value.kind).toBe('http')
    expect(contentSourceStatus.value.message).toBe('后端内容')
  })

  it('只返回上架内容，并按 sort 升序排列', async () => {
    const { source } = stubSource('http', {
      courses: [
        remoteCourse({ courseId: 'c-late', sort: 9 }),
        remoteCourse({ courseId: 'c-off', sort: 2, status: 'off' }),
        remoteCourse({ courseId: 'c-first', sort: 1 }),
      ],
      goods: [
        remoteGoods({ goodsId: 'g-late', sort: 5 }),
        remoteGoods({ goodsId: 'g-off', sort: 1, status: 'off' }),
        remoteGoods({ goodsId: 'g-first', sort: 2 }),
      ],
    })
    const service = new ContentService(source)

    expect((await service.listCourses()).map((course) => course.courseId)).toEqual(['c-first', 'c-late'])
    expect((await service.listGoods()).map((goods) => goods.goodsId)).toEqual(['g-first', 'g-late'])
  })

  it('findCourse / findGoods 命中返回条目；未命中或空 id 返回 null', async () => {
    const { source } = stubSource('http', { courses: [remoteCourse({ courseId: 'c1' })], goods: [remoteGoods({ goodsId: 'g1' })] })
    const service = new ContentService(source)

    expect((await service.findCourse('c1'))?.courseId).toBe('c1')
    expect((await service.findGoods('g1'))?.goodsId).toBe('g1')
    expect(await service.findCourse('not-exist')).toBeNull()
    expect(await service.findGoods('not-exist')).toBeNull()
    expect(await service.findCourse('')).toBeNull()
    expect(await service.findGoods('')).toBeNull()
  })

  it('快照缓存：多次取数只加载一次，invalidate 后重新加载', async () => {
    const { source, calls } = stubSource('http', { courses: [remoteCourse()], goods: [remoteGoods()] })
    const service = new ContentService(source)

    await service.listCourses()
    await service.listGoods()
    await service.findCourse('remote-course')
    expect(calls).toEqual({ courses: 1, goods: 1 })

    service.invalidate()
    await service.listCourses()
    expect(calls).toEqual({ courses: 2, goods: 2 })
  })
})

describe('内容服务（HTTP 失败静默回退内置内容）', () => {
  it('课程与商品接口都失败时回退内置内容，且不抛错', async () => {
    const service = new ContentService(failingSource('http', '目录接口请求失败：500'))

    const courses = await service.listCourses()
    const goods = await service.listGoods()

    expect(courses).toHaveLength(builtinCourses.length)
    expect(goods).toHaveLength(builtinGoods.length)
    expect(courses.map((course) => course.courseId)).toEqual(builtinCourses.map((course) => course.id))
    expect(contentSourceStatus.value.kind).toBe('http-fallback-mock')
    expect(contentSourceStatus.value.message).toContain('500')
  })

  it('只有课程接口失败时，课程用内置、商品仍用远端，并标记为已回退', async () => {
    const service = new ContentService(halfFailingSource([remoteGoods({ goodsId: 'g-remote' })]))

    expect((await service.listCourses()).map((course) => course.courseId)).toEqual(builtinCourses.map((course) => course.id))
    expect((await service.listGoods()).map((goods) => goods.goodsId)).toEqual(['g-remote'])
    expect(contentSourceStatus.value.kind).toBe('http-fallback-mock')
  })

  it('网络不可达（fetch 抛出）时同样静默回退，页面照常有内容', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('fetch failed') }))
    const service = new ContentService(new HttpContentSource())

    const courses = await service.listCourses()

    expect(courses).toHaveLength(builtinCourses.length)
    expect(contentSourceStatus.value.kind).toBe('http-fallback-mock')
    expect(contentSourceStatus.value.message).toContain('无法连接服务器')
  })

  it('后端返回 500 时用真实 HttpContentSource 验证回退（不抛错）', async () => {
    const fetchMock = vi.fn(async (input: unknown) => {
      const url = String(input)
      const status = 500
      const body = JSON.stringify({ code: 500, message: `内容接口不可用：${url}`, data: null })
      return new Response(body, { status })
    })
    vi.stubGlobal('fetch', fetchMock)
    const service = new ContentService(new HttpContentSource())

    const goods = await service.listGoods()

    expect(goods).toHaveLength(builtinGoods.length)
    expect(goods.find((item) => item.goodsId === 'desk-torque-base')?.priceLabel).toBe('¥599')
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls.map((call) => String(call[0])).sort()).toEqual([
      `${API_BASE}${CONTENT_COURSES_PATH}`,
      `${API_BASE}${CONTENT_GOODS_PATH}`,
    ])
    expect(contentSourceStatus.value.kind).toBe('http-fallback-mock')
  })

  it('内置源（mock）自身异常时不做二次兜底，错误照常抛出', async () => {
    const service = new ContentService(failingSource('mock', '内置内容损坏'))

    await expect(service.listCourses()).rejects.toThrow('内置内容损坏')
  })
})

describe('内容服务（内置兜底与既有界面一致）', () => {
  it('默认内置源下，课程与商品字段与改造前页面使用的数据完全一致', async () => {
    const service = new ContentService(new LocalContentSource())

    const courses = await service.listCourses()
    const goods = await service.listGoods()

    // 课程卡片渲染所需的 id / 封面 / 时长 / 分类 / 难度 / 标签
    const first = courses[0]
    expect(first.courseId).toBe(builtinCourses[0].id)
    expect(first.cover).toBe(builtinCourses[0].cover)
    expect(first.durationLabel).toBe('约 8 分钟')
    expect(first.category).toBe('上肢')
    expect(first.level).toBe('入门')
    expect(first.tags).toEqual(builtinCourses[0].tags)

    // 商城卡片渲染所需的价格 / 单位 / 徽标 / 品类
    const base = goods.find((item) => item.goodsId === 'desk-torque-base')!
    const builtinBase = builtinGoods.find((item) => item.id === 'desk-torque-base')!
    expect(base.priceLabel).toBe(`¥${builtinBase.price}`)
    expect(base.priceUnit).toBe(builtinBase.priceUnit)
    expect(base.badges).toEqual(builtinBase.badges)
    expect(base.kind).toBe(builtinBase.kind)
    expect(base.relatedHardware).toBe(builtinBase.relatedHardware ?? false)

    expect(contentSourceStatus.value.kind).toBe('mock')
    expect(contentSourceStatus.value.message).toBe('内置内容')
  })

  it('找不到的课程/商品返回 null（详情页渲染空态而不是崩溃）', async () => {
    const service = new ContentService(new LocalContentSource())

    expect(await service.findCourse('no-such-course')).toBeNull()
    expect(await service.findGoods('no-such-goods')).toBeNull()
  })
})
