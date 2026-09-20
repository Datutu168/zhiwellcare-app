<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAdminAuthStore } from '../stores/adminAuth'
import { useAdminPermissionsStore } from '../stores/adminPermissions'

const auth = useAdminAuthStore()
const perms = useAdminPermissionsStore()
const route = useRoute()
const router = useRouter()

const phone = ref('')
const password = ref('')
const loading = ref(false)

onMounted(() => auth.restore())

// 先做前端校验：手机号为空/格式错误、密码为空时直接提示，避免“只填了密码就提交”导致 401。
async function submit(): Promise<void> {
  const phoneValue = phone.value.trim()
  if (!/^1[3-9]\d{9}$/.test(phoneValue)) {
    auth.error = '请输入正确的 11 位手机号（如 13800008888）'
    return
  }
  if (!password.value) {
    auth.error = '请输入密码'
    return
  }
  loading.value = true
  try {
    await auth.login(phoneValue, password.value)
    // 登录成功后立即拉权限：role=admin 或拥有任意权限码才允许进入后台。
    // 权限接口未就绪（loaded=false）时放行，避免后端未上线就完全无法登录。
    perms.reset()
    await perms.load(true)
    if (perms.loaded && !perms.isSuperAdmin && perms.permissions.length === 0) {
      auth.logout()
      perms.reset()
      auth.error = '该账号没有后台访问权限，请联系管理员分配角色'
      return
    }
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/dashboard'
    await router.replace(redirect.startsWith('/') ? redirect : '/dashboard')
  } catch {
    // auth.error 已填充
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <el-card class="login-card">
      <div class="brand">
        <div class="brand-logo">康</div>
        <div>
          <h1>智为康乐 · 运营后台</h1>
          <p>设备型号 / 游戏目录 / 用户与训练数据管理</p>
        </div>
      </div>
      <el-alert v-if="auth.error" :title="auth.error" type="error" :closable="false" show-icon style="margin-bottom: 14px" />
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item label="管理员手机号">
          <el-input v-model="phone" type="tel" maxlength="11" placeholder="13800008888"
                    size="large" autofocus @keyup.enter="submit" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="password" type="password" show-password placeholder="请输入密码" size="large" @keyup.enter="submit" />
        </el-form-item>
        <el-button type="primary" size="large" style="width: 100%" :loading="loading" native-type="submit">
          登 录
        </el-button>
      </el-form>
      <p class="hint">本机管理员账号：13800008888 / admin123456</p>
      <p class="hint">
        消费版运营后台仅管理主动训练设备与健身游戏目录；医疗红线同 APP 口径。
      </p>
    </el-card>
  </div>
</template>

<style scoped>
.login-page { min-height: 100vh; display: grid; place-items: center; padding: 24px;
  background: linear-gradient(150deg, #0436a7 0%, #0d74c4 34%, #10b3cd 66%, #0dd5c1 100%); }
.login-card { width: min(400px, 100%); border-radius: 18px; }
.brand { display: flex; gap: 12px; align-items: center; margin-bottom: 18px; }
.brand-logo { width: 46px; height: 46px; border-radius: 12px; display: grid; place-items: center;
  background: linear-gradient(135deg, #0436a7, #0dd5c1); color: #fff; font-size: 24px; font-weight: 800; }
.brand h1 { font-size: 18px; margin: 0; }
.brand p { margin: 2px 0 0; color: #8593a8; font-size: 12px; }
.hint { margin: 12px 0 0; color: #8593a8; font-size: 12px; text-align: center; }
</style>
