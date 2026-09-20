<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { firmwareUpdateService, motionProfileService } from '../app/AppServices'
import UpdateSettingsCard from '../components/update/UpdateSettingsCard.vue'
import type { FirmwareCheckResult } from '../platform/update/FirmwareUpdateService'
import { useDeviceLibraryStore } from '../stores/deviceLibrary'

const router = useRouter()
const profile = computed(() => motionProfileService.getCurrent())
const library = useDeviceLibraryStore()

// 固件检查只读展示：只查询版本信息，不执行任何刷写动作，失败也只显示提示。
const firmwareChecking = ref(false)
const firmwareResult = ref<FirmwareCheckResult | null>(null)

/** 当前机型 id：取不到（未连接/未识别设备）时不请求后端，直接显示“未连接设备”。 */
const firmwareModelId = computed(() => library.activeModelId ?? '')

const firmwareStatus = computed(() => {
  if (!firmwareModelId.value) return '未连接设备'
  if (firmwareChecking.value) return '正在检查…'
  const result = firmwareResult.value
  if (!result) return '尚未检查'
  if (result.error) return '暂时无法检查'
  return result.available ? `有新版本 v${result.version ?? '--'}` : '已是最新'
})

const firmwareNotes = computed(() => (firmwareResult.value?.available ? (firmwareResult.value.notes ?? []) : []))

async function checkFirmware(): Promise<void> {
  const modelId = firmwareModelId.value
  if (!modelId || firmwareChecking.value) return
  firmwareChecking.value = true
  try {
    // 设备固件版本暂未纳入本地状态（BLE 版本寄存器未接入），以空基线查询，由后端按型号返回最新固件。
    firmwareResult.value = await firmwareUpdateService.checkFirmware(modelId, '')
  } catch {
    firmwareResult.value = { available: false, error: '固件检查失败' }
  } finally {
    firmwareChecking.value = false
  }
}

onMounted(() => { void checkFirmware() })

// Settings 仅保存长期配置；日常设备连接统一由顶部状态菜单处理。
function range(value: number | undefined): string { return value === undefined ? '--' : `${value.toFixed(1)}°` }
</script>

<template><main class="content-page"><p class="eyebrow">Settings</p><h1>设置</h1><section class="card"><h2>个人活动范围</h2><template v-if="profile.measuredRange"><p>实测活动范围：前 {{ range(profile.measuredRange.forwardMax) }} · 后 {{ range(profile.measuredRange.backwardMax) }} · 左 {{ range(profile.measuredRange.leftMax) }} · 右 {{ range(profile.measuredRange.rightMax) }}</p><p class="muted">当前训练范围：前 {{ range(profile.activeRange.forwardMax) }} · 后 {{ range(profile.activeRange.backwardMax) }} · 左 {{ range(profile.activeRange.leftMax) }} · 右 {{ range(profile.activeRange.rightMax) }}</p><p class="muted small">训练比例 {{ (profile.trainingRatio * 100).toFixed(0) }}% · 死区 {{ profile.horizontalDeadZone }}° / {{ profile.verticalDeadZone }}°</p></template><p v-else class="muted">尚未完成个人活动范围测量。</p><button class="button primary" @click="router.push('/calibration?source=settings')">{{ profile.measuredRange ? '重新测量个人活动范围' : '开始个人活动范围测量' }}</button></section><section class="card"><h2>设备固件</h2><div class="update-version-row"><span>更新状态</span><strong>{{ firmwareStatus }}</strong></div><p v-if="firmwareNotes.length" class="muted small">更新说明：{{ firmwareNotes.join('；') }}</p><p class="muted small">仅展示固件更新信息，更新需在设备页按服务端白名单校验后执行。</p></section><UpdateSettingsCard /><section class="card"><h2>高级</h2><button class="button" @click="router.push('/mine/settings/debug')">开发者诊断</button></section></main></template>
