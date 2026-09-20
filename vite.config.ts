import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  clearScreen: false,
  server: {
    port: 1420,
    strictPort: true,
  },
  envPrefix: ['VITE_', 'TAURI_'],
  build: {
    target: process.env.TAURI_ENV_PLATFORM === 'windows' ? 'chrome105' : 'es2022',
    minify: !process.env.TAURI_ENV_DEBUG ? 'esbuild' : false,
    sourcemap: !!process.env.TAURI_ENV_DEBUG,
  },
  test: {
    // 根测试只覆盖 App 本体（src/ 与 tests/）。
    // admin/ 是独立子包（自带 package.json、依赖与浏览器环境桩），
    // 其冒烟测试由 admin 侧自己的命令运行，避免把它的环境假设带进根套件。
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      '**/release-output/**',
      '**/smoke-dist/**',
      'admin/**',
    ],
  },
})
