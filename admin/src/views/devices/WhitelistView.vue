<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import {
  adminApi,
  apiErrorText,
  type DeviceKeyPayload,
  type DeviceMappingItem,
  type DeviceModelBrief,
  type DeviceWhitelistItem,
  type WhitelistPayload,
} from '../../api/admin'
import { PERM } from '../../constants/permissions'
import { useAdminPermissionsStore } from '../../stores/adminPermissions'

type TabKey = 'whitelist' | 'mapping'

interface EntryForm {
  deviceKey: string
  modelId: string
  note: string
  enabled: boolean
}

const DEVICE_KEY_PATTERN = /^[A-Za-z0-9][A-Za-z0-9:._-]{1,127}$/

const activeTab = ref<TabKey>('whitelist')

const perms = useAdminPermissionsStore()
/** 白名单与映射分别对应不同的写权限（后端如此鉴权）。 */
const canWrite = computed(() => perms.allows(isWhitelist.value ? [PERM.whitelistWrite] : [PERM.deviceWrite]))

const whitelist = ref<DeviceWhitelistItem[]>([])
const mappings = ref<DeviceMappingItem[]>([])
const loadingWhitelist = ref(false)
const loadingMapping = ref(false)
const errorWhitelist = ref('')
const errorMapping = ref('')
const keywordWhitelist = ref('')
const keywordMapping = ref('')

const models = ref<DeviceModelBrief[]>([])
const modelError = ref('')

const dialogVisible = ref(false)
const editing = ref(false)
const saving = ref(false)
const formRef = ref<FormInstance>()
const form = reactive<EntryForm>({ deviceKey: '', modelId: '', note: '', enabled: true })

const rules: FormRules = {
  deviceKey: [
    { required: true, message: '请输入设备 key', trigger: 'blur' },
    { pattern: DEVICE_KEY_PATTERN, message: '以字母或数字开头，仅含字母/数字/:_- . ，共 2-128 位', trigger: 'blur' },
  ],
  modelId: [{ required: true, message: '请选择或输入设备型号 modelId', trigger: 'change' }],
}

onMounted(() => {
  void loadAll()
  void loadModels()
})

const isWhitelist = computed(() => activeTab.value === 'whitelist')

const filteredWhitelist = computed<DeviceWhitelistItem[]>(() =>
  matchFilter(whitelist.value, keywordWhitelist.value),
)
const filteredMappings = computed<DeviceMappingItem[]>(() =>
  matchFilter(mappings.value, keywordMapping.value),
)

function matchFilter<T extends { deviceKey: string; modelId: string; note?: string }>(
  rows: T[],
  keyword: string,
): T[] {
  const kw = keyword.trim().toLowerCase()
  if (!kw) return rows
  return rows.filter(
    (row) =>
      row.deviceKey.toLowerCase().includes(kw) ||
      (row.modelId ?? '').toLowerCase().includes(kw) ||
      (row.note ?? '').toLowerCase().includes(kw),
  )
}

async function loadAll(): Promise<void> {
  await Promise.all([loadWhitelist(), loadMappings()])
}

async function loadWhitelist(): Promise<void> {
  loadingWhitelist.value = true
  try {
    const data = await adminApi.listWhitelist()
    whitelist.value = data?.items ?? []
    errorWhitelist.value = ''
  } catch (e) {
    whitelist.value = []
    errorWhitelist.value = apiErrorText(e, '白名单加载失败')
  } finally {
    loadingWhitelist.value = false
  }
}

async function loadMappings(): Promise<void> {
  loadingMapping.value = true
  try {
    const data = await adminApi.listMappings()
    mappings.value = data?.items ?? []
    errorMapping.value = ''
  } catch (e) {
    mappings.value = []
    errorMapping.value = apiErrorText(e, '设备映射加载失败')
  } finally {
    loadingMapping.value = false
  }
}

/** 型号下拉失败不阻塞录入：仍可手动输入 modelId。 */
async function loadModels(): Promise<void> {
  try {
    const data = await adminApi.listDeviceModels()
    models.value = Array.isArray(data) ? data : []
    modelError.value = ''
  } catch (e) {
    models.value = []
    modelError.value = apiErrorText(e, '设备型号列表加载失败')
  }
}

function findRow(deviceKey: string): DeviceWhitelistItem | DeviceMappingItem | null {
  if (isWhitelist.value) return whitelist.value.find((row) => row.deviceKey === deviceKey) ?? null
  return mappings.value.find((row) => row.deviceKey === deviceKey) ?? null
}

async function openAdd(): Promise<void> {
  editing.value = false
  form.deviceKey = ''
  form.modelId = ''
  form.note = ''
  form.enabled = true
  dialogVisible.value = true
  await nextTick()
  formRef.value?.clearValidate()
}

async function openEdit(row: DeviceWhitelistItem | DeviceMappingItem): Promise<void> {
  editing.value = true
  form.deviceKey = row.deviceKey
  form.modelId = row.modelId ?? ''
  form.note = row.note ?? ''
  form.enabled = 'enabled' in row ? row.enabled !== false : true
  dialogVisible.value = true
  await nextTick()
  formRef.value?.clearValidate()
}

/** 白名单/映射的新增接口都是 upsert：同一 deviceKey 再次提交即为更新。 */
function buildPayload(deviceKey: string, modelId: string, note: string): DeviceKeyPayload | WhitelistPayload {
  if (isWhitelist.value) return { deviceKey, modelId, note, enabled: form.enabled }
  return { deviceKey, modelId, note }
}

function addEntry(payload: DeviceKeyPayload | WhitelistPayload): Promise<unknown> {
  return isWhitelist.value ? adminApi.addWhitelist(payload) : adminApi.addMapping(payload)
}

function deleteEntry(deviceKey: string): Promise<{ deleted: boolean }> {
  return isWhitelist.value ? adminApi.deleteWhitelist(deviceKey) : adminApi.deleteMapping(deviceKey)
}

function modeText(): string {
  return isWhitelist.value ? '白名单' : '设备映射'
}

async function submit(): Promise<void> {
  if (!formRef.value) return
  const ok = await formRef.value.validate().catch(() => false)
  if (!ok) return

  const deviceKey = form.deviceKey.trim()
  const modelId = form.modelId.trim()
  const note = form.note.trim()
  const original = editing.value ? findRow(deviceKey) : null

  if (original) {
    const unchanged =
      (original.modelId ?? '') === modelId &&
      (original.note ?? '') === note &&
      (!isWhitelist.value || ('enabled' in original && (original.enabled !== false) === form.enabled))
    if (unchanged) {
      dialogVisible.value = false
      ElMessage.info('未做任何修改')
      return
    }
  }

  saving.value = true
  try {
    await addEntry(buildPayload(deviceKey, modelId, note))
    ElMessage.success(editing.value ? '已更新' : `${modeText()}已新增`)
    dialogVisible.value = false
    await loadAll()
  } catch (e) {
    ElMessage.error(apiErrorText(e, '保存失败'))
  } finally {
    saving.value = false
  }
}

async function remove(row: DeviceWhitelistItem | DeviceMappingItem): Promise<void> {
  try {
    await ElMessageBox.confirm(
      `删除后该设备将不再被识别。确认从${modeText()}中删除「${row.deviceKey}」？`,
      '删除确认',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    await deleteEntry(row.deviceKey)
    ElMessage.success('已删除')
    await loadAll()
  } catch (e) {
    ElMessage.error(apiErrorText(e, '删除失败'))
  }
}

function modelLabel(modelId: string): string {
  const hit = models.value.find((model) => model.modelId === modelId)
  return hit?.name ? `${hit.name}（${modelId}）` : modelId
}
</script>

<template>
  <div>
    <el-card class="page-card" shadow="never">
      <el-tabs v-model="activeTab">
        <el-tab-pane name="whitelist">
          <template #label>
            <span>设备白名单（{{ whitelist.length }}）</span>
          </template>

          <div class="table-toolbar">
            <div class="toolbar-filters">
              <el-input
                v-model="keywordWhitelist"
                placeholder="按 deviceKey / modelId / 备注过滤"
                clearable
                class="filter-input"
              />
            </div>
            <div class="toolbar-actions">
              <el-button :loading="loadingWhitelist" @click="loadWhitelist">刷新</el-button>
              <el-button type="primary" :disabled="!canWrite" @click="openAdd">＋ 新增白名单</el-button>
            </div>
          </div>
          <p class="form-tip">
            仅白名单内的设备 key 允许绑定/上报；同一 deviceKey 再次提交即为更新，启用/停用请在「编辑」里切换。
          </p>
          <p v-if="!canWrite" class="form-tip warn">当前账号缺少 whitelist:write 权限，只能查看白名单。</p>
          <el-alert
            v-if="errorWhitelist"
            :title="`白名单加载失败：${errorWhitelist}`"
            type="error"
            :closable="false"
            show-icon
            style="margin-bottom: 12px"
          />
          <el-table :data="filteredWhitelist" v-loading="loadingWhitelist" row-key="deviceKey">
            <el-table-column label="设备 key" min-width="200">
              <template #default="{ row }">
                <span class="mono">{{ row.deviceKey }}</span>
              </template>
            </el-table-column>
            <el-table-column label="设备型号" min-width="200">
              <template #default="{ row }">
                <div class="cell-main">{{ modelLabel(row.modelId) }}</div>
                <div class="cell-sub mono">{{ row.modelId }}</div>
              </template>
            </el-table-column>
            <el-table-column label="备注" min-width="180">
              <template #default="{ row }">{{ row.note || '—' }}</template>
            </el-table-column>
            <el-table-column label="启用" width="90" align="center">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small" :effect="row.enabled ? 'dark' : 'plain'">
                  {{ row.enabled ? '已启用' : '未启用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="130" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" :disabled="!canWrite" @click="openEdit(row)">编辑</el-button>
                <el-button link type="danger" :disabled="!canWrite" @click="remove(row)">删除</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty description="暂无白名单设备" :image-size="90" />
            </template>
          </el-table>
        </el-tab-pane>

        <el-tab-pane name="mapping">
          <template #label>
            <span>设备映射（{{ mappings.length }}）</span>
          </template>

          <div class="table-toolbar">
            <div class="toolbar-filters">
              <el-input
                v-model="keywordMapping"
                placeholder="按 deviceKey / modelId / 备注过滤"
                clearable
                class="filter-input"
              />
            </div>
            <div class="toolbar-actions">
              <el-button :loading="loadingMapping" @click="loadMappings">刷新</el-button>
              <el-button type="primary" :disabled="!canWrite" @click="openAdd">＋ 新增映射</el-button>
            </div>
          </div>
          <p class="form-tip">设备映射把具体设备 key 直接指向某个设备型号，优先级高于白名单的自动识别；同一 deviceKey 再次提交即为更新。</p>
          <p v-if="!canWrite" class="form-tip warn">当前账号缺少 device:write 权限，只能查看设备映射。</p>
          <el-alert
            v-if="errorMapping"
            :title="`设备映射加载失败：${errorMapping}`"
            type="error"
            :closable="false"
            show-icon
            style="margin-bottom: 12px"
          />
          <el-table :data="filteredMappings" v-loading="loadingMapping" row-key="deviceKey">
            <el-table-column label="设备 key" min-width="200">
              <template #default="{ row }">
                <span class="mono">{{ row.deviceKey }}</span>
              </template>
            </el-table-column>
            <el-table-column label="映射型号" min-width="220">
              <template #default="{ row }">
                <div class="cell-main">{{ modelLabel(row.modelId) }}</div>
                <div class="cell-sub mono">{{ row.modelId }}</div>
              </template>
            </el-table-column>
            <el-table-column label="备注" min-width="220">
              <template #default="{ row }">{{ row.note || '—' }}</template>
            </el-table-column>
            <el-table-column label="操作" width="130" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" :disabled="!canWrite" @click="openEdit(row)">编辑</el-button>
                <el-button link type="danger" :disabled="!canWrite" @click="remove(row)">删除</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty description="暂无设备映射" :image-size="90" />
            </template>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="`${editing ? '编辑' : '新增'}${modeText()}`"
      width="560px"
      append-to-body
      :close-on-click-modal="false"
    >
      <el-alert
        v-if="editing"
        :title="`同一设备 key 再次提交即为更新（${modeText()}接口为 upsert）；设备 key 是唯一标识，如需更换请先删除再新增。`"
        type="info"
        :closable="false"
        show-icon
        style="margin-bottom: 14px"
      />
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item label="设备 key" prop="deviceKey">
          <el-input
            v-model="form.deviceKey"
            :disabled="editing"
            placeholder="如 ZWK-BAND-0001 / BLE MAC / SN"
            :maxlength="128"
            clearable
          />
          <p class="form-tip">设备侧上报的唯一标识，需与固件/APP 上报值完全一致。</p>
        </el-form-item>
        <el-form-item label="设备型号 modelId" prop="modelId">
          <el-select
            v-model="form.modelId"
            filterable
            allow-create
            default-first-option
            placeholder="选择型号，或直接输入 modelId 后回车"
            style="width: 100%"
          >
            <el-option
              v-for="model in models"
              :key="model.modelId"
              :label="model.name ? `${model.name}（${model.modelId}）` : model.modelId"
              :value="model.modelId"
            />
          </el-select>
          <p v-if="modelError" class="form-tip warn">{{ modelError }}（仍可手动输入 modelId）</p>
        </el-form-item>
        <el-form-item v-if="isWhitelist" label="启用状态">
          <el-switch v-model="form.enabled" active-text="启用" inactive-text="停用" inline-prompt />
          <p class="form-tip">停用后该设备 key 不再被白名单放行，但记录仍保留。</p>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.note" type="textarea" :rows="2" placeholder="用途说明（可选）" :maxlength="200" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">
          {{ editing ? '确认修改' : '确认新增' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.toolbar-filters,
.toolbar-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.filter-input {
  width: 280px;
}
.mono {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  font-size: 12px;
}
.cell-main {
  font-size: 13px;
  color: #1f2d3d;
  line-height: 1.5;
}
.cell-sub {
  color: #8593a8;
  line-height: 1.5;
}
.warn {
  color: #e6a23c;
}
</style>
