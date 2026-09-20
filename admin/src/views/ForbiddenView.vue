<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAdminPermissionsStore } from '../stores/adminPermissions'

const route = useRoute()
const router = useRouter()
const perms = useAdminPermissionsStore()

const reloading = ref(false)

const from = computed(() => (typeof route.query.from === 'string' ? route.query.from : ''))
const roleText = computed(() => (perms.roles.length ? perms.roles.join('、') : '未分配角色'))
const permText = computed(() =>
  perms.permissions.length ? perms.permissions.join('、') : '无任何权限码（或权限接口未返回数据）',
)

async function reload(): Promise<void> {
  reloading.value = true
  try {
    await perms.load(true)
    if (perms.loaded) ElMessage.success('权限已刷新')
    else ElMessage.warning(perms.error || '权限信息仍不可用')
  } finally {
    reloading.value = false
  }
}

function goDashboard(): void {
  void router.replace('/dashboard')
}
</script>

<template>
  <el-card class="page-card" shadow="never">
    <el-result icon="warning" title="无权访问该页面（403）" :sub-title="from ? `请求地址：${from}` : ''">
      <template #extra>
        <div class="forbidden-body">
          <el-descriptions :column="1" border size="small" class="forbid-desc">
            <el-descriptions-item label="当前角色">{{ roleText }}</el-descriptions-item>
            <el-descriptions-item label="已授予权限">
              <span class="mono">{{ permText }}</span>
            </el-descriptions-item>
          </el-descriptions>
          <p class="form-tip">
            该页面对应的权限未授予当前账号。请联系管理员在「角色权限」中为你的角色勾选相应权限，或直接为你分配角色。
          </p>
          <div class="forbid-actions">
            <el-button type="primary" @click="goDashboard">返回仪表盘</el-button>
            <el-button :loading="reloading" @click="reload">刷新权限</el-button>
          </div>
        </div>
      </template>
    </el-result>
  </el-card>
</template>

<style scoped>
.forbidden-body {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}
.forbid-desc {
  width: min(560px, 90vw);
  text-align: left;
}
.forbid-actions {
  display: flex;
  gap: 10px;
  margin-top: 4px;
}
.mono {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  font-size: 12px;
  word-break: break-all;
}
</style>
