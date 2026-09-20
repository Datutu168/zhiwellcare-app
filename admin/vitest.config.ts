import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

/**
 * admin 子包独立的 vitest 配置（仅供冒烟测试使用，不影响 `npm run build`——那个走 vite.config.ts）。
 * 运行：cd admin && npm run smoke
 *
 * 关键点：
 * - environment 指向本地 jsdom 桩（`smoke/vitest-env.ts`），并把它标为 transformMode: 'web'，
 *   让 .vue 走客户端编译（SSR transform 会在挂载时调用 useSSRContext 而失败）。
 * - include 只收 `*.smoke.ts`，用例文件刻意不叫 *.test.ts，
 *   因此仓库根的 `npm test` 默认 include（**\/*.test.ts）永远收不到它。
 * - 不产生任何构建产物（vitest 在内存中转译），仓库里不会出现 smoke-dist 之类的目录。
 */
export default defineConfig({
  plugins: [vue()],
  test: {
    root: fileURLToPath(new URL('.', import.meta.url)),
    environment: './smoke/vitest-env.ts',
    include: ['smoke/**/*.smoke.ts'],
    exclude: ['**/node_modules/**', '**/dist/**', '**/smoke-dist/**'],
    testTimeout: 120_000,
    hookTimeout: 60_000,
  },
})
