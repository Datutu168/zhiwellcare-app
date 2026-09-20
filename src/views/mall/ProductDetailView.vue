<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { contentService } from '../../app/AppServices'
import { readMallCart, writeMallCart } from '../../core/content/MallCart'
import type { MallGoodsItem } from '../../core/content/ContentTypes'

const route = useRoute()
const router = useRouter()

const buyOpen = ref(false)
const serviceNoteOpen = ref(false)
const addTip = ref(false)
let addTipTimer: number | null = null

const productId = computed(() => (typeof route.params.productId === 'string' ? route.params.productId : ''))
/** 商品内容统一来自 ContentService；商品不存在时 goods 为 null → 渲染空态。 */
const goods = ref<MallGoodsItem | null>(null)
const goodsLoaded = ref(false)
const isService = computed(() => goods.value?.kind === 'service')
const isPresale = computed(() => goods.value?.badges.includes('预售演示') ?? false)

/** 路由参数变化（同组件复用）时重新取数。 */
watch(productId, (id) => { void loadGoods(id) }, { immediate: true })

async function loadGoods(id: string): Promise<void> {
  try { goods.value = id ? await contentService.findGoods(id) : null }
  catch { goods.value = null }
  finally { goodsLoaded.value = true }
}

/** 强制展示的消费版通用声明（硬件 / 服务 / 品类未知分别措辞）。 */
const genericDisclaimer = computed(() => {
  const current = goods.value
  if (!current) return ''
  // 远端契约不提供品类：品类未知时用中性表述，避免把上门服务误标成消费级硬件。
  if (!current.kindKnown) {
    return '本商品为消费级商品（消费品）：实物为主动训练设备，无医疗器械功能、无被动电机驱动，训练动作完全由使用者主动发力完成；上门类为居家健身指导（非医疗服务）。请按说明使用并量力而行，感到不适时立即停止。'
  }
  return current.kind === 'hardware'
    ? '本商品为消费级主动训练设备（消费品），无医疗器械功能、无被动电机驱动，训练动作完全由使用者主动发力完成；请按说明使用并量力而行，感到不适时立即停止。'
    : '本服务为上门居家健身指导（非医疗服务），由合作服务商承接，仅提供居家健身动作演示与跟练支持；请量力而行，感到不适时立即停止，必要时寻求线下专业帮助。'
})

/** 品类角标：品类未知时不写死硬件/服务。 */
const kindChipLabel = computed(() => {
  const current = goods.value
  if (!current?.kindKnown) return '商品'
  return current.kind === 'service' ? '服务' : '硬件'
})

onBeforeUnmount(() => {
  if (addTipTimer !== null) window.clearTimeout(addTipTimer)
})

function addToCart(): void {
  const current = readMallCart()
  const existing = current.find((item) => item.productId === productId.value)
  if (existing) existing.quantity += 1
  else current.push({ productId: productId.value, quantity: 1 })
  writeMallCart(current)
  addTip.value = true
  if (addTipTimer !== null) window.clearTimeout(addTipTimer)
  addTipTimer = window.setTimeout(() => { addTip.value = false }, 2_200)
}

function goBackToMall(): void {
  void router.push('/mall')
}
</script>

<template>
  <main class="content-page">
    <template v-if="goods">
      <div class="mall-detail-top">
        <button type="button" class="button ghost small" @click="goBackToMall">← 返回商城</button>
        <p class="mall-detail-crumb muted small">商城 / {{ goods.category }} / {{ goods.name }}</p>
      </div>

      <div class="mall-detail-grid">
        <!-- 左侧：cover 大图区 -->
        <section class="card mall-detail-cover-card">
          <div class="goods-cover mall-detail-cover" :class="{ 'is-service': isService }">
            <span class="mall-kind-chip" :class="{ 'is-service': isService }">{{ kindChipLabel }}</span>
            <img v-if="goods.coverUrl" class="cover-img" :src="goods.coverUrl" :alt="goods.name" />
            <span v-else class="mall-cover-emoji" aria-hidden="true">{{ goods.cover }}</span>
            <span class="mall-detail-price-pill">演示价 {{ goods.priceLabel }}/{{ goods.priceUnit }}</span>
          </div>
          <div v-if="goods.relatedHardware" class="mall-related-hint">
            <span class="tag-chip is-warn">需搭配</span>
            <span>桌面力矩主动训练底座（另购）使用</span>
          </div>
          <p v-if="isPresale" class="mall-presale-hint">预售演示占位：正式版开售并接入订单系统与支付后开放下单，当前不产生真实交易。</p>
        </section>

        <!-- 右侧：信息与操作区 -->
        <section class="card mall-detail-info">
          <p class="eyebrow">{{ goods.category }}</p>
          <h1 class="mall-detail-name">{{ goods.name }}</h1>
          <p class="mall-detail-summary">{{ goods.summary }}</p>
          <div class="tag-chip-row">
            <span v-for="badge in goods.badges" :key="badge" class="tag-chip" :class="{ 'is-cyan': isService }">{{ badge }}</span>
            <span v-if="isService" class="tag-chip is-cyan">上门居家健身指导（非医疗服务）</span>
          </div>
          <a v-if="goods.detailUrl" class="mall-detail-link" :href="goods.detailUrl" target="_blank" rel="noopener">
            查看官方商品详情 <span aria-hidden="true">›</span>
          </a>

          <div class="mall-detail-price">
            <span class="mall-detail-price-num">演示价 {{ goods.priceLabel }}<span class="mall-detail-price-unit">/{{ goods.priceUnit }}</span></span>
            <span class="mall-detail-price-note">正式版接入支付后开放下单，当前不产生真实订单与扣款。</span>
          </div>

          <div class="mall-actions">
            <template v-if="isService">
              <button type="button" class="button primary wide" @click="buyOpen = true">立即购买（演示）</button>
              <button type="button" class="button ghost wide" @click="serviceNoteOpen = true">服务说明与免责声明</button>
            </template>
            <template v-else>
              <button type="button" class="button wide" @click="addToCart">{{ addTip ? '✓ 已加入购物车' : '加入购物车' }}</button>
              <button type="button" class="button primary wide" @click="buyOpen = true">立即购买（演示）</button>
            </template>
          </div>
          <p class="mall-demo-line">演示环境：不发起任何支付或网络请求；购物车数据仅保存在本机。</p>
        </section>
      </div>

      <!-- 规格 / 套餐内容 -->
      <section v-if="goods.specs.length" class="card mall-detail-section">
        <h2 class="section-title">{{ isService ? '套餐内容' : '规格与能力' }}</h2>
        <ul class="mall-spec-list">
          <li v-for="(item, index) in goods.specs" :key="index">{{ item }}</li>
        </ul>
      </section>

      <!-- 详情说明 -->
      <section v-if="goods.detail.length" class="card mall-detail-section">
        <h2 class="section-title">详情说明</h2>
        <p v-for="(paragraph, index) in goods.detail" :key="index" class="mall-detail-paragraph">{{ paragraph }}</p>
      </section>

      <!-- 服务类：购买与核销占位说明 -->
      <section v-if="isService" class="card mall-detail-section">
        <h2 class="section-title">购买与核销（正式版占位）</h2>
        <ul class="mall-spec-list">
          <li>订单处理：正式版由 Golang 后端订单系统生成并处理订单状态。</li>
          <li>账号打通：经 UnionID 打通 APP 账号与合作服务商账号。</li>
          <li>核销与预约：由合作服务商小程序完成；APP 内不展示派单 / 工单信息。</li>
          <li>当前演示版：不产生真实订单、不扣款，以上流程均未启用。</li>
        </ul>
      </section>

      <!-- 强制免责声明卡（专属 + 消费版通用） -->
      <section class="mall-disclaimers">
        <div v-if="goods.disclaimer" class="compliance-note">
          <strong>专属免责声明：</strong><span>{{ goods.disclaimer }}</span>
        </div>
        <div class="compliance-note">
          <strong>{{ isService ? '服务通用声明：' : (goods.kindKnown ? '消费品通用声明：' : '商品通用声明：') }}</strong><span>{{ genericDisclaimer }}</span>
        </div>
      </section>

      <div class="mall-detail-bottom">
        <button type="button" class="button ghost wide" @click="goBackToMall">返回商城</button>
      </div>

      <!-- 立即购买（演示）弹层 -->
      <Teleport to="body">
        <div v-if="buyOpen" class="device-dialog-backdrop" @click.self="buyOpen = false">
          <section class="device-dialog" role="dialog" aria-modal="true" aria-label="立即购买（演示）">
            <header>
              <div><p class="eyebrow">演示购买</p><h2>立即购买（演示）</h2></div>
              <button type="button" class="button" @click="buyOpen = false">关闭</button>
            </header>
            <p class="mall-dialog-paragraph">当前为演示环境：点击购买不会产生真实订单，也不会发生任何扣款；页面展示价格均为「演示价」。</p>
            <p class="mall-dialog-paragraph">正式版接入 Golang 后端订单系统与支付后，「立即购买」将开放真实下单；本演示版本不发起任何网络请求。</p>
            <footer class="mall-dialog-actions"><button type="button" class="button primary wide" @click="buyOpen = false">知道了</button></footer>
          </section>
        </div>
      </Teleport>

      <!-- 服务说明与免责声明弹层（仅服务类） -->
      <Teleport to="body">
        <div v-if="serviceNoteOpen" class="device-dialog-backdrop" @click.self="serviceNoteOpen = false">
          <section class="device-dialog" role="dialog" aria-modal="true" aria-label="服务说明与免责声明">
            <header>
              <div><p class="eyebrow">服务说明</p><h2>服务说明与免责声明</h2></div>
              <button type="button" class="button" @click="serviceNoteOpen = false">关闭</button>
            </header>
            <div class="compliance-note"><strong>服务性质：</strong><span>本服务为上门居家健身指导（非医疗服务），由智为康乐合作服务商提供上门健身动作演示与跟练支持，不包含任何专业健康服务内容。</span></div>
            <h3 class="mall-dialog-h3">购买与核销（正式版占位）</h3>
            <ul class="mall-spec-list">
              <li>订单处理：正式版由 Golang 后端订单系统生成并处理订单状态。</li>
              <li>账号打通：经 UnionID 打通 APP 账号与合作服务商账号。</li>
              <li>核销与预约：由合作服务商小程序完成；APP 内不展示派单 / 工单信息。</li>
            </ul>
            <template v-if="goods.disclaimer">
              <h3 class="mall-dialog-h3">专属免责声明</h3>
              <p class="mall-dialog-paragraph">{{ goods.disclaimer }}</p>
            </template>
            <footer class="mall-dialog-actions"><button type="button" class="button primary wide" @click="serviceNoteOpen = false">知道了</button></footer>
          </section>
        </div>
      </Teleport>
    </template>

    <!-- 加载中：内容来自 ContentService（可能等待后端接口） -->
    <template v-else-if="!goodsLoaded">
      <div class="empty-state mall-not-found">
        <div class="empty-ic">⏳</div>
        <p>正在加载商品…</p>
      </div>
    </template>

    <!-- 商品缺失空状态 -->
    <template v-else>
      <div class="empty-state mall-not-found">
        <div class="empty-ic">🧭</div>
        <p>未找到该商品，可能已下架或链接有误。</p>
        <button type="button" class="button primary" @click="goBackToMall">返回商城</button>
      </div>
    </template>
  </main>
</template>

<style scoped>
.mall-detail-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}
.mall-detail-crumb { margin: 0; min-width: 0; }

.mall-detail-grid { display: grid; gap: 18px; margin-bottom: 18px; }

.goods-cover.mall-detail-cover { height: 240px; font-size: 0; }
.goods-cover.mall-detail-cover.is-service { background: linear-gradient(150deg, #e6fbf8, #dff3ef); }
.mall-kind-chip {
  position: absolute;
  top: 12px;
  left: 12px;
  z-index: 1;
  padding: 4px 12px;
  border-radius: 999px;
  background: var(--c-primary);
  color: #fff;
  font-size: 12px;
  font-weight: 700;
}
.mall-kind-chip.is-service { background: #08978b; }
.mall-cover-emoji { font-size: 78px; line-height: 1; }
/* 远端下发 CDN 商品图时用图片替代 emoji 占位（内置兜底数据无 coverUrl） */
.cover-img { max-width: 100%; max-height: 100%; object-fit: contain; border-radius: 12px; }
.mall-detail-link { display: inline-block; margin: 10px 0 0; color: var(--c-primary); font-size: 13px; font-weight: 700; text-decoration: none; }
.mall-detail-link:hover { text-decoration: underline; }
.mall-detail-price-pill {
  position: absolute;
  left: 12px;
  bottom: 12px;
  padding: 5px 14px;
  border-radius: 999px;
  background: var(--grad-brand-soft);
  color: #fff;
  font-size: 14px;
  font-weight: 800;
  box-shadow: 0 6px 16px rgba(13, 180, 204, .3);
}
.mall-related-hint {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  color: var(--c-ink-2);
  font-size: 13px;
}
.mall-presale-hint {
  margin: 12px 0 0;
  padding: 10px 12px;
  border: 1px solid var(--c-line);
  border-left: 4px solid var(--c-primary-2);
  border-radius: 10px;
  background: var(--c-warn-bg);
  color: var(--c-warn-ink);
  font-size: 12px;
  line-height: 1.7;
}

.mall-detail-info { display: flex; flex-direction: column; }
.mall-detail-name { margin: 2px 0 8px; font-size: clamp(20px, 2.4vw, 26px); line-height: 1.35; }
.mall-detail-summary { color: var(--c-ink-2); font-size: 14px; margin: 0 0 12px; line-height: 1.7; }
.mall-detail-price {
  display: grid;
  gap: 6px;
  margin-top: 14px;
  padding: 14px;
  border-radius: 12px;
  background: var(--c-bg-2);
}
.mall-detail-price-num { color: #e2574c; font-size: 26px; font-weight: 800; line-height: 1.2; }
.mall-detail-price-unit { color: var(--c-ink-3); font-size: 14px; font-weight: 700; margin-left: 4px; }
.mall-detail-price-note { color: var(--c-ink-3); font-size: 12px; line-height: 1.6; }

.mall-actions { display: grid; gap: 10px; margin-top: 14px; }
.mall-demo-line { margin: 10px 0 0; color: var(--c-ink-3); font-size: 12px; text-align: center; }

.mall-detail-section { margin-bottom: 16px; }
.mall-spec-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 10px; }
.mall-spec-list li {
  position: relative;
  padding: 10px 12px 10px 30px;
  border-radius: 10px;
  background: var(--c-bg-2);
  color: var(--c-ink-2);
  font-size: 13px;
  line-height: 1.6;
}
.mall-spec-list li::before {
  content: '✓';
  position: absolute;
  left: 12px;
  top: 10px;
  color: var(--c-accent);
  font-weight: 800;
}
.mall-detail-paragraph { margin: 0 0 12px; color: var(--c-ink-2); font-size: 14px; line-height: 1.8; }
.mall-detail-paragraph:last-child { margin-bottom: 0; }

.mall-disclaimers { display: grid; gap: 10px; margin: 6px 0 18px; }
.mall-detail-bottom { margin-bottom: 6px; }

.mall-not-found { margin-top: 8vh; }
.mall-dialog-paragraph { margin: 0 0 10px; color: var(--c-ink-2); font-size: 14px; line-height: 1.8; }
.mall-dialog-h3 { margin: 16px 0 8px; font-size: 15px; }
.mall-dialog-actions { display: grid; margin-top: 16px; }

@media (min-width: 920px) {
  .mall-detail-grid { grid-template-columns: 340px minmax(0, 1fr); align-items: start; }
  .mall-detail-cover-card { position: sticky; top: 18px; }
  .mall-actions { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .mall-detail-info .mall-actions .button.wide { width: 100%; }
  .mall-demo-line { text-align: left; }
}
@media (max-width: 479px) {
  .goods-cover.mall-detail-cover { height: 190px; }
  .mall-cover-emoji { font-size: 60px; }
}
</style>
