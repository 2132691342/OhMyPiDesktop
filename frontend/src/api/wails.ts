// Wails 绑定层：拿本地服务地址与令牌、系统对话框、窗口控制。
//
// v3 的绑定由 `wails3 generate bindings` 静态生成（见 frontend/bindings/），
// 这里只做两件事：把生成的调用包成 Promise，以及保留「纯浏览器调后端」的兜底。
// 业务一律走 HTTP，所以这个文件保持很小。
import * as App from '@wails/OhMyPiDesktop/app'
import { Events } from '@wailsio/runtime'
import type { Endpoint } from '@wails/OhMyPiDesktop/models'

export type { Endpoint }

// 纯浏览器调试时检测不到绑定：wails3 dev 之外（比如只用 vite）走这条。
// 生成代码在 WebView 里总是存在，所以「有没有绑定」= 能不能调到 Endpoint。
let devFallback = false

async function call<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn()
  } catch {
    devFallback = true
    throw new Error('没有拿到 Wails 绑定。请在桌面应用中运行，或先设置 localStorage 的 omp.dev.port / omp.dev.token。')
  }
}

export async function getEndpoint(): Promise<Endpoint> {
  if (devFallback) return devEndpoint()
  try {
    return await App.Endpoint()
  } catch {
    devFallback = true
    return devEndpoint()
  }
}

// 只调后端时不必开 Wails：go run ./tools/devserver -token dev 会打印这一行里的端口与令牌。
function devEndpoint(): Endpoint {
  const port = Number(localStorage.getItem('omp.dev.port') ?? 0)
  const token = localStorage.getItem('omp.dev.token') ?? ''
  if (!port) throw new Error('没有拿到 Wails 绑定，也没有设置 omp.dev.port / omp.dev.token。')
  return { port, token, version: 'dev', contract: 0, base_url: `http://127.0.0.1:${port}/api/v1` }
}

export async function pickFolder(defaultDir: string): Promise<string> {
  return call(() => App.PickFolder(defaultDir))
}

export async function openExternal(url: string): Promise<void> {
  try {
    await App.OpenExternal(url)
  } catch {
    window.open(url, '_blank')
  }
}

export const windowCtl = {
  minimize: () => void App.WindowMinimize(),
  toggleMaximize: () => void App.WindowToggleMaximize(),
  close: () => void App.WindowClose(),
}

// 启动失败由后端经事件推来：装配没完成时窗口已经起来了，前端要能显示原因。
export function onStartupError(cb: (message: string) => void): void {
  void Events.On('app:startup-error', (data: unknown) => {
    const payload = data as { message?: string }
    cb(payload?.message ?? '后端没有启动起来')
  })
}
