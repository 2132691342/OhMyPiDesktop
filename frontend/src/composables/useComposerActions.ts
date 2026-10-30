// 输入区的六个动作：新建 / 换模型 / 换思考档 / 开关自动压缩 / 切引擎。
//
// 拆出来的理由：它们是「点一下 → 调一个接口 → 失败就把原话写进 promptError」，
// 五个动作长得一模一样。留在输入区里会让那个文件一半是键盘逻辑、
// 一半是五段重复的 try/catch。
//
// 这里不持有按钮的 loading 态：那是「点下去到回来之间」的瞬时状态，
// 由调用方（输入区）持有更合适——它同时还要管发送与排队两个 loading。
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'

/**
 * 统一收口「调用失败」：把原话写进 promptError。
 *
 * 为什么不弹 toast：这些动作失败时用户正盯着输入区，
 * 而 promptError 那条横幅就挂在输入区上方，位置比 toast 更准。
 */
export function useComposerActions() {
  const chat = useChatStore()
  const { t } = useI18n()

  async function guard(fn: () => Promise<void>): Promise<void> {
    try {
      await fn()
    } catch (err) {
      chat.promptError = err instanceof Error ? err.message : String(err)
    }
  }

  async function startNewChat(): Promise<void> {
    await guard(async () => {
      await chat.newChat()
      chat.pushNotice('info', t('chat.newChatStarted'))
    })
  }

  async function pickAutoCompaction(enabled: boolean): Promise<void> {
    await guard(async () => {
      await chat.setAutoCompaction(enabled)
      chat.pushNotice('info', enabled ? t('chat.autoCompactOn') : t('chat.autoCompactOff'))
    })
  }

  return { startNewChat, pickAutoCompaction }
}
