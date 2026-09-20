<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../../api/http'
import { adminApi, apiErrorText, type RoleItem } from '../../api/admin'
import { PERM } from '../../constants/permissions'
import { useAdminPermissionsStore } from '../../stores/adminPermissions'

interface AdminUser {
  id: string
  phone: string
  nickname: string
  role: string
  status: number
  createdAt: string
  lastLoginAt?: string | null
}

interface UserPage {
  items: AdminUser[]
  total: number
}

const perms = useAdminPermissionsStore()

const rows = ref<AdminUser[]>([])
const total = ref(0)
const loading = ref(false)

const keyword = ref('')
const statusFilter = ref(-1)
const page = ref(1)
const pageSize = ref(20)

/** 每行用户的多角色（GET /admin/users/:id/roles），按页并发拉取。 */
const rolesMap = ref<Record<string, string[]>>({})
const rolesUnavailable = ref(false)
let rolesWarnedOnce = false

const assignVisible = ref(false)
const assignUser = ref<AdminUser | null>(null)
const allRoles = ref<RoleItem[]>([])
const selectedRoles = ref<string[]>([])
const assignPermissions = ref<string[]>([])
const assignLoading = ref(false)
const assignSaving = ref(false)
const assignError = ref('')

const canAssign = computed(() =>
  perms.allows([PERM.userWrite, PERM.userRoleWrite, 'user:manage', 'role:write']),
)

onMounted(() => {
  void load()
  void loadRoleCatalog()
})

/** 角色目录：用于「角色」列的悬浮名称与分配对话框的下拉选项；失败仅记录，不阻塞列表。 */
async function loadRoleCatalog(): Promise<void> {
  try {
    const data = await adminApi.listRoles()
    allRoles.value = data?.items ?? []
  } catch {
    allRoles.value = []
  }
}

async function load(): Promise<void> {
  loading.value = true
  try {
    const params = new URLSearchParams()
    params.set('keyword', keyword.value.trim())
    params.set('status', String(statusFilter.value))
    params.set('page', String(page.value))
    params.set('pageSize', String(pageSize.value))
    const data = await api.get<UserPage>(`/admin/users?${params.toString()}`)
    rows.value = data.items
    total.value = data.total
    rolesMap.value = {}
    void loadRolesForRows()
  } catch (e) {
    // 401/403（普通账号误入等）会带 message，统一用其提示
    ElMessage.error(e instanceof Error ? e.message : '加载失败，请稍后重试')
  } finally {
    loading.value = false
  }
}

/** 逐行拉取角色；接口未就绪时只提示一次，且不影响列表展示。 */
async function loadRolesForRows(): Promise<void> {
  const list = rows.value
  if (list.length === 0) return
  const results = await Promise.allSettled(list.map((row) => adminApi.userRoles(row.id)))
  const map: Record<string, string[]> = {}
  let failed = 0
  results.forEach((result, index) => {
    const id = list[index].id
    if (result.status === 'fulfilled') map[id] = result.value?.roles ?? []
    else failed += 1
  })
  rolesMap.value = map
  rolesUnavailable.value = failed > 0 && failed === results.length
  if (rolesUnavailable.value && !rolesWarnedOnce) {
    rolesWarnedOnce = true
    ElMessage.warning('角色接口暂不可用（GET /admin/users/:id/roles），「角色」列显示为 —')
  }
}

function search(): void {
  page.value = 1
  void load()
}

function reset(): void {
  keyword.value = ''
  statusFilter.value = -1
  page.value = 1
  void load()
}

function onPageChange(p: number): void {
  page.value = p
  void load()
}

function onSizeChange(size: number): void {
  pageSize.value = size
  page.value = 1
  void load()
}

async function toggleStatus(row: AdminUser): Promise<void> {
  const next = row.status === 1 ? 0 : 1
  if (next === 0) {
    try {
      await ElMessageBox.confirm('禁用后该用户将无法登录，确认？', '禁用确认', {
        type: 'warning',
        confirmButtonText: '禁用',
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  try {
    await api.patch(`/admin/users/${row.id}/status`, { status: next })
    row.status = next
    ElMessage.success(next === 0 ? '已禁用该用户' : '已启用该用户')
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '操作失败，请稍后重试')
  }
}

/* ---------------- 角色分配 ---------------- */

async function openAssign(row: AdminUser): Promise<void> {
  assignUser.value = row
  assignVisible.value = true
  assignError.value = ''
  assignPermissions.value = []
  selectedRoles.value = [...(rolesMap.value[row.id] ?? [])]
  assignLoading.value = true
  try {
    if (allRoles.value.length === 0) {
      const data = await adminApi.listRoles()
      allRoles.value = data?.items ?? []
    }
    const detail = await adminApi.userRoles(row.id)
    selectedRoles.value = [...(detail?.roles ?? [])]
    assignPermissions.value = detail?.permissions ?? []
    rolesMap.value[row.id] = [...selectedRoles.value]
  } catch (e) {
    assignError.value = apiErrorText(e, '角色数据加载失败')
  } finally {
    assignLoading.value = false
  }
}

async function saveAssign(): Promise<void> {
  const user = assignUser.value
  if (!user) return
  assignSaving.value = true
  try {
    const data = await adminApi.setUserRoles(user.id, [...selectedRoles.value])
    const next = data?.roles ?? selectedRoles.value
    rolesMap.value[user.id] = [...next]
    selectedRoles.value = [...next]
    ElMessage.success(`已更新「${user.nickname || user.phone}」的角色`)
    assignVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(apiErrorText(e, '角色分配失败'))
  } finally {
    assignSaving.value = false
  }
}

function roleName(code: string): string {
  const hit = allRoles.value.find((role) => role.code === code)
  return hit?.name ? `${hit.name}（${code}）` : code
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

function accountTypeInfo(role: string): { type: 'warning' | 'info'; text: string } {
  return role === 'admin' ? { type: 'warning', text: '管理员' } : { type: 'info', text: '用户' }
}

function statusInfo(status: number): { type: 'success' | 'danger'; text: string } {
  return status === 1 ? { type: 'success', text: '正常' } : { type: 'danger', text: '禁用' }
}
</script>

<template>
  <div>
    <el-card class="page-card" shadow="never">
      <div class="table-toolbar">
        <div class="toolbar-filters">
          <el-input
            v-model="keyword"
            placeholder="手机号 / 昵称（模糊）"
            clearable
            class="filter-input"
            @keyup.enter="search"
          />
          <el-select v-model="statusFilter" class="status-select">
            <el-option label="全部状态" :value="-1" />
            <el-option label="正常" :value="1" />
            <el-option label="禁用" :value="0" />
          </el-select>
        </div>
        <div class="toolbar-actions">
          <el-button type="primary" @click="search">查询</el-button>
          <el-button @click="reset">重置</el-button>
        </div>
      </div>

      <p class="form-tip phone-tip">手机号为完整明文展示，仅供管理员在本后台查看（用户端始终为脱敏号码）。</p>
      <p v-if="rolesUnavailable" class="form-tip warn-tip">
        用户角色接口暂不可用，「角色」列与分配功能可能无法使用（后端 /admin/users/:id/roles 未就绪）。
      </p>
      <p v-if="!canAssign" class="form-tip warn-tip">当前账号缺少 user:write 权限，「分配角色」不可用。</p>

      <el-table :data="rows" v-loading="loading" row-key="id">
        <el-table-column label="昵称" min-width="140">
          <template #default="{ row }">{{ row.nickname || '—' }}</template>
        </el-table-column>
        <el-table-column label="手机号" min-width="150">
          <template #default="{ row }">
            <span class="mono">{{ row.phone || '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="账号类型" width="100">
          <template #default="{ row }">
            <el-tag :type="accountTypeInfo(row.role).type" size="small">{{ accountTypeInfo(row.role).text }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="角色" min-width="180">
          <template #default="{ row }">
            <div v-if="(rolesMap[row.id] ?? []).length" class="tags-wrap">
              <el-tag
                v-for="code in rolesMap[row.id]"
                :key="code"
                size="small"
                effect="plain"
                class="role-tag"
                :title="roleName(code)"
                @click="openAssign(row)"
              >
                {{ code }}
              </el-tag>
            </div>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="statusInfo(row.status).type" size="small">{{ statusInfo(row.status).text }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="注册时间" min-width="160">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="最近登录" min-width="160">
          <template #default="{ row }">{{ formatDateTime(row.lastLoginAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" :disabled="!canAssign" @click="openAssign(row)">分配角色</el-button>
            <el-button v-if="row.status === 1" link type="danger" @click="toggleStatus(row)">禁用</el-button>
            <el-button v-else link type="success" @click="toggleStatus(row)">启用</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无用户" :image-size="90" />
        </template>
      </el-table>

      <div v-if="total > 0" class="pager">
        <el-pagination
          background
          layout="total, sizes, prev, pager, next, jumper"
          :total="total"
          :current-page="page"
          :page-size="pageSize"
          :page-sizes="[10, 20, 50]"
          @current-change="onPageChange"
          @size-change="onSizeChange"
        />
      </div>
    </el-card>

    <el-dialog
      v-model="assignVisible"
      :title="`分配角色 · ${assignUser?.nickname || assignUser?.phone || ''}`"
      width="560px"
      append-to-body
      :close-on-click-modal="false"
    >
      <el-alert
        v-if="assignError"
        :title="assignError"
        type="error"
        :closable="false"
        show-icon
        style="margin-bottom: 14px"
      />
      <div v-loading="assignLoading">
        <el-form label-position="top">
          <el-form-item label="角色（可多选）">
            <el-select
              v-model="selectedRoles"
              multiple
              filterable
              placeholder="选择角色，可多选"
              style="width: 100%"
            >
              <el-option
                v-for="role in allRoles"
                :key="role.code"
                :label="role.name ? `${role.name}（${role.code}）` : role.code"
                :value="role.code"
              >
                <span>{{ role.name || role.code }}</span>
                <span class="option-code mono">{{ role.code }} · {{ (role.permissions ?? []).length }} 项权限</span>
              </el-option>
            </el-select>
            <p class="form-tip">
              角色决定该用户在后台可见的菜单与可执行的操作；未分配任何角色时用户只能看到仪表盘。保存后立即生效。
            </p>
          </el-form-item>

          <el-form-item label="当前已选角色的权限预览（保存前为上次生效值）">
            <div v-if="assignPermissions.length" class="tags-wrap">
              <el-tag v-for="code in assignPermissions" :key="code" size="small" effect="plain" class="perm-tag">
                {{ code }}
              </el-tag>
            </div>
            <span v-else class="muted">无权限记录（或角色接口未返回 permissions）</span>
          </el-form-item>

          <el-form-item v-if="assignUser" label="账号信息">
            <span class="form-tip">
              ID：<span class="mono">{{ assignUser.id }}</span> · 手机号：<span class="mono">{{ assignUser.phone }}</span>
              · 账号类型：{{ accountTypeInfo(assignUser.role).text }}
            </span>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <el-button @click="assignVisible = false">取消</el-button>
        <el-button type="primary" :loading="assignSaving" :disabled="assignLoading" @click="saveAssign">
          保存角色
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
  width: 220px;
}
.status-select {
  width: 130px;
}
.phone-tip {
  margin: -4px 0 12px;
}
.warn-tip {
  margin: -6px 0 12px;
  color: #e6a23c;
}
.mono {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  font-size: 12px;
}
.muted {
  color: #b6c0cf;
}
.tags-wrap {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.role-tag {
  cursor: pointer;
}
.perm-tag {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
}
.option-code {
  float: right;
  color: #8593a8;
}
</style>
