/**
 * 购物车本地占位存储（演示环境：不发起任何网络请求）。
 *
 * 原位于 src/data/mall.ts；内容数据改由 ContentService 提供后，
 * 该本地存储能力独立成模块，页面不再直接依赖内置数据文件。
 */

/** 购物车本地占位存储键（localStorage）；键名保持不变以兼容既有本机数据。 */
export const cartStorageKey = 'zhiwellcare.mall.cart.v1'

/** 购物车条目：仅记录商品 id 与数量，商品信息实时由 ContentService 解析。 */
export interface MallCartItem {
  productId: string
  quantity: number
}

function isMallCartItem(value: unknown): value is MallCartItem {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return (
    typeof item.productId === 'string' &&
    typeof item.quantity === 'number' &&
    Number.isInteger(item.quantity) &&
    item.quantity > 0
  )
}

/** 读取本地购物车；数据缺失或损坏时安全降级为空购物车。 */
export function readMallCart(): MallCartItem[] {
  try {
    const raw = localStorage.getItem(cartStorageKey)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter(isMallCartItem)
  } catch {
    return []
  }
}

/** 写回本地购物车（演示占位，无网络请求）。 */
export function writeMallCart(items: MallCartItem[]): void {
  try {
    localStorage.setItem(cartStorageKey, JSON.stringify(items))
  } catch {
    /* 存储不可用时忽略，不影响页面浏览。 */
  }
}
