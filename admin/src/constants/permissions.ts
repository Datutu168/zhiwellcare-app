/**
 * 权限码与后台菜单定义。
 *
 * 权限码以后端 `GET /admin/me/permissions` 返回的 code 为准（角色编辑页也会直接用
 * `GET /admin/permissions` 返回的目录渲染分组复选框，不依赖下面的常量）。
 * 下面的常量只用于「菜单/按钮显隐」这一层 UX 判断，因此每项都给了若干别名，
 * 只要命中其中之一即视为有权限；`role=admin` 或持有 `*` 时一律放行。
 * 注意：前端隐藏菜单只是体验优化，真正的鉴权始终由后端 403 决定。
 */
export const PERM = {
  deviceRead: 'device:read',
  deviceWrite: 'device:write',
  gameRead: 'game:read',
  userRead: 'user:read',
  userWrite: 'user:write',
  userRoleWrite: 'user:role:write',
  recordRead: 'record:read',
  roleRead: 'role:read',
  roleWrite: 'role:write',
  configRead: 'config:read',
  configWrite: 'config:write',
  whitelistRead: 'whitelist:read',
  whitelistWrite: 'whitelist:write',
  assetRead: 'asset:read',
  assetWrite: 'asset:write',
} as const

/** 菜单（与 router.ts 的 meta.permission 保持一致）。permission 为空表示登录即可见。 */
export interface AdminMenu {
  path: string
  label: string
  icon: string
  permission: string[]
}

export const ADMIN_MENUS: AdminMenu[] = [
  { path: '/dashboard', label: '仪表盘', icon: '📊', permission: [] },
  {
    path: '/devices',
    label: '设备型号',
    icon: '⌚',
    permission: [PERM.deviceRead, 'device:write', 'device:manage', 'device_model:read'],
  },
  {
    path: '/devices/whitelist',
    label: '白名单 / 映射',
    icon: '🧾',
    permission: [
      PERM.whitelistRead,
      PERM.whitelistWrite,
      // 设备映射接口用的是 device:read/device:write，故一并放行
      PERM.deviceRead,
      PERM.deviceWrite,
      'device:whitelist:read',
      'device_whitelist:read',
    ],
  },
  {
    path: '/games',
    label: '游戏目录',
    icon: '🎯',
    permission: [PERM.gameRead, 'game:write', 'game:manage'],
  },
  {
    path: '/assets',
    label: '资产发布',
    icon: '📦',
    permission: [PERM.assetRead, PERM.assetWrite, 'asset:manage', 'assets:read'],
  },
  {
    path: '/users',
    label: '用户管理',
    icon: '👥',
    permission: [PERM.userRead, PERM.userWrite, 'user:manage'],
  },
  {
    path: '/records',
    label: '训练记录',
    icon: '📈',
    permission: [PERM.recordRead, 'training:read', 'record:manage'],
  },
  {
    path: '/system/roles',
    label: '角色权限',
    icon: '🔐',
    permission: [PERM.roleRead, PERM.roleWrite, 'rbac:read', 'role:manage'],
  },
  {
    path: '/system/config',
    label: '配置中心',
    icon: '⚙️',
    permission: [PERM.configRead, PERM.configWrite, 'system:config:read', 'config:manage'],
  },
]

/** 路由与菜单共用同一份权限要求，避免两处写歪。 */
export function menuPermission(path: string): string[] {
  return ADMIN_MENUS.find((menu) => menu.path === path)?.permission ?? []
}
