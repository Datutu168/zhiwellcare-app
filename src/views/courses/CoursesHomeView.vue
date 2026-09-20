<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { contentService } from '../../app/AppServices'
import { contentSourceStatus } from '../../core/content/ContentService'
import { listCourseCategories } from '../../core/content/ContentTaxonomy'
import type { CourseItem } from '../../core/content/ContentTypes'
import { LocalStorageStore } from '../../core/storage/LocalStorageStore'

/** 打卡本地存储 key：{ [courseId]: string[]（yyyy-MM-dd 打卡日期数组）}。 */
const CHECKIN_KEY = 'zhiwellcare.course.checkins.v1'

type CheckinMap = Record<string, string[]>
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/

const router = useRouter()
const store = new LocalStorageStore()

const checkins = ref<CheckinMap>({})
const loaded = ref(false)
const activeCategory = ref('全部')
/** 课程内容统一来自 ContentService（http 模式失败会自动回退内置课程）。 */
const courseList = ref<CourseItem[]>([])
const courseLoaded = ref(false)
const courseCategories = listCourseCategories()
/** 后端内容不可用已回退内置内容时的界面提示（内置/后端正常时不显示）。 */
const contentOfflineHint = computed(() => contentSourceStatus.value.kind === 'http-fallback-mock')

const filteredCourses = computed(() =>
  activeCategory.value === '全部'
    ? courseList.value
    : courseList.value.filter((course) => course.category === activeCategory.value),
)

/** 打卡汇总：今日次数 / 连续天数 / 累计次数 / 本机日期。 */
const stats = computed(() => {
  const day = localDateKey(new Date())
  const doneTodayIds = new Set<string>()
  const allDates = new Set<string>()
  let totalCount = 0
  for (const [id, list] of Object.entries(checkins.value)) {
    if (!Array.isArray(list)) continue
    totalCount += list.length
    for (const date of list) {
      if (typeof date !== 'string' || !DATE_RE.test(date)) continue
      allDates.add(date)
      if (date === day) doneTodayIds.add(id)
    }
  }
  return { todayCount: doneTodayIds.size, totalCount, streakDays: calcStreakDays(allDates, day), dayKey: day }
})

/** 某课程今天是否已打卡。 */
function isDoneToday(courseId: string): boolean {
  const day = localDateKey(new Date())
  const list = checkins.value[courseId]
  return Array.isArray(list) && list.includes(day)
}

function openCourse(courseId: string): void { void router.push(`/courses/${courseId}`) }

function resetCategory(): void { activeCategory.value = '全部' }

onMounted(async () => {
  try { checkins.value = normalizeCheckins(await store.get(CHECKIN_KEY)) }
  catch { checkins.value = {} }
  finally { loaded.value = true }
  try { courseList.value = await contentService.listCourses() }
  catch { courseList.value = [] }
  finally { courseLoaded.value = true }
})

/** 本机日期 → yyyy-MM-dd（避免 UTC 时区偏差）。 */
function localDateKey(date: Date): string {
  const pad = (value: number) => `${value}`.padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

/** 容错解析本地打卡数据；损坏或缺失时安全返回空对象。 */
function normalizeCheckins(raw: string | null): CheckinMap {
  if (!raw) return {}
  try {
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {}
    const map: CheckinMap = {}
    for (const [id, list] of Object.entries(parsed as Record<string, unknown>)) {
      if (!Array.isArray(list)) continue
      const dates = [...new Set(list.filter((item): item is string => typeof item === 'string' && DATE_RE.test(item)))].sort()
      if (dates.length) map[id] = dates
    }
    return map
  }
  catch { return {} }
}

/** 最近不间断打卡天数：今日已打卡从今日倒推，今日未打卡则从昨日倒推。 */
function calcStreakDays(dates: Set<string>, todayKey: string): number {
  if (!dates.size) return 0
  const cursor = new Date()
  if (!dates.has(todayKey)) cursor.setDate(cursor.getDate() - 1)
  let streak = 0
  while (dates.has(localDateKey(cursor))) {
    streak += 1
    cursor.setDate(cursor.getDate() - 1)
  }
  return streak
}
</script>

<template>
  <main class="content-page courses-home">
    <header class="page-hero">
      <h1>课程训练</h1>
      <p>居家上肢健身 · 主动发力 · 按自己的节奏跟练</p>
      <div class="hero-tags">
        <span class="hero-tag">图文跟练</span>
        <span class="hero-tag">热身再练</span>
        <span class="hero-tag">量力而行</span>
      </div>
    </header>

    <!-- 打卡汇总小卡（今日次数 / 连续天数 / 累计次数，均来自本机打卡数据） -->
    <section class="card checkin-bar" aria-label="训练打卡汇总">
      <div class="checkin-stat"><small>📅 今日已打卡</small><strong class="num">{{ stats.todayCount }}<em> 次</em></strong></div>
      <div class="checkin-stat"><small>🔥 连续打卡</small><strong class="num">{{ stats.streakDays }}<em> 天</em></strong></div>
      <div class="checkin-stat"><small>🏅 累计打卡</small><strong class="num">{{ stats.totalCount }}<em> 次</em></strong></div>
      <p class="muted small checkin-tip">{{ loaded ? `打卡记录保存在本机 · 每天每门课程计 1 次 · 日期以本机为准（${stats.dayKey}）` : '正在读取本机打卡记录…' }}</p>
    </section>

    <!-- 分类筛选 chips -->
    <div class="filter-row" role="group" aria-label="按分类筛选课程">
      <button
        v-for="category in courseCategories"
        :key="category"
        type="button"
        class="filter-chip"
        :class="{ 'is-active': category === activeCategory }"
        @click="activeCategory = category"
      >{{ category }}</button>
    </div>

    <p v-if="contentOfflineHint" class="muted small content-source-note" :title="contentSourceStatus.message">
      当前为离线内容：后端课程接口暂不可用，已回退内置课程。
    </p>

    <!-- 课程卡片网格 -->
    <section v-if="filteredCourses.length" class="grid-cards auto course-grid">
      <button
        v-for="course in filteredCourses"
        :key="course.courseId"
        type="button"
        class="course-card card card-hover"
        :aria-label="`${course.title}，${course.category}${course.level}课程，${isDoneToday(course.courseId) ? '今日已打卡' : '今日未打卡'}`"
        @click="openCourse(course.courseId)"
      >
        <span class="course-cover">
          <img v-if="course.coverUrl" class="cover-img" :src="course.coverUrl" :alt="course.title" />
          <span v-else class="cover-emoji" aria-hidden="true">{{ course.cover }}</span>
          <span class="duration">{{ course.durationLabel }}</span>
        </span>
        <span class="course-title">{{ course.title }}</span>
        <span class="course-summary">{{ course.summary }}</span>
        <span class="tag-chip-row course-tags">
          <span class="tag-chip is-cyan">{{ course.category }}</span>
          <span v-if="course.level" class="tag-chip">{{ course.level }}</span>
          <span v-for="tag in course.tags.slice(0, 2)" :key="tag" class="tag-chip is-off">{{ tag }}</span>
        </span>
        <span class="course-foot">
          <span class="checkin-state" :class="{ 'is-done': isDoneToday(course.courseId) }">{{ isDoneToday(course.courseId) ? '✔ 今日已打卡' : '今日未打卡' }}</span>
          <span class="course-cta">开始 / 详情 <span aria-hidden="true">→</span></span>
        </span>
      </button>
    </section>
    <div v-else-if="!courseLoaded" class="empty-state">
      <div class="empty-ic" aria-hidden="true">⏳</div>
      <p>正在加载课程…</p>
    </div>
    <div v-else class="empty-state">
      <div class="empty-ic" aria-hidden="true">🧘</div>
      <p>该分类下暂时没有课程，先看看其他分类吧。</p>
      <button type="button" class="button ghost" @click="resetCategory">查看全部课程</button>
    </div>

    <!-- 纯健身声明 + 运动安全提示 -->
    <section class="compliance-note courses-compliance">
      <span aria-hidden="true">ℹ️</span>
      <p>
        <strong>纯健身产品声明：</strong>智为康乐 ZhiWellCare 课程仅提供居家上肢健身、日常机能维持锻炼的通用健身指导，属于纯健身消费产品服务，不替代也不提供任何专业健康服务。
        <strong>运动安全提示：</strong>训练前请充分热身，量力而行，如有不适立即停止。
      </p>
    </section>
  </main>
</template>

<style scoped>
/* 打卡汇总小卡 */
.checkin-bar { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; padding: 18px 20px 14px; }
.checkin-stat { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.checkin-stat small { color: var(--c-ink-3); font-size: 12px; white-space: nowrap; }
.checkin-stat .num { font-size: 24px; line-height: 1.2; color: var(--c-primary); }
.checkin-stat .num em { font-style: normal; font-size: 12px; font-weight: 600; color: var(--c-ink-3); }
.checkin-tip { grid-column: 1 / -1; margin: 8px 0 0; padding-top: 10px; border-top: 1px dashed var(--c-line); }

/* 分类筛选 */
.filter-row { display: flex; flex-wrap: wrap; gap: 8px; margin: 16px 0 14px; }
.filter-chip {
  border: 1px solid var(--c-line-2); background: var(--c-bg); color: var(--c-ink-2);
  border-radius: 999px; padding: 6px 16px; font-size: 13px; transition: all .15s ease;
}
.filter-chip:hover { border-color: var(--c-primary); color: var(--c-primary); }
.filter-chip.is-active { background: var(--grad-brand-soft); color: #fff; border-color: transparent; font-weight: 600; }

/* 课程卡（按钮整卡可点） */
.course-card { width: 100%; text-align: left; color: var(--c-ink); }
.cover-emoji { line-height: 1; }
/* 远端下发 CDN 封面时用图片替代 emoji 占位（离线内置数据无 coverUrl，仍走 emoji） */
.cover-img { max-width: 100%; max-height: 100%; object-fit: contain; border-radius: 10px; }
.content-source-note { margin: 10px 0 0; }
.course-title { font-weight: 700; font-size: 15px; line-height: 1.4; color: var(--c-ink); }
.course-summary {
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
  min-height: calc(2 * 1.6em); color: var(--c-ink-2); font-size: 13px; line-height: 1.6;
}
.course-tags { margin: 0; }
.course-foot { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.checkin-state { color: var(--c-ink-3); font-size: 12px; white-space: nowrap; }
.checkin-state.is-done { color: var(--c-accent); font-weight: 600; }
.course-cta { display: inline-flex; align-items: center; gap: 4px; color: var(--c-primary); font-size: 13px; font-weight: 700; white-space: nowrap; }

.courses-compliance { margin-top: 18px; }
.courses-compliance p { margin: 0; }
</style>
