import { defineStore } from 'pinia'
import { adminApi } from '../api/admin'

interface AdminPermissionsState {
  roles: string[]
  permissions: string[]
  loaded: boolean
  loading: boolean
  error: string
}

/**
 * 当前登录管理员的角色与权限码（来源 `GET /admin/me/permissions`）。
 * 用于菜单/按钮显隐：加载失败时（后端接口未就绪等）不隐藏菜单，只在页面上给出提示，
 * 因为真正的鉴权由后端 403 兜底，前端隐藏只是体验优化。
 */
export const useAdminPermissionsStore = defineStore('adminPermissions', {
  state: (): AdminPermissionsState => ({
    roles: [],
    permissions: [],
    loaded: false,
    loading: false,
    error: '',
  }),
  getters: {
    /** superAdmin：role=admin 或持有通配 `*` 视为拥有全部权限。 */
    isSuperAdmin(state): boolean {
      return state.roles.includes('admin') || state.permissions.includes('*')
    },
  },
  actions: {
    async load(force = false): Promise<void> {
      if (this.loading) return
      if (this.loaded && !force) return
      this.loading = true
      try {
        const data = await adminApi.mePermissions()
        this.roles = Array.isArray(data?.roles) ? data.roles : []
        this.permissions = Array.isArray(data?.permissions) ? data.permissions : []
        this.loaded = true
        this.error = ''
      } catch (e) {
        // 不抛出：菜单侧需要“降级显示 + 提示”，避免整个后台不可用。
        this.loaded = false
        this.roles = []
        this.permissions = []
        this.error = e instanceof Error ? e.message : '权限信息加载失败'
      } finally {
        this.loading = false
      }
    },

    /** 单个权限码判断，支持后端下发 `device:*` 这类前缀通配。 */
    can(code: string): boolean {
      if (!code) return true
      if (this.isSuperAdmin) return true
      if (this.permissions.includes(code)) return true
      return this.permissions.some((grant) => grant.endsWith(':*') && code.startsWith(grant.slice(0, -1)))
    },

    /** 任一命中即通过；codes 为空视为不需要权限；权限未加载成功时放行（后端仍会校验）。 */
    allows(codes?: string[]): boolean {
      if (!codes || codes.length === 0) return true
      if (!this.loaded) return true
      return codes.some((code) => this.can(code))
    },

    reset(): void {
      this.roles = []
      this.permissions = []
      this.loaded = false
      this.loading = false
      this.error = ''
    },
  },
})
