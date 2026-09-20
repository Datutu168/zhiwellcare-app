<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { changePassword } from '../../api/auth'
import { ChangePasswordForm } from '../../core/account/ChangePasswordForm'
import { useAuthStore } from '../../stores/auth'

const router = useRouter()
const auth = useAuthStore()

// 表单状态与校验集中在 ChangePasswordForm（core 单测覆盖），页面只做绑定与展示。
// 用 ref 包一层让表单字段获得响应式，模板可直接 v-model 到 form.oldPassword 等。
const form = ref(new ChangePasswordForm(changePassword))
/** 成功后停留一小会儿展示提示，再回到「我的」；离开页面时清理定时器。 */
let backTimer: number | null = null

// 与「我的」首页一致：先恢复本地令牌，再校验登录态（令牌失效由 store 现有逻辑清理会话）。
onMounted(() => {
  auth.restoreFromStorage()
  if (auth.accessToken) void auth.ensureProfile()
})

onBeforeUnmount(() => {
  if (backTimer !== null) window.clearTimeout(backTimer)
})

/** 提交：成功后回到「我的」；失败时提示由表单状态给出（取后端中文 message）。 */
async function submit(): Promise<void> {
  const ok = await form.value.submit()
  if (ok) {
    if (backTimer === null) backTimer = window.setTimeout(() => { void router.replace('/mine') }, 1_200)
    return
  }
  // 令牌确实失效（401）时，交给 store 现有逻辑（ensureProfile 失败即清空会话）收尾，
  // 页面随即显示上面的登录引导；此处不新增全局跳转行为。
  if (form.value.lastErrorStatus === 401) void auth.ensureProfile()
}

function backToMine(): void { void router.push('/mine') }
function goLogin(): void { void router.push('/auth/login?redirect=/mine/password') }
</script>

<template>
  <main class="content-page">
    <header class="subpage-head">
      <button class="button ghost small subpage-back" type="button" @click="backToMine">← 返回</button>
      <div class="subpage-head-text">
        <p class="eyebrow">Security</p>
        <h1>修改密码</h1>
      </div>
    </header>

    <div class="subpage-body">
      <!-- 未登录：与「我的」一致，引导登录后回到本页继续操作（不新增全局路由守卫）。 -->
      <section v-if="!auth.isLoggedIn" class="card pwd-cta">
        <strong>请先登录</strong>
        <p class="muted small">修改密码需要登录账号；登录成功后会回到本页继续操作。</p>
        <div class="row">
          <button class="button primary" type="button" @click="goLogin">去登录</button>
          <button class="button ghost" type="button" @click="backToMine">返回我的</button>
        </div>
      </section>

      <template v-else>
        <section class="card">
          <div class="section-title">设置新密码</div>
          <form @submit.prevent="submit">
            <div class="auth-field">
              <label for="pwd-old">原密码</label>
              <input id="pwd-old" v-model="form.oldPassword" class="auth-input" type="password" maxlength="64"
                     autocomplete="current-password" placeholder="请输入当前登录密码" />
            </div>
            <div class="auth-field">
              <label for="pwd-new">新密码</label>
              <input id="pwd-new" v-model="form.newPassword" class="auth-input" type="password" maxlength="64"
                     autocomplete="new-password" placeholder="6-64 位，区分大小写" />
            </div>
            <div class="auth-field">
              <label for="pwd-confirm">确认新密码</label>
              <input id="pwd-confirm" v-model="form.confirmPassword" class="auth-input" type="password" maxlength="64"
                     autocomplete="new-password" placeholder="再次输入新密码" />
            </div>

            <p v-if="form.errorMessage" class="auth-error">{{ form.errorMessage }}</p>
            <p v-if="form.successMessage" class="success-text pwd-success" aria-live="polite">{{ form.successMessage }}</p>

            <button class="button primary wide pwd-submit" type="submit" :disabled="form.busy">
              {{ form.busy ? '修改中…' : '确认修改' }}
            </button>
          </form>
        </section>

        <p class="compliance-note">
          <strong>说明：</strong>修改密码不会让其他已登录设备立即退出——已签发的登录令牌在过期前仍然有效；
          如需在其他设备上立即退出，请在该设备上手动退出登录。
        </p>

        <button class="button ghost wide subpage-bottom-button" type="button" @click="backToMine">返回我的</button>
      </template>
    </div>
  </main>
</template>

<style scoped>
.subpage-head { display: flex; align-items: center; gap: 14px; margin-bottom: 18px; }
.subpage-back { flex: 0 0 auto; }
.subpage-head-text { min-width: 0; }
.subpage-head-text h1 { margin-bottom: 0; }
.subpage-body { display: flex; flex-direction: column; gap: 16px; }
.pwd-cta strong { font-size: 16px; display: block; }
.pwd-cta p { margin: 4px 0 0; }
.pwd-success { margin: 0 0 12px; font-size: 13px; line-height: 1.7; }
.pwd-submit { min-height: 48px; }
.subpage-bottom-button { min-height: 48px; }
</style>
