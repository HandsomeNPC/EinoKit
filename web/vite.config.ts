import {fileURLToPath, URL} from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// 后端路由不带 /api 前缀(见 CLAUDE.md)，所以这里按真实路径逐个代理，
// 不能用一条 /api 前缀规则兜住。
const backend = 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/threads': backend,
      '/health': backend,
    },
  },
})
