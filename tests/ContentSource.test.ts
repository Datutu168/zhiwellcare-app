import { afterEach, describe, expect, it, vi } from 'vitest'
import { API_BASE } from '../src/api/http'
import {
  CONTENT_COURSES_PATH,
  CONTENT_GOODS_PATH,
  HttpContentSource,
} from '../src/core/content/HttpContentSource'
import type { ContentRequester } from '../src/core/content/HttpContentSource'
import { LocalContentSource } from '../src/core/content/LocalContentSource'
import { createContentSource } from '../src/core/content/ContentService'
import {
  formatPriceCents,
  normalizeCourseList,
  normalizeGoods,
  normalizeGoodsList,
  resolvePriceLabel,
  yuanToCents,
} from '../src/core/content/normalize'
import { courses as builtinCourses } from '../src/data/courses'
import { mallGoods as builtinGoods } from '../src/data/mall'

/** 后端契约样例：课程（只含契约字段，展示字段由内置详情补齐）。 */
const REMOTE_COURSES = [
  {
    courseId: 'shoulder-circles-warmup',
    title: '肩部环绕热身',
    summary: '居家上肢健身开篇热身。',
    coverUrl: 'https://cdn.example.com/courses/shoulder.png',
    videoUrl: '',
    durationLabel: '约 8 分钟',
    level: '入门',
    tags: ['零器械'],
    status: 'on',
    sort: 1,
  },
  {
    courseId: 'remote-only-course',
    title: '远端新增课程',
    summary: '只在后端存在的课程。',
    coverUrl: '',
    videoUrl: '',
    durationLabel: '',
    level: '',
    tags: null,
    status: 'on',
    sort: 2,
  },
]

/** 后端契约样例：商品。 */
const REMOTE_GOODS = [
  {
    goodsId: 'handle-sphere',
    name: '球形手柄配件',
    summary: '球形握持手柄。',
    priceCents: 6900,
    priceLabel: '',
    coverUrl: '',
    detailUrl: '',
    specs: null,
    status: 'on',
    sort: 1,
  },
]

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify({ code: 0, message: 'ok', data }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/** 全局 fetch 桩：按请求地址返回响应，并记录调用。 */
function stubFetch(handler: (url: string) => Response) {
  const mock = vi.fn(async (input: unknown, _init?: RequestInit) => handler(String(input)))
  vi.stubGlobal('fetch', mock)
  return mock
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.unstubAllEnvs()
})

describe('内容 HTTP 源（请求路径与解析）', () => {
  it('按契约路径请求课程/商品接口，并带上 Accept: application/json', async () => {
    const fetchMock = stubFetch((url) => jsonResponse(url.endsWith(CONTENT_COURSES_PATH) ? REMOTE_COURSES : REMOTE_GOODS))
    const source = new HttpContentSource()

    const courses = await source.loadCourses()
    const goods = await source.loadGoods()

    expect(fetchMock.mock.calls.map((call) => call[0])).toEqual([
      `${API_BASE}${CONTENT_COURSES_PATH}`,
      `${API_BASE}${CONTENT_GOODS_PATH}`,
    ])
    expect(new Headers(fetchMock.mock.calls[0][1]?.headers).get('Accept')).toBe('application/json')
    expect(courses.map((course) => course.courseId)).toEqual(['shoulder-circles-warmup', 'remote-only-course'])
    expect(goods.map((item) => item.goodsId)).toEqual(['handle-sphere'])
  })

  it('解析统一信封 data：契约字段直取，展示字段由同 id 内置详情补齐', async () => {
    stubFetch((url) => jsonResponse(url.endsWith(CONTENT_COURSES_PATH) ? REMOTE_COURSES : REMOTE_GOODS))
    const source = new HttpContentSource()

    const [course] = await source.loadCourses()
    expect(course.title).toBe('肩部环绕热身')
    expect(course.coverUrl).toBe('https://cdn.example.com/courses/shoulder.png')
    expect(course.status).toBe('on')
    // 内置同 id 详情补齐：分类 / emoji 封面 / 图文步骤
    expect(course.category).toBe('上肢')
    expect(course.cover).toBe('🌀')
    expect(course.steps.length).toBeGreaterThan(0)

    const [goods] = await source.loadGoods()
    // priceLabel 为空 → 由 priceCents（分）整数换算
    expect(goods.priceLabel).toBe('¥69')
    expect(goods.kind).toBe('hardware')
    expect(goods.kindKnown).toBe(true)
    expect(goods.priceUnit).toBe('个')
    expect(goods.cover).toBe('🔮')
    expect(goods.relatedHardware).toBe(true)
  })

  it('请求路径使用相对路径交给 request() 拼装（可注入请求器）', async () => {
    const paths: string[] = []
    const send: ContentRequester = async <T>(path: string) => {
      paths.push(path)
      return (path === CONTENT_COURSES_PATH ? REMOTE_COURSES : REMOTE_GOODS) as T
    }
    const source = new HttpContentSource(send)

    await source.loadCourses()
    await source.loadGoods()

    expect(paths).toEqual([CONTENT_COURSES_PATH, CONTENT_GOODS_PATH])
  })

  it('非 2xx 响应抛出错误（携带后端 message）', async () => {
    stubFetch(() => new Response(JSON.stringify({ code: 500, message: '内容服务内部错误', data: null }), { status: 500 }))
    const source = new HttpContentSource()

    await expect(source.loadCourses()).rejects.toThrow('内容服务内部错误')
    await expect(source.loadGoods()).rejects.toThrow('内容服务内部错误')
  })

  it('信封 code 非 0（HTTP 200）同样视为失败', async () => {
    stubFetch(() => new Response(JSON.stringify({ code: 1001, message: '目录未初始化', data: null }), { status: 200 }))
    const source = new HttpContentSource()

    await expect(source.loadCourses()).rejects.toThrow('目录未初始化')
  })

  it('非 JSON 响应体不会静默返回空数据，而是抛错交给上层回退', async () => {
    stubFetch(() => new Response('<html>gateway error</html>', { status: 200 }))
    const source = new HttpContentSource()

    await expect(source.loadCourses()).rejects.toThrow()
  })
})

describe('内容结构容错（远端字段缺失/类型异常）', () => {
  it('data 不是数组时安全降级为空列表', () => {
    expect(normalizeCourseList(null)).toEqual([])
    expect(normalizeCourseList({ courses: [] })).toEqual([])
    expect(normalizeGoodsList('boom')).toEqual([])
  })

  it('缺 id 或标题的条目被丢弃，不影响其余条目', () => {
    const list = normalizeCourseList([
      'not-an-object',
      {},
      { title: '只有标题' },
      { courseId: 'ok', title: '正常课程' },
    ])
    expect(list.map((course) => course.courseId)).toEqual(['ok'])
  })

  it('字段类型异常时取安全默认值（tags/steps/status/sort）', () => {
    const [course] = normalizeCourseList([
      { courseId: 'weird', title: '异常数据课程', tags: 'upper', steps: null, status: 'unknown', sort: '3', durationMin: '12' },
    ])
    expect(course.tags).toEqual([])
    expect(course.steps).toEqual([])
    expect(course.status).toBe('on')
    expect(course.sort).toBe(3)
    expect(course.durationMin).toBe(12)
    expect(course.durationLabel).toBe('约 12 分钟')
    expect(course.category).toBe('其他')
    expect(course.cover).toBe('📚')
  })

  it('数字型 id 与字符串型价格同样可解析，下架状态被识别', () => {
    const goods = normalizeGoods({ goodsId: 42, name: '数字 id 商品', priceCents: '1999', specs: null, status: 'off' })
    expect(goods).not.toBeNull()
    expect(goods!.goodsId).toBe('42')
    expect(goods!.priceCents).toBe(1999)
    expect(goods!.priceLabel).toBe('¥19.99')
    expect(goods!.specs).toEqual([])
    expect(goods!.status).toBe('off')
    expect(goods!.kind).toBe('hardware')
    expect(goods!.kindKnown).toBe(false)
    expect(goods!.priceUnit).toBe('件')
  })

  it('远端未提供品类时用中性文案（分类「商品」、单位「件」、kindKnown=false）', () => {
    const goods = normalizeGoods({ goodsId: 'unknown-kind', name: '品类未知商品', priceCents: 1000 })
    expect(goods!.kindKnown).toBe(false)
    expect(goods!.category).toBe('商品')
    expect(goods!.priceUnit).toBe('件')
    // 内置同 id 命中时品类来自内置数据，marker 为 true
    expect(normalizeGoods({ goodsId: 'home-guide-trial', name: '体验课', priceCents: 9900 })!.kindKnown).toBe(true)
  })

  it('服务类商品归一化保留品类与计价单位', () => {
    const goods = normalizeGoods({ goodsId: 'svc-1', name: '上门指导', priceCents: 9900, kind: 'service' })
    expect(goods!.kind).toBe('service')
    expect(goods!.kindKnown).toBe(true)
    expect(goods!.category).toBe('训练服务')
    expect(goods!.priceUnit).toBe('次')
  })
})

describe('价格换算（分 → 展示文案，整数运算）', () => {
  it('formatPriceCents 由分换算为元，无浮点误差', () => {
    expect(formatPriceCents(39900)).toBe('¥399')
    expect(formatPriceCents(39950)).toBe('¥399.50')
    expect(formatPriceCents(1999)).toBe('¥19.99')
    expect(formatPriceCents(1)).toBe('¥0.01')
    expect(formatPriceCents(0)).toBe('¥0')
    // 元 → 分：8.7*100 = 869.9999999999999，必须四舍五入，否则会渲染成 ¥8.69
    expect(yuanToCents(8.7)).toBe(870)
    expect(formatPriceCents(yuanToCents(8.7))).toBe('¥8.70')
    expect(formatPriceCents(8.7 * 100)).toBe('¥8.70')
    expect(formatPriceCents(Number.NaN)).toBe('¥0')
  })

  it('priceLabel 优先用后端文案，为空时由分换算，都缺失时给「价格待定」', () => {
    expect(resolvePriceLabel('¥699/次卡', 69900)).toBe('¥699/次卡')
    expect(resolvePriceLabel('', 69900)).toBe('¥699')
    expect(resolvePriceLabel('   ', 69900)).toBe('¥699')
    expect(resolvePriceLabel(null, null)).toBe('价格待定')
    expect(resolvePriceLabel(undefined, Number.NaN)).toBe('价格待定')
  })

  it('元 → 分换算取整（演示价为整数元）', () => {
    expect(yuanToCents(399)).toBe(39900)
    expect(yuanToCents(19.99)).toBe(1999)
    expect(yuanToCents(Number.NaN)).toBe(0)
  })
})

describe('内置内容源（兜底数据完整性）', () => {
  it('内置课程全部可用：顺序、时长文案与图文步骤与内置数据一致', async () => {
    const courses = await new LocalContentSource().loadCourses()

    expect(courses.map((course) => course.courseId)).toEqual(builtinCourses.map((course) => course.id))
    expect(courses.every((course) => course.status === 'on')).toBe(true)
    const first = courses[0]
    expect(first.title).toBe(builtinCourses[0].title)
    expect(first.durationLabel).toBe(`约 ${builtinCourses[0].durationMin} 分钟`)
    expect(first.cover).toBe(builtinCourses[0].cover)
    expect(first.steps).toHaveLength(builtinCourses[0].steps.length)
    expect(courses.find((course) => course.courseId === 'resistance-band-shoulder-back')?.disclaimer).toBe(
      builtinCourses.find((course) => course.id === 'resistance-band-shoulder-back')?.disclaimer,
    )
  })

  it('内置商品全部可用：演示价（元）换算为分，展示文案与内置价格一致', async () => {
    const goods = await new LocalContentSource().loadGoods()

    expect(goods.map((item) => item.goodsId)).toEqual(builtinGoods.map((item) => item.id))
    expect(goods.every((item) => item.status === 'on')).toBe(true)
    for (const item of goods) {
      const builtin = builtinGoods.find((entry) => entry.id === item.goodsId)!
      expect(item.priceCents).toBe(builtin.price * 100)
      expect(item.priceLabel).toBe(`¥${builtin.price}`)
      expect(item.priceUnit).toBe(builtin.priceUnit)
      expect(item.badges).toEqual(builtin.badges)
      expect(item.specs).toEqual(builtin.spec)
      expect(item.cover).toBe(builtin.cover)
    }
    expect(goods.find((item) => item.goodsId === 'home-guide-monthly')?.kind).toBe('service')
  })

  it('内置源排序与内置数组顺序一致（sort = 下标）', async () => {
    const source = new LocalContentSource()
    const [courses, goods] = await Promise.all([source.loadCourses(), source.loadGoods()])
    expect(courses.map((course) => course.sort)).toEqual(builtinCourses.map((_, index) => index))
    expect(goods.map((item) => item.sort)).toEqual(builtinGoods.map((_, index) => index))
  })
})

describe('内容源工厂（VITE_CATALOG_MODE）', () => {
  it('mock 模式不发起任何请求，直接使用内置内容', async () => {
    vi.stubEnv('VITE_CATALOG_MODE', 'mock')
    const fetchMock = stubFetch(() => jsonResponse([]))

    const source = createContentSource()
    expect(source.kind).toBe('mock')

    expect(await source.loadCourses()).toHaveLength(builtinCourses.length)
    expect(await source.loadGoods()).toHaveLength(builtinGoods.length)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('http 模式发起请求，走 VITE_API_BASE 下的内容接口', async () => {
    vi.stubEnv('VITE_CATALOG_MODE', 'http')
    const fetchMock = stubFetch((url) => jsonResponse(url.endsWith(CONTENT_COURSES_PATH) ? REMOTE_COURSES : REMOTE_GOODS))

    const source = createContentSource()
    expect(source.kind).toBe('http')

    expect(await source.loadCourses()).toHaveLength(2)
    expect(await source.loadGoods()).toHaveLength(1)
    expect(fetchMock.mock.calls.map((call) => call[0])).toEqual([
      `${API_BASE}${CONTENT_COURSES_PATH}`,
      `${API_BASE}${CONTENT_GOODS_PATH}`,
    ])
  })

  it('未配置 VITE_CATALOG_MODE 时默认 mock（与设备目录口径一致）', async () => {
    vi.stubEnv('VITE_CATALOG_MODE', undefined)
    const fetchMock = stubFetch(() => jsonResponse([]))

    const source = createContentSource()
    expect(source.kind).toBe('mock')

    await source.loadCourses()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
