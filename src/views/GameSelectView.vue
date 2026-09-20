<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  catalogService,
  connectionManager,
  motionProfileService,
  resolveCurrentDeviceModel,
  sensorService,
} from '../app/AppServices'
import { catalogSourceStatus } from '../core/catalog/CatalogService'
import { tagLabel, type MatchTag } from '../core/catalog/CapabilityTags'
import { describeMissingTags } from '../core/catalog/tagMatching'
import type { DeviceGameProfile, DeviceModelInfo, GameCatalogEntry } from '../core/catalog/DeviceCatalogTypes'
import { gameResourceResolver } from '../core/resources/GameResourceResolver'
import type { SensorConnectionSnapshot } from '../core/sensor/SensorConnectionManager'

/** 大厅封面在 CDN 资源目录中的相对路径（清单登记后才启用，否则用内置字形封面）。 */
const CDN_COVER_PATH = 'cover.png'

/** 大厅列表：后端目录游戏 + 能运行该游戏的设备型号名（首台用于推荐展示）。 */
type CatalogGame = GameCatalogEntry & { compatibleModels: string[] }

type CardState = 'ready' | 'tag-missing' | 'no-device'

interface GameCardModel {
  entry: CatalogGame
  recommended: string
  state: CardState
  missing: MatchTag[]
  driver: string
}

const route = useRoute()
const router = useRouter()

const loading = ref(true)
const loadError = ref('')
const games = ref<CatalogGame[]>([])
const connection = ref<SensorConnectionSnapshot>(connectionManager.getSnapshot())
const connected = ref(false)
const activeModel = ref<DeviceModelInfo | null>(null)
const profile = ref<DeviceGameProfile | null>(null)
const rangeReady = ref(false)
/** 游戏 id → CDN 封面地址：仅清单登记 cover.png 且 resourceUrl 有效时才有值。 */
const coverUrls = ref<Record<string, string>>({})

const routeError = computed(() => (route.query.error === 'game-unavailable' ? '该训练游戏不存在或尚未开放。' : ''))

/** 目录源徽标：本地目录 / 后端目录 / 后端不可用已回退本地。 */
const sourceKindLabel = computed(() => {
  switch (catalogSourceStatus.value.kind) {
    case 'http':
      return '后端目录'
    case 'http-fallback-mock':
      return '已回退本地目录'
    default:
      return '本地目录'
  }
})

/** 无激活设备 / 设备未连接时的顶部引导条。 */
const heroBanner = computed<{ text: string; cta: string } | null>(() => {
  if (!connection.value.binding) return { text: '连接设备解锁适配游戏', cta: '去连接' }
  if (!connected.value) return { text: '当前设备未连接，连接成功后可开始训练', cta: '去设备页' }
  return null
})

/** 卡片视图模型：标签满足 + 已连接 → 可训练；已连接但不满足 → 提示缺失能力；其余按未连接处理。 */
const cards = computed<GameCardModel[]>(() =>
  games.value.map((entry) => {
    const model = activeModel.value
    const unavailable = profile.value?.unavailableGames.find((game) => game.gameId === entry.gameId)
    const state: CardState = (() => {
      if (model && connected.value) {
        const available = profile.value?.games.some((game) => game.gameId === entry.gameId)
        return available ? 'ready' : 'tag-missing'
      }
      return 'no-device'
    })()
    return {
      entry,
      recommended: entry.compatibleModels[0] ?? '',
      state,
      missing: unavailable?.missingTags ?? [],
      driver: model?.name ?? '',
    }
  }),
)

let unsubscribeConnection: (() => void) | null = null
let unsubscribeSensor: (() => void) | null = null
let refreshRunning = false
let refreshScheduled = false

onMounted(() => {
  // 绑定设备变化（切换/忘记/首次连接）后重新解析型号与游戏可用画像。
  unsubscribeConnection = connectionManager.onChanged((next) => {
    connection.value = next
    void scheduleRefresh()
  })
  // 传感器状态决定卡片是否可进入训练；订阅回调会立即以当前快照执行一次。
  unsubscribeSensor = sensorService.onSnapshot((next) => {
    connected.value = next.state === 'connected'
  })
  void scheduleRefresh()
})

onBeforeUnmount(() => {
  unsubscribeConnection?.()
  unsubscribeSensor?.()
})

async function scheduleRefresh(): Promise<void> {
  if (refreshRunning) {
    refreshScheduled = true
    return
  }
  refreshRunning = true
  do {
    refreshScheduled = false
    await runRefresh()
  } while (refreshScheduled)
  refreshRunning = false
}

async function runRefresh(): Promise<void> {
  try {
    const [list, model] = await Promise.all([catalogService.listGamesWithModels(), resolveCurrentDeviceModel()])
    profile.value = model ? await catalogService.getDeviceGameProfile(model) : null
    games.value = list
    activeModel.value = model
    // Profile 为普通对象（非响应式），回到本页时组件会重建并在此重读。
    rangeReady.value = motionProfileService.getCurrent().measuredRange !== null
    void loadCdnCovers(list)
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

/** 开始训练：范围未设定先进入个人活动范围测量；未连接则引导去设备页。 */
function startTraining(gameId: string): void {
  if (motionProfileService.getCurrent().measuredRange === null) {
    void router.push(`/calibration?return=${encodeURIComponent(`/training/${gameId}`)}`)
    return
  }
  if (!connected.value) {
    loadError.value = '请先连接训练设备。'
    void router.push('/devices')
    return
  }
  void router.push(`/training/${gameId}`)
}

function onAction(card: GameCardModel): void {
  if (card.state === 'ready') {
    startTraining(card.entry.gameId)
    return
  }
  void router.push('/devices')
}

function actionLabel(card: GameCardModel): string {
  if (card.state === 'ready') return '开始训练'
  return connection.value.binding ? '去设备页' : '去连接'
}

/** 不可直接训练卡片的引导文案（缺失能力 / 未连接 / 型号未收录）。 */
function cardHint(card: GameCardModel): string {
  if (card.state === 'tag-missing') {
    if (card.missing.length) return `需连接具备：${describeMissingTags(card.missing)} 能力的设备`
    return '当前设备不支持该游戏，请前往设备页更换设备'
  }
  if (connection.value.binding) {
    if (!activeModel.value) return '当前设备未收录到游戏目录，请前往设备页更换设备'
    return card.driver ? `设备未连接，由「${card.driver}」驱动，恢复连接后可开始训练` : '设备未连接，恢复连接后可开始训练'
  }
  return card.recommended ? `连接「${card.recommended}」后可训练` : '连接目录中的适配设备后可训练'
}

function tagClass(card: GameCardModel, tag: MatchTag): string {
  if (card.state === 'ready') return 'is-cyan'
  if (card.state === 'tag-missing') return card.missing.includes(tag) ? 'is-warn' : 'is-off'
  return 'is-off'
}

function formatPresets(presets: readonly number[]): string {
  // 空预设代表由赛道或目标完成条件结束，而不是缺失目录数据。
  return presets.length ? `${presets.join(' / ')} 分钟` : '固定赛道'
}

function coverGlyph(name: string): string {
  return name.slice(0, 2)
}

/**
 * 远程封面解析：清单声明了 cover.png 才使用 CDN 地址（内存 + localStorage 缓存清单）；
 * resourceUrl 为空、清单不可用或未声明该文件时静默回退内置字形封面，绝不因为 CDN 问题让大厅不可用。
 */
async function loadCdnCovers(list: CatalogGame[]): Promise<void> {
  try {
    const resolved = await Promise.all(list.map(async (entry) => {
      const url = await gameResourceResolver.resolveDeclaredUrl(entry, CDN_COVER_PATH)
      return url ? ([entry.gameId, url] as const) : null
    }))
    const next: Record<string, string> = {}
    for (const item of resolved) if (item) next[item[0]] = item[1]
    coverUrls.value = next
  } catch {
    coverUrls.value = {}
  }
}

/** 远程封面加载失败（404/断网）时静默回退内置字形封面。 */
function onCoverError(gameId: string): void {
  const next = { ...coverUrls.value }
  delete next[gameId]
  coverUrls.value = next
}
</script>

<template>
  <main class="content-page">
    <header class="page-hero">
      <p class="eyebrow">训练中心</p>
      <h1>训练游戏</h1>
      <p>连接设备后自动过滤适配游戏；新增设备与游戏由后端目录动态下发（本地演示目录模式）</p>
      <div class="hero-tags">
        <span class="hero-tag" :title="catalogSourceStatus.message">{{ sourceKindLabel }}</span>
        <span v-if="connected && activeModel" class="hero-tag">设备：{{ activeModel.name }}</span>
        <span v-if="connected && !rangeReady" class="hero-tag">未完成个人活动范围测量</span>
      </div>
    </header>

    <p v-if="routeError" class="error">{{ routeError }}</p>
    <p v-if="loadError" class="error">{{ loadError }}</p>

    <section v-if="heroBanner" class="hub-banner">
      <p>{{ heroBanner.text }}</p>
      <button class="button primary" @click="router.push('/devices')">{{ heroBanner.cta }}</button>
    </section>

    <div v-if="loading" class="empty-state"><p>游戏目录加载中…</p></div>

    <div v-else-if="!cards.length" class="empty-state"><div class="empty-ic">🎮</div><p>暂无游戏，敬请期待（目录由后端下发）</p></div>

    <div v-else class="grid-cards auto">
      <article v-for="card in cards" :key="card.entry.gameId" class="card game-card">
        <div class="game-card-cover grad"><img v-if="coverUrls[card.entry.gameId]" class="game-cover-img" :src="coverUrls[card.entry.gameId]" :alt="card.entry.name" @error="onCoverError(card.entry.gameId)" /><span v-else class="cover-glyph">{{ coverGlyph(card.entry.name) }}</span></div>
        <div>
          <div class="game-title-line">
            <h3>{{ card.entry.name }}</h3>
            <span class="tag-chip" :class="card.state === 'ready' ? 'is-cyan' : 'is-off'">{{ card.entry.categoryLabel }}</span>
          </div>
          <p class="muted small">{{ card.entry.summary }}</p>
        </div>
        <dl class="game-meta">
          <div><dt>时长预设</dt><dd>{{ formatPresets(card.entry.durationPresetsMin) }}</dd></div>
          <div><dt>推荐设备</dt><dd>{{ card.recommended || '暂无适配设备' }}</dd></div>
        </dl>
        <div class="tag-chip-row">
          <span v-for="tag in card.entry.requiredTags" :key="tag" class="tag-chip" :class="tagClass(card, tag)">{{ tagLabel(tag) }}</span>
        </div>
        <footer class="game-state">
          <div class="game-state-note">
            <p v-if="card.state === 'ready' && card.driver" class="muted small">由「{{ card.driver }}」驱动</p>
            <p v-if="card.state === 'ready' && !rangeReady" class="game-lock-hint">首次开始前需先完成个人活动范围测量</p>
            <p v-else-if="card.state !== 'ready'" class="game-lock-hint">{{ cardHint(card) }}</p>
          </div>
          <button class="button" :class="card.state === 'ready' ? 'primary' : 'ghost'" :title="card.state === 'ready' && !rangeReady ? '尚未完成个人活动范围测量，点击后将先进入测量流程' : undefined" @click="onAction(card)">{{ actionLabel(card) }}</button>
        </footer>
      </article>
    </div>
  </main>
</template>

<style scoped>
.game-card p { flex: initial; }
.cover-glyph { font-size: 40px; font-weight: 800; letter-spacing: .12em; }
.game-cover-img { width: 100%; height: 100%; border-radius: 12px; object-fit: cover; display: block; }
.game-title-line { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.game-title-line h3 { margin: 0; }
.game-meta { display: grid; grid-template-columns: auto 1fr; gap: 2px 12px; margin: 0 0 4px; font-size: 12px; }
.game-meta div { display: contents; }
.game-meta dt { color: var(--c-ink-3); white-space: nowrap; }
.game-meta dd { margin: 0; color: var(--c-ink-2); overflow-wrap: anywhere; }
.game-state { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; margin-top: auto; }
.game-state-note { flex: 1; min-width: 0; }
.game-state-note p { margin: 0 0 2px; }
.game-state .button { flex: 0 0 auto; }
.hub-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding: 14px 16px;
  margin-bottom: 18px;
  border-radius: var(--radius);
  background: linear-gradient(120deg, rgba(13, 213, 193, .14), rgba(4, 54, 167, .07));
  border: 1px solid rgba(13, 213, 193, .4);
}
.hub-banner p { margin: 0; font-size: 14px; color: var(--c-ink-2); }
</style>
