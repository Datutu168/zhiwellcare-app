<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { ApiError } from '../../api/http'
import { adminApi, apiErrorText, type PermissionItem, type RoleItem } from '../../api/admin'
import { PERM } from '../../constants/permissions'
import { useAdminPermissionsStore } from '../../stores/adminPermissions'

interface PermissionGroup {
  name: string
  items: PermissionItem[]
}

interface RoleForm {
  code: string
  name: string
  description: string
  permissions: string[]
}

const CODE_PATTERN = /^[a-z][a-z0-9_:.-]{1,63}$/

const perms = useAdminPermissionsStore()
/** 没有 role:write 时只读展示（后端同样会 403）。 */
const canWrite = computed(() => perms.allows([PERM.roleWrite]))

const roles = ref<RoleItem[]>([])
const catalog = ref<PermissionItem[]>([])
const loading = ref(false)
const catalogLoading = ref(false)
const listError = ref('')
const catalogError = ref('')
const busyCode = ref('')

const dialogVisible = ref(false)
const editing = ref(false)
const saving = ref(false)
const formRef = ref<FormInstance>()
const form = reactive<RoleForm>({ code: '', name: '', description: '', permissions: [] })

const rules: FormRules = {
  code: [
    { required: true, message: '请输入角色 code', trigger: 'blur' },
    { pattern: CODE_PATTERN, message: '以小写字母开头，仅含小写字母/数字/下划线/冒号/点/连字符，共 2-64 位', trigger: 'blur' },
  ],
  name: [{ required: true, message: '请输入角色名称', trigger: 'blur' }],
}

onMounted(() => {
  void load()
  void loadCatalog()
})

/** 权限目录按 group 分组；目录接口不可用时退化为「已有权限码」清单，保证页面可用。 */
const groups = computed<PermissionGroup[]>(() => {
  const map = new Map<string, PermissionItem[]>()
  for (const item of catalog.value) {
    const key = item.group || '其他'
    const bucket = map.get(key)
    if (bucket) bucket.push(item)
    else map.set(key, [item])
  }
  if (map.size === 0) {
    const codes = new Set<string>()
    roles.value.forEach((role) => (role.permissions ?? []).forEach((code) => codes.add(code)))
    form.permissions.forEach((code) => codes.add(code))
    if (codes.size > 0) {
      map.set(
        '已有权限码（权限目录接口未就绪，可手动补全）',
        [...codes].sort().map((code) => ({ code, name: code, group: '', description: '' })),
      )
    }
  }
  return [...map.entries()].map(([name, items]) => ({ name, items }))
})

const permissionNames = computed<Map<string, string>>(() => {
  const map = new Map<string, string>()
  catalog.value.forEach((item) => map.set(item.code, item.name || item.code))
  return map
})

const totalPermissions = computed(() => groups.value.reduce((sum, group) => sum + group.items.length, 0))

function permLabel(code: string): string {
  const name = permissionNames.value.get(code)
  return name && name !== code ? `${name}（${code}）` : code
}

async function load(): Promise<void> {
  loading.value = true
  try {
    const data = await adminApi.listRoles()
    roles.value = data?.items ?? []
    listError.value = ''
  } catch (e) {
    roles.value = []
    listError.value = apiErrorText(e, '角色列表加载失败')
  } finally {
    loading.value = false
  }
}

async function loadCatalog(): Promise<void> {
  catalogLoading.value = true
  try {
    const data = await adminApi.listPermissions()
    catalog.value = data?.items ?? []
    catalogError.value = ''
  } catch (e) {
    catalog.value = []
    catalogError.value = apiErrorText(e, '权限目录加载失败')
  } finally {
    catalogLoading.value = false
  }
}

function resetForm(): void {
  form.code = ''
  form.name = ''
  form.description = ''
  form.permissions = []
}

async function openAdd(): Promise<void> {
  editing.value = false
  resetForm()
  dialogVisible.value = true
  if (catalog.value.length === 0 && !catalogLoading.value) await loadCatalog()
  formRef.value?.clearValidate()
}

async function openEdit(row: RoleItem): Promise<void> {
  editing.value = true
  form.code = row.code
  form.name = row.name || ''
  form.description = row.description || ''
  form.permissions = [...(row.permissions ?? [])]
  dialogVisible.value = true
  if (catalog.value.length === 0 && !catalogLoading.value) await loadCatalog()
  formRef.value?.clearValidate()
}

/* ---------------- 权限勾选（按组全选 / 半选） ---------------- */

function isChecked(code: string): boolean {
  return form.permissions.includes(code)
}

function onToggle(code: string, value: unknown): void {
  const on = value === true || value === 1 || value === 'true'
  const index = form.permissions.indexOf(code)
  if (on && index < 0) form.permissions.push(code)
  else if (!on && index >= 0) form.permissions.splice(index, 1)
}

function groupState(group: PermissionGroup): { all: boolean; indeterminate: boolean; hit: number } {
  const total = group.items.length
  const hit = group.items.filter((item) => form.permissions.includes(item.code)).length
  return { all: total > 0 && hit === total, indeterminate: hit > 0 && hit < total, hit }
}

function toggleGroup(group: PermissionGroup): void {
  const state = groupState(group)
  const codes = group.items.map((item) => item.code)
  if (state.all) form.permissions = form.permissions.filter((code) => !codes.includes(code))
  else form.permissions = [...new Set([...form.permissions, ...codes])]
}

function selectAll(): void {
  form.permissions = [...new Set(groups.value.flatMap((group) => group.items.map((item) => item.code)))]
}

function clearAll(): void {
  form.permissions = []
}

/* ---------------- 保存 / 删除 ---------------- */

async function submit(): Promise<void> {
  if (!formRef.value) return
  const ok = await formRef.value.validate().catch(() => false)
  if (!ok) return
  if (form.permissions.length === 0) {
    try {
      await ElMessageBox.confirm('该角色未勾选任何权限，被分配该角色的用户将只能看到仪表盘。确认保存？', '权限为空', {
        type: 'warning',
        confirmButtonText: '继续保存',
        cancelButtonText: '返回勾选',
      })
    } catch {
      return
    }
  }
  saving.value = true
  try {
    const payload = {
      name: form.name.trim(),
      description: form.description.trim(),
      permissions: [...form.permissions],
    }
    if (editing.value) await adminApi.updateRole(form.code, payload)
    else await adminApi.createRole({ code: form.code.trim(), ...payload })
    ElMessage.success(editing.value ? '角色已更新' : '角色已创建')
    dialogVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(apiErrorText(e, editing.value ? '角色更新失败' : '角色创建失败'))
  } finally {
    saving.value = false
  }
}

async function remove(row: RoleItem): Promise<void> {
  if (row.builtin) {
    ElMessage.warning('内置角色不可删除')
    return
  }
  try {
    await ElMessageBox.confirm(
      `删除后不可恢复，已分配该角色的用户将失去对应权限。确认删除角色「${row.name}」（${row.code}）？`,
      '删除确认',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  busyCode.value = row.code
  try {
    await adminApi.deleteRole(row.code)
    ElMessage.success('角色已删除')
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) {
      ElMessage.error(`删除被拒绝：${e.message || '内置角色或仍有用户正在使用该角色'}（请先解除用户分配）`)
    } else {
      ElMessage.error(apiErrorText(e, '角色删除失败'))
    }
  } finally {
    busyCode.value = ''
  }
}
</script>

<template>
  <div>
    <el-card class="page-card" shadow="never">
      <div class="table-toolbar">
        <div class="toolbar-info">
          <strong>共 {{ roles.length }} 个角色</strong>
          <span class="form-tip">
            权限码由后端统一维护；勾选后立即影响被分配该角色的用户菜单与按钮显隐，接口侧仍会二次校验。
          </span>
        </div>
        <div class="toolbar-actions">
          <el-button :loading="loading" @click="load">刷新</el-button>
          <el-button type="primary" :disabled="!canWrite" @click="openAdd">＋ 新建角色</el-button>
        </div>
      </div>

      <p v-if="!canWrite" class="form-tip warn-tip">当前账号缺少 role:write 权限，只能查看角色与权限，不能新建/修改/删除。</p>

      <el-alert
        v-if="listError"
        :title="`角色列表加载失败：${listError}`"
        type="error"
        :closable="false"
        show-icon
        style="margin-bottom: 12px"
      />

      <el-table :data="roles" v-loading="loading" row-key="code">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="perm-detail">
              <span class="detail-label">权限（{{ (row.permissions ?? []).length }}）</span>
              <div v-if="(row.permissions ?? []).length" class="tags-wrap">
                <el-tag v-for="code in row.permissions" :key="code" size="small" effect="plain" class="tag-item">
                  {{ permLabel(code) }}
                </el-tag>
              </div>
              <span v-else class="muted">未授予任何权限</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="角色名称" min-width="180">
          <template #default="{ row }">
            <div class="name-cell">
              <span class="name-line">{{ row.name }}</span>
              <span class="sub-line">{{ row.description || '—' }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="code" min-width="150">
          <template #default="{ row }">
            <span class="mono">{{ row.code }}</span>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="100" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.builtin" type="warning" size="small" effect="dark">内置</el-tag>
            <el-tag v-else type="info" size="small" effect="plain">自定义</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="用户数" width="90" align="center">
          <template #default="{ row }">
            <span :class="{ muted: !row.userCount }">{{ row.userCount ?? 0 }}</span>
          </template>
        </el-table-column>
        <el-table-column label="权限数" width="90" align="center">
          <template #default="{ row }">
            <span :class="{ muted: !(row.permissions ?? []).length }">{{ (row.permissions ?? []).length }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" :disabled="!canWrite" @click="openEdit(row)">编辑</el-button>
            <el-tooltip content="内置角色不可删除" placement="top" :disabled="!row.builtin">
              <span class="del-wrap">
                <el-button
                  link
                  type="danger"
                  :disabled="row.builtin || !canWrite"
                  :loading="busyCode === row.code"
                  @click="remove(row)"
                >
                  删除
                </el-button>
              </span>
            </el-tooltip>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无角色" :image-size="90" />
        </template>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="editing ? `编辑角色 · ${form.code}` : '新建角色'"
      width="820px"
      top="6vh"
      append-to-body
      :close-on-click-modal="false"
    >
      <el-alert
        v-if="editing && roles.find((role) => role.code === form.code)?.builtin"
        title="内置角色：不可修改 code、不可删除，权限可按需调整（调整后请确认核心功能仍可访问）。"
        type="warning"
        :closable="false"
        show-icon
        style="margin-bottom: 14px"
      />
      <el-alert
        v-if="catalogError"
        :title="`权限目录不可用：${catalogError}`"
        type="warning"
        :closable="false"
        show-icon
        style="margin-bottom: 14px"
      />

      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-row :gutter="16">
          <el-col :span="10">
            <el-form-item label="角色 code" prop="code">
              <el-input
                v-model="form.code"
                :disabled="editing"
                placeholder="如 operator"
                :maxlength="64"
                clearable
              />
            </el-form-item>
          </el-col>
          <el-col :span="14">
            <el-form-item label="角色名称" prop="name">
              <el-input v-model="form.name" placeholder="如 运营专员" :maxlength="40" clearable />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="描述">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="2"
            placeholder="该角色的职责范围（可选）"
            :maxlength="200"
            show-word-limit
          />
        </el-form-item>

        <el-form-item :label="`权限（已选 ${form.permissions.length} / ${totalPermissions}）`">
          <div class="perm-picker">
            <div class="perm-toolbar">
              <el-button size="small" @click="selectAll">全选</el-button>
              <el-button size="small" @click="clearAll">清空</el-button>
              <span class="form-tip">按分组勾选，组标题前的复选框可整组全选。</span>
            </div>
            <div v-loading="catalogLoading" class="perm-groups">
              <div v-for="group in groups" :key="group.name" class="perm-group">
                <div class="perm-group-head">
                  <el-checkbox
                    :model-value="groupState(group).all"
                    :indeterminate="groupState(group).indeterminate"
                    @change="toggleGroup(group)"
                  >
                    <strong>{{ group.name }}</strong>
                    <span class="group-count">{{ groupState(group).hit }}/{{ group.items.length }}</span>
                  </el-checkbox>
                </div>
                <div class="perm-group-body">
                  <el-checkbox
                    v-for="item in group.items"
                    :key="item.code"
                    :model-value="isChecked(item.code)"
                    @change="onToggle(item.code, $event)"
                  >
                    <span class="perm-name">{{ item.name || item.code }}</span>
                    <span class="perm-code mono">{{ item.code }}</span>
                  </el-checkbox>
                </div>
                <p v-if="group.items.some((item) => item.description)" class="form-tip perm-desc">
                  {{ group.items.filter((item) => item.description).map((item) => `${item.code}：${item.description}`).join('；') }}
                </p>
              </div>
              <el-empty v-if="!catalogLoading && groups.length === 0" description="暂无可勾选的权限码" :image-size="72" />
            </div>
          </div>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">
          {{ editing ? '保存修改' : '确认新建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.toolbar-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.toolbar-actions {
  display: flex;
  gap: 10px;
}
.name-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.name-line {
  font-weight: 600;
}
.sub-line {
  color: #8593a8;
  font-size: 12px;
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
.tag-item {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
}
.perm-detail {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 4px 12px 8px;
}
.detail-label {
  font-size: 12px;
  color: #8593a8;
}
.warn-tip {
  margin: -6px 0 12px;
  color: #e6a23c;
}
.del-wrap {
  display: inline-block;
}
.perm-picker {
  width: 100%;
  border: 1px solid #e4eaf2;
  border-radius: 8px;
  padding: 10px 12px;
  background: #fbfcfe;
}
.perm-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
  flex-wrap: wrap;
}
.perm-groups {
  max-height: 46vh;
  overflow: auto;
}
.perm-group + .perm-group {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed #e4eaf2;
}
.perm-group-head {
  margin-bottom: 4px;
}
.group-count {
  color: #8593a8;
  font-size: 12px;
  margin-left: 6px;
}
.perm-group-body {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 18px;
  padding-left: 22px;
}
.perm-group-body :deep(.el-checkbox) {
  height: 26px;
  margin-right: 0;
}
.perm-name {
  font-size: 13px;
}
.perm-code {
  color: #8593a8;
  margin-left: 6px;
}
.perm-desc {
  margin: 4px 0 0 22px;
}
</style>
