<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { reportService, trainingRepository } from '../../app/AppServices'
import { catalogSourceStatus } from '../../core/catalog/CatalogService'
import type { TrainingRecord } from '../../core/training/TrainingRecord'
import { useAuthStore } from '../../stores/auth'

const router = useRouter()
const auth = useAuthStore()

// ── 训练数据统计（路由守卫已初始化仓库与上报服务，页面只需读取快照） ──
const records = ref<TrainingRecord[]>([])
const loading = ref(true)
const loadError = ref('')
const pendingCount = reportService.pendingCount // 待上报队列长度（响应式）

const totalCount = computed(() => records.value.length)
const totalDurationMs = computed(() => records.value.reduce((sum, record) => sum + (record.result?.durationMs ?? 0), 0))
const todayCount = computed(() => {
  const now = new Date()
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const end = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1).getTime()
  return records.value.filter((record) => record.completedAt >= start && record.completedAt < end).length
})

onMounted(() => {
  auth.restoreFromStorage()
  if (auth.accessToken) void auth.ensureProfile()
  void load()
})

async function load(): Promise<void> {
  try {
    records.value = await trainingRepository.getAll()
    loadError.value = ''
  } catch (error) { loadError.value = formatError(error) }
  finally { loading.value = false }
  try { await reportService.refreshPendingCount() } catch { /* 后端未接入时维持 0 */ }
}

/** 按分钟折算为友好文案：不足 1 分钟显示“不足 1 分钟”，满 1 小时显示“X 小时 Y 分钟”。 */
function formatTotalDuration(): string {
  const minutes = totalDurationMs.value / 60_000
  if (minutes <= 0) return '0 分钟'
  if (minutes < 1) return '不足 1 分钟'
  const hours = Math.floor(minutes / 60)
  const rest = Math.round(minutes % 60)
  if (hours > 0) return rest > 0 ? `${hours} 小时 ${rest} 分钟` : `${hours} 小时`
  return `${Math.round(minutes)} 分钟`
}

function open(path: string): void { void router.push(path) }
async function logout(): Promise<void> {
  if (!window.confirm('确认退出登录？本机训练记录仍会保留。')) return
  await auth.logout()
}
function formatError(error: unknown): string { return error instanceof Error ? error.message : String(error) }
</script>

<template>
  <main class="content-page">
    <header class="page-hero">
      <div class="mine-hero-row">
        <div class="mine-avatar" aria-hidden="true">👤</div>
        <div class="mine-hero-text">
          <p class="eyebrow">Mine</p>
          <h1>我的</h1>
          <p>训练数据 · 订单 · 设备 · 设置</p>
        </div>
      </div>
      <div class="hero-tags">
        <span class="hero-tag">{{ auth.isLoggedIn ? '已登录' : '本地模式' }}</span>
        <span v-if="auth.isLoggedIn" class="hero-tag">{{ auth.displayName }}</span>
        <span v-else class="hero-tag">未登录</span>
      </div>
      <p class="mine-hero-note">
        {{ auth.isLoggedIn
          ? `欢迎回来：${auth.displayName} · 云端同步与订单服务可用`
          : '当前为本地模式：注册/登录后支持云端同步训练数据与订单、上门服务。' }}
      </p>
    </header>

    <div class="mine-body">
      <!-- 账号卡片：未登录 → 登录/注册 CTA；已登录 → 资料 + 退出 -->
      <section v-if="!auth.isLoggedIn" class="card account-cta">
        <div class="account-cta-main">
          <strong>登录智为康乐</strong>
          <p>一个账号同步训练数据、管理实物与服务订单。不登录也能先本地体验全部训练功能。</p>
        </div>
        <div class="row">
          <button class="button primary" type="button" @click="open('/auth/login?redirect=/mine')">登 录</button>
          <button class="button ghost" type="button" @click="open('/auth/register?redirect=/mine')">注册新账号</button>
        </div>
      </section>
      <section v-else class="card account-cta">
        <div class="account-cta-main">
          <strong>{{ auth.displayName }}</strong>
          <p>手机号：{{ auth.user?.phoneMasked ?? '—' }} · 训练数据将随登录账号同步云端</p>
        </div>
        <div class="row">
          <button class="button" type="button" @click="open('/mine/password')">修改密码</button>
          <button class="button danger" type="button" @click="logout">退出登录</button>
        </div>
      </section>

      <section class="card">
        <div class="section-title">训练数据总览</div>
        <div class="mine-stats">
          <div class="mine-stat"><strong>{{ totalCount }}</strong><span>训练总次数</span><small>全部已保存训练</small></div>
          <div class="mine-stat"><strong class="mine-stat-duration">{{ formatTotalDuration() }}</strong><span>累计训练时长</span><small>按分钟折算</small></div>
          <div class="mine-stat"><strong>{{ todayCount }}</strong><span>今日训练次数</span><small>按完成日期统计</small></div>
          <div class="mine-stat"><strong class="mine-stat-dash">—</strong><span>连续打卡</span><small>占位 · 账号体系上线后开放</small></div>
        </div>
        <div class="mine-chips">
          <span class="mine-chip">待上报 {{ pendingCount }} 条</span>
          <span class="mine-chip mine-chip-muted">后端接入后自动上报</span>
        </div>
        <p class="muted small mine-note">周 / 月统计为占位说明：待账号体系与后端上线后提供周期统计与云端同步。</p>
        <p v-if="loading" class="muted small mine-note">正在读取本地训练记录…</p>
        <p v-else-if="loadError" class="error small mine-note">{{ loadError }}</p>
        <p v-else-if="!records.length" class="muted small mine-note">暂无训练记录：完成一次训练后，这里会自动汇总统计。</p>
      </section>

      <nav class="card list-card" aria-label="我的功能入口">
        <button class="list-row" type="button" @click="open('/mine/history')"><span class="row-ic" aria-hidden="true">📈</span><span class="row-main">训练数据历史<small>按游戏查看记录、详细结果与轨迹回放</small></span><span class="row-arrow" aria-hidden="true">›</span></button>
        <button class="list-row" type="button" @click="open('/devices')"><span class="row-ic" aria-hidden="true">⌚</span><span class="row-main">我的设备<small>连接管理 · 自然中心确认 · 个人活动范围</small></span><span class="row-arrow" aria-hidden="true">›</span></button>
        <button class="list-row" type="button" @click="open('/mine/orders')"><span class="row-ic" aria-hidden="true">🧾</span><span class="row-main">我的订单<small>实物 + 服务统一聚合</small></span><span class="row-arrow" aria-hidden="true">›</span></button>
        <button class="list-row" type="button" @click="open('/courses')"><span class="row-ic" aria-hidden="true">📚</span><span class="row-main">课程打卡<small>训练课程与打卡记录</small></span><span class="row-arrow" aria-hidden="true">›</span></button>
        <button class="list-row" type="button" @click="open('/mine/settings')"><span class="row-ic" aria-hidden="true">⚙️</span><span class="row-main">更新设置<small>当前版本 · 更新方式</small></span><span class="row-arrow" aria-hidden="true">›</span></button>
        <button class="list-row" type="button" @click="open('/mine/disclaimer')"><span class="row-ic" aria-hidden="true">📄</span><span class="row-main">免责声明与隐私<small>产品定位 · 使用说明 · 数据说明</small></span><span class="row-arrow" aria-hidden="true">›</span></button>
      </nav>

      <footer class="mine-footer">
        <p class="muted small">智为康乐 ZhiWellCare · v0.1.0</p>
        <p class="muted small">设备-训练目录：{{ catalogSourceStatus.message }}</p>
      </footer>
    </div>
  </main>
</template>

<style scoped>
.mine-hero-row { display: flex; align-items: center; gap: 14px; }
.mine-avatar {
  width: 56px; height: 56px; flex: 0 0 auto; display: grid; place-items: center;
  border-radius: 18px; background: rgba(255, 255, 255, .16);
  border: 1px solid rgba(255, 255, 255, .35); font-size: 26px;
}
.mine-hero-text { min-width: 0; }
.mine-hero-note { margin: 12px 0 0; color: rgba(255, 255, 255, .78); font-size: 12px; line-height: 1.7; }

.mine-body { display: flex; flex-direction: column; gap: 16px; }
.account-cta { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.account-cta-main { min-width: 0; }
.account-cta-main strong { font-size: 16px; display: block; }
.account-cta-main p { margin: 4px 0 0; color: var(--c-ink-2); font-size: 13px; line-height: 1.6; }
.account-cta .row { margin-top: 0; }
.mine-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; margin: 4px 0 14px; }
.mine-stat {
  display: flex; flex-direction: column; gap: 2px; min-width: 0;
  padding: 12px 14px; border-radius: 12px; background: var(--c-bg-2);
}
.mine-stat > strong { font-size: 22px; line-height: 1.25; color: var(--c-primary); overflow-wrap: anywhere; }
.mine-stat-duration { font-size: 16px; }
.mine-stat-dash { color: var(--c-ink-3); }
.mine-stat span { color: var(--c-ink); font-weight: 600; font-size: 14px; }
.mine-stat small { color: var(--c-ink-3); font-size: 11px; }

.mine-chips { display: flex; flex-wrap: wrap; gap: 8px; }
.mine-chip {
  display: inline-flex; align-items: center; gap: 6px; padding: 5px 12px;
  border-radius: 999px; background: var(--c-primary-soft); color: var(--c-primary); font-size: 12px;
}
.mine-chip-muted { background: var(--c-bg-3); color: var(--c-ink-2); }
.mine-note { margin: 10px 0 0; }

.mine-footer { display: grid; gap: 3px; text-align: center; padding: 6px 0 4px; }
.mine-footer p { margin: 0; }

@media (max-width: 767px) {
  .mine-stat > strong { font-size: 19px; }
  .mine-stat-duration { font-size: 15px; }
  .mine-avatar { width: 48px; height: 48px; font-size: 22px; }
}
</style>
