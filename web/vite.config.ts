import { defineConfig, type ProxyOptions, type UserConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

// 冻结配置 CLI 的严格公共契约（与 Go internal/conf.Public 一一对应）。
interface PublicConfig {
  http_base_url: string
  http_address: string
  http_host: string
  http_port: number
  frontend_port: number
}

// 显式白名单 env 覆盖：只透传这些非敏感键，绝不携带 DSN/Redis 密码等凭据。
const CONFIG_ENV_WHITELIST = [
  'POKER_CONFIG_FILE',
  'POKER_HTTP_HOST',
  'POKER_HTTP_PORT',
  'POKER_HTTP_TIMEOUT',
  'POKER_FRONTEND_PORT',
] as const

// 运行 go run 所需的最小非凭据变量。
const PASSTHROUGH_ENV = ['PATH', 'HOME', 'GOCACHE', 'GOPATH', 'GOROOT', 'TMPDIR'] as const

function resolvePublicConfig(): PublicConfig {
  // 配置根目录 = 本配置文件所在目录的上一级（web/ 的父目录，即项目根）。
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
  const env: NodeJS.ProcessEnv = {}
  for (const key of [...PASSTHROUGH_ENV, ...CONFIG_ENV_WHITELIST]) {
    const value = process.env[key]
    if (value !== undefined) env[key] = value
  }
  const stdout = execFileSync('go', ['run', './cmd/config'], { cwd: root, env, encoding: 'utf8' })
  const data = JSON.parse(stdout) as PublicConfig
  if (data.http_host !== '127.0.0.1' && data.http_host !== 'localhost') {
    throw new Error('CONFIG_HTTP_HOST_NOT_LOCAL')
  }
  if (data.http_address !== `${data.http_host}:${data.http_port}`) {
    throw new Error('CONFIG_HTTP_ADDRESS_INVALID')
  }
  if (data.http_base_url !== `http://${data.http_address}`) {
    throw new Error('CONFIG_HTTP_BASE_URL_INVALID')
  }
  return data
}

export default defineConfig(({ command }): UserConfig => {
  const config: UserConfig = {
    plugins: [react()],
    build: { outDir: 'dist', emptyOutDir: true, target: 'es2021' },
  }
  // build 不连接后端、不触发 CLI；只有 dev server 才解析配置，避免与 dist 构建形成依赖环。
  if (command !== 'serve') return config
  const resolved = resolvePublicConfig()
  const backendOrigin = `http://${resolved.http_address}`
  const proxy: Record<string, ProxyOptions> = {
    '/api': {
      target: backendOrigin,
      changeOrigin: true,
      ws: false,
      configure: (proxyServer) => {
        // 后端严格校验 Origin，转发时统一改写为后端自身来源。
        proxyServer.on('proxyReq', (proxyReq) => {
          proxyReq.setHeader('Origin', backendOrigin)
        })
      },
    },
  }
  return {
    ...config,
    server: {
      host: resolved.http_host,
      port: resolved.frontend_port,
      strictPort: true,
      proxy,
    },
  }
})
