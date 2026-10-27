// Wails 绑定层：只用来拿本地服务地址与令牌，以及窗口控制。
// 业务一律走 HTTP，所以这个文件保持很小。
import type { Bootstrap } from '@/types/api'

interface Endpoint {
  port: number
  token: string
  version: string
  contract: number
  base_url: string
}

interface WailsBindings {
  Endpoint(): Promise<Endpoint>
  PickFolder(defaultDir: string): Promise<string>
  OpenExternal(url: string): Promise<void>
  WindowMinimize(): Promise<void>
  WindowToggleMaximize(): Promise<void>
  WindowClose(): Promise<void>
  Settings(): Promise<Record<string, string>>
}

declare global {
  interface Window {
    go?: {
      main?: {
        App?: WailsBindings
      }
    }
    runtime?: {
      EventsOn(name: string, cb: (data: unknown) => void): void
      EventsOff(name: string): void
    }
  }
}

function bindings(): WailsBindings | undefined {
  return window.go?.main?.App
}

export function hasBindings(): boolean {
  return Boolean(bindings())
}

export async function getEndpoint(): Promise<Endpoint> {
  const api = bindings()
  if (!api) {
    // 纯浏览器调试：允许手填端口，方便只用 vite 调后端
    const port = Number(localStorage.getItem('omp.dev.port') ?? 0)
    const token = localStorage.getItem('omp.dev.token') ?? ''
    if (!port) throw new Error('没有拿到 Wails 绑定。请在桌面应用中运行，或先设置 localStorage 的 omp.dev.port / omp.dev.token。')
    return { port, token, version: 'dev', contract: 0, base_url: `http://127.0.0.1:${port}/api/v1` }
  }
  return api.Endpoint()
}

export async function pickFolder(defaultDir: string): Promise<string> {
  const api = bindings()
  if (!api) return ''
  return api.PickFolder(defaultDir)
}

export async function openExternal(url: string): Promise<void> {
  const api = bindings()
  if (api) {
    await api.OpenExternal(url)
    return
  }
  window.open(url, '_blank')
}

export const windowCtl = {
  minimize: () => bindings()?.WindowMinimize(),
  toggleMaximize: () => bindings()?.WindowToggleMaximize(),
  close: () => bindings()?.WindowClose(),
}

export function onStartupError(cb: (message: string) => void): void {
  window.runtime?.EventsOn('app:startup-error', (data) => {
    const payload = data as { message?: string }
    cb(payload?.message ?? '后端没有启动起来')
  })
}

export type { Endpoint, Bootstrap }
