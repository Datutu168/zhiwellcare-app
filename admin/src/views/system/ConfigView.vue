<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { adminApi, apiErrorText, type ConfigItem } from '../../api/admin'
import { PERM } from '../../constants/permissions'
import { useAdminPermissionsStore } from '../../stores/adminPermissions'

interface ConfigForm {
  key: string
  description: string
  text: string
}

const items = ref<ConfigItem[]>([])
const loading = ref(false)
const listError = ref('')
const keyword = ref('')

const perms = useAdminPermissionsStore()
/** 没有 config:write 时只读展示（后端同样会 403）。 */
const canWrite = computed(() => perms.allows([PERM.configWrite]))

const dialogVisible = ref(false)
const editing = ref(false)
const saving = ref(false)
const jsonError = ref('')
const formRef = ref<FormInstance>()
const form = reactive<ConfigForm>({ key: '', description: '', text: '' })

const rules: FormRules = {
  key: [
    { required: true, message: '请输入配置 key', trigger: 'blur' },
    { pattern: /^[a-zA-Z][a-zA-Z0-9._:-]{1,127}$/, message: '以字母开头，仅含字母/数字/._:- ，共 2-128 位', trigger: 'blur' },
  ],
}

onMounted(() => {
  void load()
})

const filtered = computed<ConfigItem[]>(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return items.value
  return items.value.filter(
    (item) =>
      item.key.toLowerCase().includes(kw) || (item.description ?? '').toLowerCase().includes(kw),
  )
})

function emptyForm(): ConfigForm {
  return { key: '', description: '', text: '{}' }
}

async function load(): Promise<void> {
  loading.value = true
  try {
    const data = await adminApi.listConfigs()
    items.value = data?.items ?? []
    listError.value = ''
  } catch (e) {
    items.value = []
    listError.value = apiErrorText(e, '配置列表加载失败')
  } finally {
    loading.value = false
  }
}

/** value 是任意 JSON（JSONB），编辑时统一按 2 空格缩进序列化。 */
function valueText(value: unknown): string {
  if (value === undefined) return ''
  if (typeof value === 'string') return JSON.stringify(value)
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

/** 列表里的一行摘要（单行、超长截断）。 */
function valueSummary(value: unknown): string {
  const text = valueText(value).replace(/\s+/g, ' ')
  return text.length > 80 ? `${text.slice(0, 80)}…` : text || '—'
}

function valueType(value: unknown): string {
  if (value === null) return 'null'
  if (Array.isArray(value)) return 'array'
  return typeof value
}

async function openEdit(row: ConfigItem): Promise<void> {
  editing.value = true
  form.key = row.key
  form.description = row.description ?? ''
  form.text = valueText(row.value)
  jsonError.value = ''
  dialogVisible.value = true
  await nextTick()
  formRef.value?.clearValidate()
}

async function openAdd(): Promise<void> {
  editing.value = false
  Object.assign(form, emptyForm())
  jsonError.value = ''
  dialogVisible.value = true
  await nextTick()
  formRef.value?.clearValidate()
}

function formatJson(): void {
  const parsed = parseJson()
  if (parsed.ok) form.text = JSON.stringify(parsed.value, null, 2)
}

function parseJson(): { ok: true; value: unknown } | { ok: false } {
  const raw = form.text.trim()
  if (!raw) {
    jsonError.value = '配置值不能为空，请输入合法 JSON，例如 {} 或 []'
    return { ok: false }
  }
  try {
    return { ok: true, value: JSON.parse(raw) as unknown }
  } catch (e) {
    jsonError.value = `JSON 解析失败：${e instanceof Error ? e.message : '格式非法'}`
    return { ok: false }
  }
}

function onTextChange(): void {
  if (jsonError.value) jsonError.value = ''
}

async function submit(): Promise<void> {
  if (!formRef.value) return
  const ok = await formRef.value.validate().catch(() => false)
  if (!ok) return
  const parsed = parseJson()
  if (!parsed.ok) return // 非法 JSON 不提交
  saving.value = true
  try {
    await adminApi.putConfig(form.key.trim(), {
      value: parsed.value,
      description: form.description.trim(),
    })
    ElMessage.success(editing.value ? '配置已更新' : '配置已保存')
    dialogVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(apiErrorText(e, '配置保存失败'))
  } finally {
    saving.value = false
  }
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}

function formatDateTime(value?: string | null): string {
  if (!value) return '—'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

async function copyKey(key: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(key)
    ElMessage.success('已复制 key')
  } catch {
    ElMessage.warning('当前浏览器不允许自动复制，请手动选择')
  }
}
</script>

<template>
  <div>
    <el-card class="page-card" shadow="never">
      <div class="table-toolbar">
        <div class="toolbar-filters">
          <el-input v-model="keyword" placeholder="按 key / 描述过滤" clearable class="filter-input" />
        </div>
        <div class="toolbar-actions">
          <el-button :loading="loading" @click="load">刷新</el-button>
          <el-button type="primary" :disabled="!canWrite" @click="openAdd">＋ 新增 / 覆盖配置</el-button>
        </div>
      </div>

      <p v-if="!canWrite" class="form-tip warn-tip">当前账号缺少 config:write 权限，只能查看配置，不能修改。</p>

      <p class="form-tip">
        配置值以 JSONB 存储，支持对象、数组、字符串、数字、布尔与 null；保存前会做 JSON 合法性校验，非法内容不会提交。
      </p>

      <el-alert
        v-if="listError"
        :title="`配置列表加载失败：${listError}`"
        type="error"
        :closable="false"
        show-icon
        style="margin-bottom: 12px"
      />

      <el-table :data="filtered" v-loading="loading" row-key="key">
        <el-table-column label="配置 key" min-width="220">
          <template #default="{ row }">
            <div class="key-cell">
              <span class="mono">{{ row.key }}</span>
              <el-button link type="primary" class="copy-btn" @click="copyKey(row.key)">复制</el-button>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="90" align="center">
          <template #default="{ row }">
            <el-tag size="small" type="info" effect="plain">{{ valueType(row.value) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="值摘要" min-width="300">
          <template #default="{ row }">
            <el-tooltip :content="valueSummary(row.value)" placement="top" :disabled="valueSummary(row.value).length < 80">
              <span class="mono value-summary">{{ valueSummary(row.value) }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="描述" min-width="180">
          <template #default="{ row }">{{ row.description || '—' }}</template>
        </el-table-column>
        <el-table-column label="更新时间" width="160">
          <template #default="{ row }">{{ formatDateTime(row.updatedAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="90" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" :disabled="!canWrite" @click="openEdit(row)">编辑</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无配置项" :image-size="90" />
        </template>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="editing ? `编辑配置 · ${form.key}` : '新增 / 覆盖配置'"
      width="720px"
      top="8vh"
      append-to-body
      :close-on-click-modal="false"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item label="配置 key" prop="key">
          <el-input
            v-model="form.key"
            :disabled="editing"
            placeholder="如 feature.game_upload_enabled"
            :maxlength="128"
            clearable
          />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" placeholder="该配置的用途（可选）" :maxlength="200" clearable />
        </el-form-item>
        <el-form-item label="值（JSON）">
          <el-input
            v-model="form.text"
            type="textarea"
            :rows="10"
            spellcheck="false"
            class="json-input"
            placeholder='例如：{"enabled":true,"level":3} 或 ["a","b"] 或 12'
            @input="onTextChange"
          />
          <div class="json-actions">
            <el-button size="small" @click="formatJson">格式化</el-button>
            <span class="form-tip">保存前会 JSON.parse 校验；解析失败将提示错误并阻止提交。</span>
          </div>
          <el-alert
            v-if="jsonError"
            :title="jsonError"
            type="error"
            :closable="false"
            show-icon
            style="margin-top: 8px"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">
          {{ editing ? '保存修改' : '确认保存' }}
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
  width: 260px;
}
.key-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}
.copy-btn {
  padding: 0;
}
.mono {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  font-size: 12px;
}
.value-summary {
  color: #1f2d3d;
  word-break: break-all;
}
.warn-tip {
  margin: -6px 0 12px;
  color: #e6a23c;
}
.json-input :deep(textarea) {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  font-size: 12px;
  line-height: 1.6;
}
.json-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 6px;
  flex-wrap: wrap;
}
</style>
