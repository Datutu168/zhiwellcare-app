<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { UploadFile } from 'element-plus'
import {
  adminApi,
  apiErrorText,
  type AssetItem,
  type AssetKind,
  type RegisterAssetPayload,
  type UploadTicket,
} from '../../api/admin'
import { PERM } from '../../constants/permissions'
import { useAdminPermissionsStore } from '../../stores/adminPermissions'

interface KindMeta {
  value: AssetKind
  label: string
  refLabel: string
  refPlaceholder: string
  hint: string
}

const KINDS: KindMeta[] = [
  {
    value: 'web',
    label: 'Web 包',
    refLabel: 'refId（Web 包标识）',
    refPlaceholder: '如 web-console-1.2.0',
    hint: '用户端 / 后台前端构建产物（zip）。refId 建议用「包名-版本」。',
  },
  {
    value: 'game',
    label: '游戏资源',
    refLabel: 'refId（游戏 gameId）',
    refPlaceholder: '如 wobble-wrist-band',
    hint: '游戏包/资源文件登记到对应 gameId 下，用户端按版本拉取。',
  },
  {
    value: 'firmware',
    label: '固件',
    refLabel: 'refId（设备型号 modelId）',
    refPlaceholder: '如 wobble-wrist-band',
    hint: '固件二进制登记到对应设备型号，APP 侧按版本做 OTA 提示。',
  },
]

/** el-tabs 的 v-model 只接受 string|number，这里保持宽类型，对外用 activeKindValue 收窄。 */
const activeKind = ref<string>('web')
const refIdFilter = ref('')

const perms = useAdminPermissionsStore()
/** 没有 asset:write 时只读展示（后端同样会 403）。 */
const canWrite = computed(() => perms.allows([PERM.assetWrite]))

const rows = ref<AssetItem[]>([])
const listLoading = ref(false)
const listError = ref('')
const busyId = ref('')

const rawFile = ref<File | null>(null)
const refId = ref('')
const version = ref('')
const notes = ref('')
const contentType = ref('')
const uploading = ref(false)
const step = ref(0)

const STEP_PERCENT = [0, 25, 55, 80, 100]
const STEP_TEXT = ['', '① 计算 SHA-256 校验值…', '② 获取预签名地址并直传对象存储…', '③ 登记资产记录…', '']

const activeKindValue = computed<AssetKind>(() =>
  activeKind.value === 'game' || activeKind.value === 'firmware' ? activeKind.value : 'web',
)
const kindMeta = computed<KindMeta>(() => KINDS.find((item) => item.value === activeKindValue.value) ?? KINDS[0])
const stepPercent = computed(() => STEP_PERCENT[step.value] ?? 0)
const stepText = computed(() => STEP_TEXT[step.value] ?? '')

onMounted(() => {
  void load()
})

watch(activeKind, () => {
  refIdFilter.value = ''
  void load()
})

async function load(): Promise<void> {
  listLoading.value = true
  try {
    const data = await adminApi.listAssets(activeKindValue.value, refIdFilter.value)
    rows.value = data?.items ?? []
    listError.value = ''
  } catch (e) {
    rows.value = []
    listError.value = apiErrorText(e, '资产列表加载失败')
  } finally {
    listLoading.value = false
  }
}

function guessContentType(name: string): string {
  const ext = name.slice(name.lastIndexOf('.') + 1).toLowerCase()
  const map: Record<string, string> = {
    zip: 'application/zip',
    apk: 'application/vnd.android.package-archive',
    gz: 'application/gzip',
    tar: 'application/x-tar',
    bin: 'application/octet-stream',
    json: 'application/json',
    js: 'text/javascript',
    css: 'text/css',
    html: 'text/html',
    png: 'image/png',
    jpg: 'image/jpeg',
    jpeg: 'image/jpeg',
    webp: 'image/webp',
    wasm: 'application/wasm',
    mp3: 'audio/mpeg',
    mp4: 'video/mp4',
  }
  return map[ext] ?? 'application/octet-stream'
}

function applyFile(file: File): void {
  rawFile.value = file
  if (!contentType.value.trim() || contentType.value !== file.type) {
    contentType.value = file.type || guessContentType(file.name)
  }
  if (!version.value.trim()) {
    const match = /(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)/.exec(file.name)
    if (match) version.value = match[1]
  }
}

function onFileChange(file: UploadFile): void {
  const raw = file.raw
  if (!raw) return
  applyFile(raw)
}

function clearFile(): void {
  rawFile.value = null
  contentType.value = ''
}

function resetForm(): void {
  rawFile.value = null
  refId.value = ''
  version.value = ''
  notes.value = ''
  contentType.value = ''
}

/** 浏览器端 SHA-256（小写 hex）；不可用或文件过大时返回空串，由后端自行补算。 */
async function sha256Hex(file: File): Promise<string> {
  const subtle = globalThis.crypto?.subtle
  if (!subtle) return ''
  if (file.size > 512 * 1024 * 1024) return ''
  try {
    const digest = await subtle.digest('SHA-256', await file.arrayBuffer())
    return [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, '0')).join('')
  } catch {
    return ''
  }
}

async function putFile(ticket: UploadTicket, file: File, type: string): Promise<void> {
  const url = ticket.uploadUrl ?? ''
  if (!/^(https?:)?\/\//.test(url) && !url.startsWith('/')) {
    throw new Error('后端返回的预签名上传地址非法，无法直传')
  }
  const method = (ticket.method || 'PUT').toUpperCase()
  const headers: Record<string, string> = {}
  if (type) headers['Content-Type'] = type

  let response: Response
  try {
    response = await fetch(url, { method, body: file, headers })
  } catch {
    throw new Error('直传失败：无法连接对象存储（常见原因：对象存储 CORS 未放行本后台域名，或预签名地址已过期）')
  }
  if (!response.ok) {
    let detail = ''
    try {
      detail = (await response.text()).slice(0, 200)
    } catch {
      detail = ''
    }
    const hint =
      response.status === 403
        ? '（预签名地址过期、签名与 Content-Type 不匹配，或对象存储未放行 CORS）'
        : response.status === 404
          ? '（bucket / endpoint 配置可能有误）'
          : ''
    throw new Error(`文件直传失败：HTTP ${response.status}${hint}${detail ? ` · ${detail}` : ''}`)
  }
}

async function startUpload(): Promise<void> {
  const file = rawFile.value
  if (!file) {
    ElMessage.warning('请先选择要上传的文件')
    return
  }
  const refIdValue = refId.value.trim()
  const versionValue = version.value.trim()
  if (!refIdValue) {
    ElMessage.warning(`请填写 ${kindMeta.value.refLabel}`)
    return
  }
  if (!versionValue) {
    ElMessage.warning('请填写版本号，如 1.2.0')
    return
  }
  const type = (contentType.value.trim() || file.type || guessContentType(file.name)).trim()

  uploading.value = true
  try {
    step.value = 1
    const sha256 = await sha256Hex(file)
    if (!sha256) ElMessage.info('未能计算 SHA-256（浏览器不支持 crypto.subtle 或文件过大），将以空值登记')

    step.value = 2
    const ticket = await adminApi.createUploadUrl({
      kind: activeKindValue.value,
      refId: refIdValue,
      version: versionValue,
      filename: file.name,
      contentType: type,
    })
    if (!ticket?.uploadUrl || !ticket.key) throw new Error('后端未返回 key / uploadUrl，无法直传')
    await putFile(ticket, file, type)

    step.value = 3
    const payload: RegisterAssetPayload = {
      kind: activeKindValue.value,
      refId: refIdValue,
      version: versionValue,
      filename: file.name,
      objectKey: ticket.key,
      size: file.size,
      sha256,
      contentType: type,
      notes: notes.value.trim(),
    }
    await adminApi.registerAsset(payload)

    step.value = 4
    ElMessage.success(`「${file.name}」已上传并登记（${formatBytes(file.size)}）`)
    resetForm()
    await load()
  } catch (e) {
    ElMessage.error(apiErrorText(e, '上传失败，请稍后重试'))
  } finally {
    uploading.value = false
    step.value = 0
  }
}

async function changeStatus(row: AssetItem, status: 'published' | 'offline'): Promise<void> {
  if (status === 'offline') {
    try {
      await ElMessageBox.confirm(`下架后用户端将不再拉取「${row.filename}」（${row.version}）。确认下架？`, '下架确认', {
        type: 'warning',
        confirmButtonText: '下架',
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  busyId.value = String(row.id)
  try {
    await adminApi.setAssetStatus(row.id, status)
    ElMessage.success(status === 'published' ? '已发布' : '已下架')
    await load()
  } catch (e) {
    ElMessage.error(apiErrorText(e, '状态更新失败'))
  } finally {
    busyId.value = ''
  }
}

async function remove(row: AssetItem): Promise<void> {
  try {
    await ElMessageBox.confirm(
      `删除后记录不可恢复（对象存储文件是否同时删除由后端决定）。确认删除「${row.filename}」（${row.version}）？`,
      '删除确认',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  busyId.value = String(row.id)
  try {
    await adminApi.deleteAsset(row.id)
    ElMessage.success('已删除')
    await load()
  } catch (e) {
    ElMessage.error(apiErrorText(e, '删除失败'))
  } finally {
    busyId.value = ''
  }
}

function formatBytes(size: number): string {
  if (!Number.isFinite(size) || size <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = size
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index += 1
  }
  return `${value >= 100 || index === 0 ? Math.round(value) : value.toFixed(1)} ${units[index]}`
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

function statusInfo(status: string): { type: 'success' | 'info' | 'warning'; text: string } {
  if (status === 'published') return { type: 'success', text: '已发布' }
  if (status === 'offline') return { type: 'info', text: '已下架' }
  if (status === 'draft') return { type: 'warning', text: '草稿' }
  return { type: 'warning', text: status || '未知' }
}

function shortHash(value: string): string {
  if (!value) return '—'
  return value.length > 12 ? `${value.slice(0, 12)}…` : value
}
</script>

<template>
  <div>
    <el-card class="page-card" shadow="never">
      <el-tabs v-model="activeKind">
        <el-tab-pane v-for="kind in KINDS" :key="kind.value" :name="kind.value" :label="kind.label" />
      </el-tabs>

      <p class="form-tip">{{ kindMeta.hint }}</p>
      <p v-if="!canWrite" class="form-tip warn-tip">
        当前账号缺少 asset:write 权限，只能查看资产列表，不能上传/发布/删除。
      </p>

      <el-alert
        v-if="uploading"
        :title="`正在上传，请勿关闭页面：${stepText}`"
        type="info"
        :closable="false"
        show-icon
        style="margin-bottom: 10px"
      />
      <el-progress
        v-if="uploading"
        :percentage="stepPercent"
        :stroke-width="10"
        striped
        striped-flow
        style="margin-bottom: 14px"
      />

      <el-form label-position="top" class="upload-form">
        <el-row :gutter="16">
          <el-col :xs="24" :md="8">
            <el-form-item :label="kindMeta.refLabel">
              <el-input v-model="refId" :placeholder="kindMeta.refPlaceholder" :maxlength="128" clearable />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="6">
            <el-form-item label="版本号">
              <el-input v-model="version" placeholder="如 1.2.0" :maxlength="40" clearable />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="10">
            <el-form-item label="备注">
              <el-input v-model="notes" placeholder="变更说明（可选）" :maxlength="200" clearable />
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="文件">
          <div class="file-row">
            <el-upload
              :auto-upload="false"
              :show-file-list="false"
              :on-change="onFileChange"
              class="upload-inline"
            >
              <el-button :disabled="uploading">选择本地文件</el-button>
            </el-upload>
            <span v-if="rawFile" class="file-name mono">
              {{ rawFile.name }} · {{ formatBytes(rawFile.size) }}
            </span>
            <span v-else class="form-tip">未选择文件</span>
            <el-button v-if="rawFile" link type="danger" :disabled="uploading" @click="clearFile">清除</el-button>
          </div>
          <p class="form-tip">
            流程：获取预签名地址 → 浏览器直传对象存储（不经过业务服务器）→ 登记资产。SHA-256 由浏览器计算（小写 hex），无法计算时留空。
          </p>
        </el-form-item>

        <el-form-item label="Content-Type">
          <el-input v-model="contentType" placeholder="选择文件后自动填充，可手动修改" :maxlength="120" clearable />
        </el-form-item>

        <div class="upload-actions">
          <el-button
            type="primary"
            :loading="uploading"
            :disabled="!rawFile || !canWrite"
            @click="startUpload"
          >
            上传并登记
          </el-button>
          <el-button :disabled="uploading" @click="resetForm">重置表单</el-button>
        </div>
      </el-form>
    </el-card>

    <el-card class="page-card" shadow="never">
      <div class="table-toolbar">
        <div class="toolbar-filters">
          <el-input
            v-model="refIdFilter"
            :placeholder="`按 refId 过滤（如 ${kindMeta.refPlaceholder}）`"
            clearable
            class="filter-input"
            @keyup.enter="load"
          />
        </div>
        <div class="toolbar-actions">
          <el-button :loading="listLoading" @click="load">查询</el-button>
          <el-button @click="refIdFilter = ''; load()">重置</el-button>
        </div>
      </div>

      <el-alert
        v-if="listError"
        :title="`资产列表加载失败：${listError}`"
        type="error"
        :closable="false"
        show-icon
        style="margin-bottom: 12px"
      />

      <el-table :data="rows" v-loading="listLoading" row-key="id">
        <el-table-column label="文件名" min-width="220">
          <template #default="{ row }">
            <div class="cell-main">{{ row.filename }}</div>
            <div class="cell-sub mono">{{ row.objectKey }}</div>
          </template>
        </el-table-column>
        <el-table-column label="refId" min-width="160">
          <template #default="{ row }">
            <span class="mono">{{ row.refId }}</span>
          </template>
        </el-table-column>
        <el-table-column label="版本" width="110">
          <template #default="{ row }">
            <span class="mono">{{ row.version }}</span>
          </template>
        </el-table-column>
        <el-table-column label="大小" width="100" align="right">
          <template #default="{ row }">{{ formatBytes(row.size) }}</template>
        </el-table-column>
        <el-table-column label="SHA-256" width="150">
          <template #default="{ row }">
            <el-tooltip :content="row.sha256 || '未计算'" placement="top" :disabled="!row.sha256">
              <span class="mono">{{ shortHash(row.sha256) }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="statusInfo(row.status).type" size="small" :effect="row.status === 'published' ? 'dark' : 'plain'">
              {{ statusInfo(row.status).text }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="登记时间" width="150">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="发布时间" width="150">
          <template #default="{ row }">{{ formatDateTime(row.publishedAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="row.status !== 'published'"
              link
              type="success"
              :disabled="!canWrite"
              :loading="busyId === String(row.id)"
              @click="changeStatus(row, 'published')"
            >
              发布
            </el-button>
            <el-button
              v-else
              link
              type="warning"
              :disabled="!canWrite"
              :loading="busyId === String(row.id)"
              @click="changeStatus(row, 'offline')"
            >
              下架
            </el-button>
            <el-button link type="danger" :disabled="!canWrite" :loading="busyId === String(row.id)" @click="remove(row)">
              删除
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="`暂无${kindMeta.label}资产，选择文件后点击「上传并登记」`" :image-size="90" />
        </template>
      </el-table>
    </el-card>
  </div>
</template>

<style scoped>
.upload-form {
  margin-top: 4px;
}
.file-row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.upload-inline {
  display: inline-block;
}
.file-name {
  color: #1f2d3d;
  word-break: break-all;
}
.upload-actions {
  display: flex;
  gap: 10px;
}
.warn-tip {
  margin: -6px 0 12px;
  color: #e6a23c;
}
.toolbar-filters,
.toolbar-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.filter-input {
  width: 300px;
}
.mono {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  font-size: 12px;
}
.cell-main {
  font-size: 13px;
  color: #1f2d3d;
  line-height: 1.5;
  word-break: break-all;
}
.cell-sub {
  color: #8593a8;
  line-height: 1.5;
  word-break: break-all;
}
</style>
