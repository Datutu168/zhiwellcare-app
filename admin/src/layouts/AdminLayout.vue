<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { adminApi, apiErrorText, type ChangePasswordPayload } from '../api/admin'
import { ADMIN_MENUS } from '../constants/permissions'
import {
  PASSWORD_MAX_LENGTH,
  PASSWORD_MIN_LENGTH,
  emptyPasswordChangeForm,
  passwordChangeError,
  passwordRule,
  type PasswordChangeForm,
} from '../constants/account'
import { useAdminAuthStore } from '../stores/adminAuth'
import { useAdminPermissionsStore } from '../stores/adminPermissions'

const auth = useAdminAuthStore()
const perms = useAdminPermissionsStore()
const route = useRoute()
const router = useRouter()

onMounted(() => {
  void perms.load()
})

/** 无权限的菜单直接不渲染；权限接口未就绪时 allows 放行（后端仍会 403 兜底）。 */
const menus = computed(() => ADMIN_MENUS.filter((menu) => perms.allows(menu.permission)))

const activeMenu = computed(() => {
  const exact = menus.value.find((menu) => route.path === menu.path)
  if (exact) return exact.path
  const nested = menus.value
    .filter((menu) => route.path.startsWith(`${menu.path}/`))
    .sort((a, b) => b.path.length - a.path.length)[0]
  return nested?.path ?? ''
})

const pageTitle = computed(() => route.meta.title ?? menus.value.find((m) => m.path === activeMenu.value)?.label ?? '')

/* ------------------------------------------------------------------ *
 * 修改密码（个人账号自助操作，任何登录用户都可用，不做权限码门禁）
 * ------------------------------------------------------------------ */

const passwordVisible = ref(false)
const passwordSaving = ref(false)
const passwordForm = reactive<PasswordChangeForm>(emptyPasswordChangeForm())
const passwordFormRef = ref<FormInstance>()

const passwordRules: FormRules = {
  oldPassword: [passwordRule('oldPassword', passwordForm)],
  newPassword: [passwordRule('newPassword', passwordForm)],
  confirmPassword: [passwordRule('confirmPassword', passwordForm)],
}

async function openPasswordDialog(): Promise<void> {
  Object.assign(passwordForm, emptyPasswordChangeForm())
  passwordVisible.value = true
  await nextTick()
  passwordFormRef.value?.clearValidate()
}

/** 关闭（取消 / 右上角 / 成功后）统一清空口令，避免明文密码留在页面上。 */
function closePasswordDialog(): void {
  passwordVisible.value = false
  Object.assign(passwordForm, emptyPasswordChangeForm())
  passwordFormRef.value?.clearValidate()
}

function userCommand(command: string): void {
  if (command === 'password') void openPasswordDialog()
  else if (command === 'logout') logout()
}

async function submitPassword(): Promise<void> {
  if (passwordSaving.value) return
  const message = passwordChangeError(passwordForm)
  if (message) {
    // 与各业务页一致：先给中文提示，再触发表单自身的失焦校验提示（不作为唯一防线）。
    // 校验失败不清空输入：用户往往只需改其中一项。
    ElMessage.error(message)
    void passwordFormRef.value?.validate().catch(() => false)
    return
  }
  const payload: ChangePasswordPayload = {
    oldPassword: passwordForm.oldPassword,
    newPassword: passwordForm.newPassword,
  }
  passwordSaving.value = true
  try {
    await adminApi.changePassword(payload)
    closePasswordDialog()
    // 后端限制：改密不会吊销已签发的 access token，其他设备需等令牌过期才失效。
    ElMessage.success('密码已修改，其他设备上的登录在令牌过期前仍然有效，如需立即下线请重新登录。')
  } catch (e) {
    // 失败时展示后端返回的中文 message（如「原密码不正确」）；输入一并清空并保留弹窗。
    ElMessage.error(apiErrorText(e, '密码修改失败，请稍后重试'))
    Object.assign(passwordForm, emptyPasswordChangeForm())
  } finally {
    passwordSaving.value = false
  }
}

function logout(): void {
  auth.logout()
  perms.reset()
  void router.replace('/login')
}
</script>

<template>
  <el-container class="shell">
    <el-aside width="210px" class="aside">
      <div class="brand">
        <span class="brand-logo">康</span>
        <div class="brand-text">
          <strong>智为康乐</strong>
          <small>运营管理后台</small>
        </div>
      </div>
      <el-menu :default-active="activeMenu" router class="menu">
        <el-menu-item v-for="menu in menus" :key="menu.path" :index="menu.path">
          <span class="menu-ic">{{ menu.icon }}</span>
          <span>{{ menu.label }}</span>
        </el-menu-item>
      </el-menu>
      <div class="aside-foot">
        <a class="to-app" href="http://localhost:1420/" target="_blank" rel="noreferrer">打开用户端 APP ↗</a>
        <p class="comply">纯消费版主动训练目录管理 · 医疗红线同 APP</p>
      </div>
    </el-aside>
    <el-container>
      <el-header class="header">
        <h2>{{ pageTitle }}</h2>
        <el-dropdown trigger="click" @command="userCommand">
          <span class="user">
            <el-avatar :size="30" style="background: #0436a7">{{ (auth.user?.nickname || '管').slice(0, 1) }}</el-avatar>
            <span class="user-name">{{ auth.user?.nickname || auth.user?.phoneMasked || '管理员' }}</span>
            <span v-if="perms.roles.length" class="user-role">{{ perms.roles.join('/') }}</span>
            <span class="caret">⌄</span>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="password">修改密码</el-dropdown-item>
              <el-dropdown-item command="logout" divided>退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </el-header>
      <el-main class="main">
        <el-alert
          v-if="perms.error"
          type="warning"
          show-icon
          :closable="false"
          class="perm-alert"
          :title="`权限信息加载失败（${perms.error}）：菜单暂全部显示，具体操作仍由后端校验，越权会返回 403。`"
        />
        <RouterView />
      </el-main>
    </el-container>

    <!-- 修改密码：顶栏用户菜单进入。任何已登录用户都可改自己的密码，不做权限码门禁。 -->
    <el-dialog
      v-model="passwordVisible"
      title="修改密码"
      width="420px"
      append-to-body
      :close-on-click-modal="false"
      @closed="closePasswordDialog"
    >
      <el-form ref="passwordFormRef" :model="passwordForm" :rules="passwordRules" label-position="top">
        <el-form-item label="原密码" prop="oldPassword">
          <el-input
            v-model="passwordForm.oldPassword"
            type="password"
            show-password
            placeholder="请输入当前登录密码"
            :maxlength="PASSWORD_MAX_LENGTH"
            autocomplete="current-password"
          />
        </el-form-item>
        <el-form-item label="新密码" prop="newPassword">
          <el-input
            v-model="passwordForm.newPassword"
            type="password"
            show-password
            :placeholder="`${PASSWORD_MIN_LENGTH}-${PASSWORD_MAX_LENGTH} 位，不能与原密码相同`"
            :maxlength="PASSWORD_MAX_LENGTH"
            autocomplete="new-password"
          />
        </el-form-item>
        <el-form-item label="确认新密码" prop="confirmPassword">
          <el-input
            v-model="passwordForm.confirmPassword"
            type="password"
            show-password
            placeholder="请再次输入新密码"
            :maxlength="PASSWORD_MAX_LENGTH"
            autocomplete="new-password"
          />
        </el-form-item>
      </el-form>
      <p class="pwd-tip">
        修改成功后，其他设备上已登录的会话在当前令牌过期前仍然有效；本设备无需重新登录。
      </p>
      <template #footer>
        <el-button :disabled="passwordSaving" @click="closePasswordDialog">取消</el-button>
        <el-button type="primary" :loading="passwordSaving" @click="submitPassword">确定</el-button>
      </template>
    </el-dialog>
  </el-container>
</template>

<style scoped>
.shell { height: 100vh; }
.aside { background: #fff; border-right: 1px solid #e4eaf2; display: flex; flex-direction: column; }
.brand { display: flex; gap: 10px; align-items: center; padding: 18px 16px 12px; }
.brand-logo { width: 38px; height: 38px; border-radius: 10px; display: grid; place-items: center;
  background: linear-gradient(135deg, #0436a7, #0dd5c1); color: #fff; font-size: 20px; font-weight: 800; }
.brand-text { display: grid; line-height: 1.3; }
.brand-text strong { font-size: 16px; }
.brand-text small { color: #8593a8; font-size: 12px; }
.menu { border-right: 0; flex: 1; overflow-y: auto; }
.menu-ic { margin-right: 8px; }
.aside-foot { padding: 12px 16px; }
.to-app { font-size: 13px; color: #0436a7; text-decoration: none; }
.comply { color: #8593a8; font-size: 11px; line-height: 1.6; margin: 8px 0 0; }
.header { background: #fff; border-bottom: 1px solid #e4eaf2; display: flex; align-items: center;
  justify-content: space-between; }
.header h2 { font-size: 17px; margin: 0; }
.user { display: flex; align-items: center; gap: 8px; cursor: pointer; }
.user-name { font-size: 14px; }
.user-role { font-size: 11px; color: #0436a7; background: #eef3fc; border-radius: 8px; padding: 1px 6px; }
.caret { color: #8593a8; font-size: 12px; }
.main { background: #f3f6fb; padding: 18px; }
.perm-alert { margin-bottom: 14px; }
.pwd-tip { margin: 4px 0 0; color: #8593a8; font-size: 12px; line-height: 1.7; }
</style>
