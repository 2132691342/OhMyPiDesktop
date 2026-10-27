// 轻提示：一句原因 + 可选下一步。悬停暂停计时，可点选复制。
// 不做成一闪而过的 toast——失败信息消失了就等于没提示。
import { ref } from 'vue'

export interface ToastItem {
  id: number
  level: 'info' | 'ok' | 'warn' | 'error'
  message: string
  detail?: string
}

const items = ref<ToastItem[]>([])
const timers = new Map<number, number>()
let seq = 0

function clear(id: number) {
  const timer = timers.get(id)
  if (timer) window.clearTimeout(timer)
  timers.delete(id)
  items.value = items.value.filter((t) => t.id !== id)
}

function arm(item: ToastItem, ttl: number) {
  timers.set(
    item.id,
    window.setTimeout(() => clear(item.id), ttl),
  )
}

export function useToasts() {
  function push(level: ToastItem['level'], message: string, detail?: string) {
    if (!message) return
    const item: ToastItem = { id: ++seq, level, message, detail }
    items.value = [...items.value, item].slice(-4)
    arm(item, level === 'error' ? 8000 : 4200)
    return item.id
  }

  function dismiss(id: number) {
    clear(id)
  }

  /** 鼠标悬停时暂停倒计时，移开再续 4 秒。 */
  function hold(id: number, paused: boolean) {
    const timer = timers.get(id)
    if (!timer) return
    window.clearTimeout(timer)
    timers.delete(id)
    if (!paused) {
      const item = items.value.find((t) => t.id === id)
      if (item) arm(item, 4000)
    }
  }

  return { items, push, dismiss, hold }
}
