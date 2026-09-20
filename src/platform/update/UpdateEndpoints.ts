/**
 * 更新源基础地址：优先 VITE_UPDATE_BASE（自有域名 / COS / CDN），
 * 未配置时回退到项目 GitHub Releases —— 二者路径结构一致，切换只需改环境变量并重新构建。
 * 例：VITE_UPDATE_BASE=https://dl.zhiwellcare.com/releases
 */
const UPDATE_BASE = (
  import.meta.env.VITE_UPDATE_BASE ?? 'https://github.com/NovSyang/zhiwellcare-app/releases/latest/download'
).replace(/\/+$/, '')

export const UPDATE_ENDPOINTS = {
  // 桌面端和 Android 都从更新源拉取清单；清单内的资源地址由发布脚本按同一基础地址生成。
  tauri: `${UPDATE_BASE}/latest.json`,
  android: `${UPDATE_BASE}/android-latest.json`,
} as const
