<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { contentService } from '../../app/AppServices'
import type { CourseItem } from '../../core/content/ContentTypes'
import { LocalStorageStore } from '../../core/storage/LocalStorageStore'

/** 打卡本地存储 key：{ [courseId]: string[]（yyyy-MM-dd 打卡日期数组）}。 */
const CHECKIN_KEY = 'zhiwellcare.course.checkins.v1'

type CheckinMap = Record<string, string[]>
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/

const route = useRoute()
const router = useRouter()
const store = new LocalStorageStore()

const rawCourseId = computed(() => String(route.params.courseId ?? ''))
/** 课程内容统一来自 ContentService；课程不存在时 course 为 null → 渲染空态。 */
const course = ref<CourseItem | null>(null)
const courseLoaded = ref(false)

const checkinDates = ref<string[]>([])
const loaded = ref(false)
const saving = ref(false)
const errorMessage = ref('')

const isCheckedIn = computed(() => checkinDates.value.includes(localDateKey(new Date())))
const recentText = computed(() => [...checkinDates.value].sort().reverse().slice(0, 3).join('、'))
const historyText = computed(() =>
  checkinDates.value.length
    ? `打卡历史 · 累计 ${checkinDates.value.length} 次`
    : '暂无打卡记录，完成本课程跟练即可点亮今日 ✔',
)

/** 路由参数变化（同组件复用）时重新取数。 */
watch(rawCourseId, (courseId) => { void loadCourse(courseId) }, { immediate: true })

async function loadCourse(courseId: string): Promise<void> {
  try { course.value = courseId ? await contentService.findCourse(courseId) : null }
  catch { course.value = null }
  finally { courseLoaded.value = true }
}

onMounted(async () => {
  try {
    const map = normalizeCheckins(await store.get(CHECKIN_KEY))
    checkinDates.value = map[rawCourseId.value] ?? []
  }
  catch { checkinDates.value = [] }
  finally { loaded.value = true }
})

/** 「完成打卡」：同一课程同一天只计一次（日期 = 本机 yyyy-MM-dd）。 */
async function doCheckin(): Promise<void> {
  const target = course.value
  if (!target || saving.value || isCheckedIn.value) return
  saving.value = true
  errorMessage.value = ''
  try {
    const map = normalizeCheckins(await store.get(CHECKIN_KEY))
    const day = localDateKey(new Date())
    const list = Array.isArray(map[target.courseId]) ? map[target.courseId] : []
    if (!list.includes(day)) list.push(day)
    map[target.courseId] = [...new Set(list)].sort()
    await store.set(CHECKIN_KEY, JSON.stringify(map))
    checkinDates.value = map[target.courseId]
  }
  catch (error) { errorMessage.value = error instanceof Error ? error.message : String(error) }
  finally { saving.value = false }
}

/** 返回课程列表：有站内历史则返回上一页，否则回到 /courses。 */
function goBack(): void {
  if (window.history.state?.back) window.history.back()
  else void router.push('/courses')
}

function goHome(): void { void router.push('/courses') }

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
</script>

<template>
  <main class="content-page course-detail-page">
    <!-- 课程详情 -->
    <template v-if="course">
      <div class="detail-toolbar">
        <button type="button" class="button ghost small" @click="goBack">← 返回课程列表</button>
        <span class="tag-chip is-cyan">图文跟练</span>
      </div>

      <section class="card detail-head">
        <span class="course-cover detail-cover">
          <img v-if="course.coverUrl" class="cover-img" :src="course.coverUrl" :alt="course.title" />
          <span v-else class="cover-emoji" aria-hidden="true">{{ course.cover }}</span>
          <span class="duration">{{ course.durationLabel }}</span>
        </span>
        <div class="detail-info">
          <p class="eyebrow">{{ [course.category, course.level].filter(Boolean).join(' · ') }}</p>
          <h1>{{ course.title }}</h1>
          <p class="muted">{{ course.summary }}</p>
          <div class="tag-chip-row detail-tags">
            <span class="tag-chip is-cyan">{{ course.category }}</span>
            <span v-if="course.level" class="tag-chip">{{ course.level }}</span>
            <span v-for="tag in course.tags" :key="tag" class="tag-chip is-off">{{ tag }}</span>
          </div>
        </div>
      </section>

      <div class="detail-grid">
        <!-- 图文跟练步骤 -->
        <section class="card steps-card">
          <div>
            <div class="section-title">图文跟练步骤</div>
            <p class="muted small">本课程为图文跟练（无视频资源），请按序号逐节完成；动作幅度与节奏以自己的感受为准，不赶进度。</p>
          </div>
          <ol class="step-list">
            <li v-for="(step, index) in course.steps" :key="index" class="step-item">
              <span class="step-index" aria-hidden="true">{{ index + 1 }}</span>
              <span class="step-body">
                <strong>{{ step.title }}</strong>
                <span class="step-detail">{{ step.detail }}</span>
              </span>
            </li>
          </ol>
          <p v-if="course.disclaimer" class="course-warn">{{ course.disclaimer }}</p>
          <p class="safety-line">💡 训练前请充分热身，量力而行，如有不适立即停止。</p>
        </section>

        <!-- 打卡面板 -->
        <aside class="card checkin-card">
          <div>
            <div class="section-title">今日打卡</div>
            <p class="muted small">完成本课程跟练后可打卡；同一课程每天只计一次，日期以本机为准。</p>
          </div>
          <div class="checkin-today" :data-done="isCheckedIn">
            <span class="checkin-today-ic" aria-hidden="true">{{ isCheckedIn ? '✔' : '○' }}</span>
            <span>{{ loaded ? (isCheckedIn ? '今日已完成打卡' : '今日还未打卡') : '正在读取打卡记录…' }}</span>
          </div>
          <button
            type="button"
            class="button primary wide checkin-btn"
            :disabled="isCheckedIn || saving || !loaded"
            @click="doCheckin"
          >{{ saving ? '记录中…' : (isCheckedIn ? '今日已完成 ✔' : '完成打卡') }}</button>
          <p v-if="errorMessage" class="error">{{ errorMessage }}</p>
          <div v-if="checkinDates.length" class="checkin-history">
            <span class="small">{{ historyText }}</span>
            <span class="small muted">最近：{{ recentText }}</span>
          </div>
          <p v-else class="muted small history-empty-line">{{ historyText }}</p>
        </aside>
      </div>

      <!-- 纯健身声明 + 运动安全提示 -->
      <section class="compliance-note detail-compliance">
        <span aria-hidden="true">ℹ️</span>
        <p>
          <strong>纯健身产品声明：</strong>本课程为智为康乐 ZhiWellCare 提供的居家上肢健身、日常机能维持锻炼通用指导，属于纯健身消费产品服务，不替代也不提供任何专业健康服务。
          <strong>运动安全提示：</strong>训练前请充分热身，量力而行，如有不适立即停止。
        </p>
      </section>
    </template>

    <!-- 加载中：内容来自 ContentService（可能等待后端接口） -->
    <template v-else-if="!courseLoaded">
      <div class="empty-state">
        <div class="empty-ic" aria-hidden="true">⏳</div>
        <p>正在加载课程…</p>
      </div>
    </template>

    <!-- 空态：课程不存在 -->
    <template v-else>
      <div class="empty-state">
        <div class="empty-ic" aria-hidden="true">🔍</div>
        <p>未找到课程「{{ rawCourseId }}」，它可能已下架或链接有误。</p>
        <button type="button" class="button ghost" @click="goHome">返回课程列表</button>
      </div>
    </template>
  </main>
</template>

<style scoped>
.detail-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 14px; }

/* 页头：封面 + 信息 */
.detail-head { display: grid; grid-template-columns: 220px minmax(0, 1fr); gap: 20px; align-items: center; }
.detail-cover { height: 170px; font-size: 60px; }
.cover-emoji { line-height: 1; }
/* 远端下发 CDN 封面时用图片替代 emoji 占位（内置兜底数据无 coverUrl） */
.cover-img { max-width: 100%; max-height: 100%; object-fit: contain; border-radius: 10px; }
.detail-info { min-width: 0; }
.detail-info h1 { margin: 6px 0 8px; font-size: clamp(20px, 3vw, 26px); color: var(--c-ink); }
.detail-info .muted { font-size: 13px; line-height: 1.7; }
.detail-tags { margin-top: 12px; }

/* 两栏：步骤（主）+ 打卡（侧） */
.detail-grid { display: grid; grid-template-columns: minmax(0, 1fr) 320px; gap: 16px; margin-top: 16px; align-items: start; }
.steps-card { display: flex; flex-direction: column; gap: 12px; }
.steps-card p { margin: 0; }
.step-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; }
.step-item { display: flex; gap: 12px; align-items: flex-start; padding: 12px 14px; border: 1px solid var(--c-line); border-radius: 12px; background: var(--c-bg-2); }
.step-index { flex: 0 0 28px; height: 28px; display: grid; place-items: center; border-radius: 50%; background: var(--grad-brand-soft); color: #fff; font-size: 13px; font-weight: 700; }
.step-body { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.step-body strong { color: var(--c-ink); line-height: 1.4; }
.step-detail { color: var(--c-ink-2); font-size: 13px; line-height: 1.75; }
.course-warn { padding: 10px 12px; border-radius: 10px; background: var(--c-warn-bg); color: var(--c-warn-ink); font-size: 12px; line-height: 1.7; }
.safety-line { color: var(--c-ink-2); font-size: 12px; line-height: 1.7; }

/* 打卡面板 */
.checkin-card { display: flex; flex-direction: column; gap: 12px; position: sticky; top: 16px; }
.checkin-card p { margin: 0; }
.checkin-today { display: flex; align-items: center; gap: 8px; padding: 14px; border: 1px dashed var(--c-line-2); border-radius: 12px; background: var(--c-bg-2); color: var(--c-ink-2); font-weight: 600; font-size: 14px; }
.checkin-today[data-done="true"] { border-color: var(--c-accent); color: var(--c-accent); }
.checkin-today-ic { font-size: 18px; line-height: 1; }
.checkin-history { display: flex; flex-direction: column; gap: 6px; padding: 10px 12px; border-radius: 10px; background: var(--c-bg-2); }
.history-empty-line { line-height: 1.7; }

.detail-compliance { margin-top: 16px; }
.detail-compliance p { margin: 0; }

/* 多端自适应：窄屏回到单列 */
@media (max-width: 900px) {
  .detail-head { grid-template-columns: 1fr; }
  .detail-cover { height: 150px; }
  .detail-grid { grid-template-columns: 1fr; }
  .checkin-card { position: static; }
}
</style>
