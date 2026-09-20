import { ApiError, api } from './http'

/* ------------------------------------------------------------------ *
 * 统一信封 {code,message,data} 由 http.ts 处理，这里只声明各接口的 data 形状。
 * 后端契约见任务说明（/api/v1/admin/*），后端未就绪时统一抛出 ApiError。
 * ------------------------------------------------------------------ */

export type AssetKind = 'web' | 'game' | 'firmware'
export type AssetStatus = 'published' | 'offline' | string

export interface MePermissions {
  roles: string[]
  permissions: string[]
}

export interface PermissionItem {
  code: string
  name: string
  group: string
  description?: string
}

export interface RoleItem {
  code: string
  name: string
  description?: string
  builtin: boolean
  permissions: string[]
  userCount: number
}

export interface RolePayload {
  name: string
  description: string
  permissions: string[]
}

export interface ConfigItem {
  key: string
  value: unknown
  description?: string
  updatedAt?: string
}

export interface DeviceWhitelistItem {
  deviceKey: string
  modelId: string
  note?: string
  enabled: boolean
}

export interface DeviceMappingItem {
  deviceKey: string
  modelId: string
  note?: string
}

export interface AssetItem {
  id: string | number
  kind: AssetKind | string
  refId: string
  version: string
  filename: string
  objectKey: string
  size: number
  sha256: string
  contentType: string
  notes?: string
  status: AssetStatus
  createdAt?: string
  publishedAt?: string | null
}

export interface UploadTicket {
  key: string
  uploadUrl: string
  method?: string
  expiresInSeconds?: number
  publicUrl?: string
}

export interface UploadUrlPayload {
  kind: AssetKind
  refId: string
  version: string
  filename: string
  contentType: string
}

export interface RegisterAssetPayload {
  kind: AssetKind
  refId: string
  version: string
  filename: string
  objectKey: string
  size: number
  sha256: string
  contentType: string
  notes: string
}

export interface DeviceKeyPayload {
  deviceKey: string
  modelId: string
  note: string
}

/** 白名单 POST 为 upsert（同一 deviceKey 即更新），后端额外接受可选 enabled。 */
export interface WhitelistPayload extends DeviceKeyPayload {
  enabled?: boolean
}

/** 复用既有设备型号目录接口（DevicesView 同样直接取数组）。 */
export interface DeviceModelBrief {
  modelId: string
  name: string
}

function seg(value: string): string {
  return encodeURIComponent(value)
}

export const adminApi = {
  /* 权限 */
  mePermissions: () => api.get<MePermissions>('/admin/me/permissions'),
  listPermissions: () => api.get<{ items: PermissionItem[] }>('/admin/permissions'),

  /* 角色 */
  listRoles: () => api.get<{ items: RoleItem[] }>('/admin/roles'),
  createRole: (body: RolePayload & { code: string }) => api.post<{ role: RoleItem }>('/admin/roles', body),
  updateRole: (code: string, body: RolePayload) => api.put<{ role: RoleItem }>(`/admin/roles/${seg(code)}`, body),
  deleteRole: (code: string) => api.delete<{ deleted: boolean }>(`/admin/roles/${seg(code)}`),

  /* 用户角色 */
  userRoles: (id: string) =>
    api.get<{ roles: string[]; permissions?: string[] }>(`/admin/users/${seg(id)}/roles`),
  setUserRoles: (id: string, roles: string[]) =>
    api.put<{ roles: string[] }>(`/admin/users/${seg(id)}/roles`, { roles }),

  /* 配置中心 */
  listConfigs: () => api.get<{ items: ConfigItem[] }>('/admin/config'),
  putConfig: (key: string, body: { value: unknown; description?: string }) =>
    api.put<ConfigItem>(`/admin/config/${seg(key)}`, body),

  /* 设备白名单（POST 为 upsert） */
  listWhitelist: () => api.get<{ items: DeviceWhitelistItem[] }>('/admin/device-whitelist'),
  addWhitelist: (body: WhitelistPayload) => api.post<{ item?: DeviceWhitelistItem }>('/admin/device-whitelist', body),
  deleteWhitelist: (deviceKey: string) =>
    api.delete<{ deleted: boolean }>(`/admin/device-whitelist/${seg(deviceKey)}`),

  /* 设备型号目录（白名单/映射的 modelId 下拉用） */
  listDeviceModels: () => api.get<DeviceModelBrief[]>('/admin/devices'),

  /* 设备映射（POST 为 upsert） */
  listMappings: () => api.get<{ items: DeviceMappingItem[] }>('/admin/device-mappings'),
  addMapping: (body: DeviceKeyPayload) => api.post<{ item?: DeviceMappingItem }>('/admin/device-mappings', body),
  deleteMapping: (deviceKey: string) =>
    api.delete<{ deleted: boolean }>(`/admin/device-mappings/${seg(deviceKey)}`),

  /* 资产发布 */
  createUploadUrl: (body: UploadUrlPayload) => api.post<UploadTicket>('/admin/assets/upload-url', body),
  registerAsset: (body: RegisterAssetPayload) => api.post<{ asset?: AssetItem }>('/admin/assets', body),
  listAssets: (kind: AssetKind, refId: string) => {
    const params = new URLSearchParams({ kind })
    if (refId.trim()) params.set('refId', refId.trim())
    return api.get<{ items: AssetItem[] }>(`/admin/assets?${params.toString()}`)
  },
  setAssetStatus: (id: string | number, status: 'published' | 'offline') =>
    api.patch<{ asset?: AssetItem }>(`/admin/assets/${seg(String(id))}/status`, { status }),
  deleteAsset: (id: string | number) => api.delete<{ deleted: boolean }>(`/admin/assets/${seg(String(id))}`),
}

/** 把任意异常转成可展示的中文提示（接口未就绪 / 无权限 / 冲突都给出可操作信息）。 */
export function apiErrorText(e: unknown, fallback = '操作失败，请稍后重试'): string {
  if (e instanceof ApiError) {
    const raw = e.message || ''
    switch (e.status) {
      case 0:
        return raw || '无法连接后端服务，请确认后端已启动'
      case 401:
        return raw || '登录已过期，请重新登录'
      case 403:
        return raw || '当前账号没有该操作权限（403）'
      case 404:
        return `${raw || '接口不存在'}（后端可能尚未上线该接口）`
      case 409:
        return raw || '操作冲突：内置角色或仍被使用的角色不可删除'
      case 500:
      case 502:
      case 503:
        return `${raw || '服务异常'}（后端错误 ${e.status}）`
      default:
        return raw || fallback
    }
  }
  return e instanceof Error ? e.message : fallback
}
