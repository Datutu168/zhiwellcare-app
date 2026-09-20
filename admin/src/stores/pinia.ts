import { createPinia } from 'pinia'

/**
 * 共享的 Pinia 实例：路由守卫（在组件外）也要读取权限 store，
 * 因此这里显式导出实例并传入 `useXxxStore(pinia)`，避免“没有激活的 Pinia”报错。
 */
export const pinia = createPinia()
