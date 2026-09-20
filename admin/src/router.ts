import { createRouter, createWebHashHistory } from 'vue-router'
import type { Router } from 'vue-router'
import { adminToken } from './api/token'
import { menuPermission } from './constants/permissions'
import { useAdminPermissionsStore } from './stores/adminPermissions'
import { pinia } from './stores/pinia'
import AdminLayout from './layouts/AdminLayout.vue'
import LoginView from './views/LoginView.vue'
import DashboardView from './views/DashboardView.vue'
import ForbiddenView from './views/ForbiddenView.vue'
import DevicesView from './views/catalog/DevicesView.vue'
import GamesView from './views/catalog/GamesView.vue'
import CoursesView from './views/content/CoursesView.vue'
import GoodsView from './views/content/GoodsView.vue'
import WhitelistView from './views/devices/WhitelistView.vue'
import AssetsView from './views/assets/AssetsView.vue'
import UsersView from './views/users/UsersView.vue'
import RecordsView from './views/records/RecordsView.vue'
import RolesView from './views/system/RolesView.vue'
import ConfigView from './views/system/ConfigView.vue'

declare module 'vue-router' {
  interface RouteMeta {
    /** 免登录页面 */
    public?: boolean
    /** 顶栏标题 */
    title?: string
    /** 访问该页所需权限码（任一命中即可，见 useAdminPermissionsStore.allows） */
    permission?: string[]
  }
}

export const router: Router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/login', component: LoginView, meta: { public: true } },
    {
      path: '/',
      component: AdminLayout,
      redirect: '/dashboard',
      children: [
        { path: 'dashboard', component: DashboardView, meta: { title: '仪表盘' } },
        { path: 'devices', component: DevicesView, meta: { title: '设备型号', permission: menuPermission('/devices') } },
        {
          path: 'devices/whitelist',
          component: WhitelistView,
          meta: { title: '设备白名单 / 映射', permission: menuPermission('/devices/whitelist') },
        },
        { path: 'games', component: GamesView, meta: { title: '游戏目录', permission: menuPermission('/games') } },
        {
          path: 'content/courses',
          component: CoursesView,
          meta: { title: '教程课程', permission: menuPermission('/content/courses') },
        },
        {
          path: 'content/goods',
          component: GoodsView,
          meta: { title: '商品管理', permission: menuPermission('/content/goods') },
        },
        { path: 'assets', component: AssetsView, meta: { title: '资产发布', permission: menuPermission('/assets') } },
        { path: 'users', component: UsersView, meta: { title: '用户管理', permission: menuPermission('/users') } },
        { path: 'records', component: RecordsView, meta: { title: '训练记录', permission: menuPermission('/records') } },
        {
          path: 'system/roles',
          component: RolesView,
          meta: { title: '角色权限', permission: menuPermission('/system/roles') },
        },
        {
          path: 'system/config',
          component: ConfigView,
          meta: { title: '配置中心', permission: menuPermission('/system/config') },
        },
        { path: 'forbidden', component: ForbiddenView, meta: { title: '无访问权限' } },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
  ],
})

router.beforeEach(async (to) => {
  if (!to.meta.public && !adminToken.get()) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.path === '/login' && adminToken.get()) return { path: '/dashboard' }

  const required = to.meta.permission
  if (!to.meta.public && required && required.length > 0) {
    const perms = useAdminPermissionsStore(pinia)
    // 首次进入（或刷新页面）时先拉一次权限，否则无法判断菜单/路由可见性。
    if (!perms.loaded && !perms.loading) await perms.load()
    if (!perms.allows(required)) {
      return { path: '/forbidden', query: { from: to.fullPath } }
    }
  }
  return true
})
