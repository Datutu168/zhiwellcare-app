/**
 * 冒烟测试用的 jsdom 环境（跟随 vitest.config.ts 的 test.environment 加载）。
 *
 * 之所以自定义而不用 vitest 内置的 'jsdom'：
 * 1) 内置环境要从 vitest 自身位置解析 jsdom 包（本子包已自行声明 jsdom 依赖，路径解析更可控）；
 * 2) 需要 transformMode: 'web'，否则 .vue 会被 SSR transform 编译，
 *    挂载时会调用 useSSRContext() 而报 “Server rendering context not provided”。
 */
import { JSDOM } from 'jsdom'
import { webcrypto } from 'node:crypto'

const GLOBAL_KEYS = [
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'HTMLTextAreaElement',
  'SVGElement',
  'Element',
  'Node',
  'Event',
  'CustomEvent',
  'MouseEvent',
  'KeyboardEvent',
  'FocusEvent',
  'InputEvent',
  'File',
  'FileList',
  'Blob',
  'DOMParser',
  'MutationObserver',
  'getComputedStyle',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'localStorage',
  'sessionStorage',
  'location',
  'history',
  'screen',
  'CSS',
]

export default {
  name: 'admin-smoke-jsdom',
  transformMode: 'web' as const,
  async setup(global: Record<string, unknown>) {
    const dom = new JSDOM('<!doctype html><html><body></body></html>', {
      url: 'http://localhost:5173/',
      pretendToBeVisual: true, // 提供 requestAnimationFrame（Element Plus 表格/标签页布局需要）
    })
    const win = dom.window as unknown as Record<string, unknown>

    for (const key of GLOBAL_KEYS) {
      const value = win[key]
      if (value === undefined) continue
      const finalValue = typeof value === 'function' ? (value as (...args: unknown[]) => unknown).bind(dom.window) : value
      try {
        Object.defineProperty(global, key, { value: finalValue, writable: true, configurable: true, enumerable: true })
      } catch {
        /* Node 只读全局：保留 Node 自带实现 */
      }
    }

    Object.defineProperty(global, 'window', { value: dom.window, writable: true, configurable: true })
    Object.defineProperty(global, 'document', { value: dom.window.document, writable: true, configurable: true })

    // jsdom 未实现的两个 observer，Element Plus 的表格会用到。
    if (typeof global.ResizeObserver !== 'function') {
      class ResizeObserverStub {
        observe(): void {}
        unobserve(): void {}
        disconnect(): void {}
      }
      global.ResizeObserver = ResizeObserverStub
    }

    if (typeof global.IntersectionObserver !== 'function') {
      class IntersectionObserverStub {
        observe(): void {}
        unobserve(): void {}
        disconnect(): void {}
        takeRecords(): unknown[] {
          return []
        }
      }
      global.IntersectionObserver = IntersectionObserverStub
    }

    if (typeof global.matchMedia !== 'function') {
      global.matchMedia = () => ({
        matches: false,
        addEventListener(): void {},
        removeEventListener(): void {},
        addListener(): void {},
        removeListener(): void {},
        dispatchEvent(): boolean {
          return false
        },
      })
    }

    // jsdom 的 window.crypto 没有 subtle；真实浏览器在 https/localhost 下有，
    // 这里用 Node 的 webcrypto 顶替，以便真实验证「浏览器计算 SHA-256」这条链路。
    if (!(global.crypto as Crypto | undefined)?.subtle) {
      Object.defineProperty(global, 'crypto', { value: webcrypto, writable: true, configurable: true })
    }

    return {
      teardown(): void {
        dom.window.close()
      },
    }
  },
}
