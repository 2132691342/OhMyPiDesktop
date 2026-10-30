// 一轮对话的「过程读数」：状态文字、引擎小组件、提示条、重试进度。
//
// 单独一个 composable 的理由：这四样是引擎在回答过程中报上来的过程信息，
// 它们有一个共同的性质——**用完即弃**，下一轮就重来。
// 而消息、历史、模型那些是「会话状态」，要跨轮保留。
// 两者放在一起时，「reset 要清哪些」这件事就不清楚了。
import { ref } from 'vue'

/** 一条过程提示。 */
export interface Notice {
  id: number
  level: string
  message: string
}

/** 引擎报上来的一组读数（多行）。 */
export interface Widget {
  key: string
  lines: string[]
}

/** 重试进度。 */
export interface RetryState {
  attempt: number
  max: number
  delay_ms: number
}

/**
 * 一轮对话的过程读数。
 */
export function useRunStatus() {
  const statusText = ref('')
  const widgets = ref<Widget[]>([])
  const notices = ref<Notice[]>([])
  const retry = ref<RetryState | null>(null)
  let noticeSeq = 0

  /** 推一条过程提示。最多留四条：更多的话屏幕会被它占满。 */
  function pushNotice(level: string, message: string): void {
    notices.value = [...notices.value, { id: ++noticeSeq, level, message }].slice(-4)
  }

  function dismissNotice(id: number): void {
    notices.value = notices.value.filter((n) => n.id !== id)
  }

  /**
   * 设状态文字。
   *
   * key 与 text 任一为空就清掉：引擎报「这一项没有值」时，
   * 界面上留一条空行比不留更糟。
   */
  function setStatus(key: string, text: string): void {
    if (key === '' || text === '') {
      statusText.value = ''
      return
    }
    statusText.value = text
  }

  /** 设一组读数；给了空行就撤掉这一组。 */
  function setWidget(key: string, lines: string[]): void {
    const rest = widgets.value.filter((w) => w.key !== key)
    widgets.value = (lines ?? []).length ? [...rest, { key, lines }] : rest
  }

  /** 清空这一轮的过程读数（收尾或换会话时）。 */
  function reset(): void {
    statusText.value = ''
    widgets.value = []
    notices.value = []
    retry.value = null
  }

  return {
    statusText,
    widgets,
    notices,
    retry,
    pushNotice,
    dismissNotice,
    setStatus,
    setWidget,
    reset,
  }
}
