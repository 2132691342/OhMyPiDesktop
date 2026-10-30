// 输入区的内置斜杠命令：界面自己就能做完的动作。
//
// 与「引擎命令」分开是因为它们的性质不同：
//   - 内置命令由界面执行，点了立刻发生什么很明确；
//   - 引擎命令只是往输入框里填一段文字，真正生效靠引擎那一轮处理。
//
// 只列「点了真的会发生什么」的条目。终端里那些只活在 TUI 上的命令
// （/tree /share /bug）不列——这里的界面没有对应的东西可展示。
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'
import { useAppStore } from '@/stores/app'

/** 内置命令的键。新增一条要同时加 i18n 文案与 run 分支。 */
export type BuiltinKey = 'new' | 'model' | 'thinking' | 'compact' | 'copy' | 'export' | 'reload'

interface BuiltinDef {
  key: BuiltinKey
  nameKey: string
  descKey: string
}

const DEFS: BuiltinDef[] = [
  { key: 'new', nameKey: 'chat.slashNew', descKey: 'chat.slashNewDesc' },
  { key: 'model', nameKey: 'chat.slashModel', descKey: 'chat.slashModelDesc' },
  { key: 'thinking', nameKey: 'chat.slashThinking', descKey: 'chat.slashThinkingDesc' },
  { key: 'compact', nameKey: 'chat.slashCompact', descKey: 'chat.slashCompactDesc' },
  { key: 'copy', nameKey: 'chat.slashCopy', descKey: 'chat.slashCopyDesc' },
  { key: 'export', nameKey: 'chat.slashExport', descKey: 'chat.slashExportDesc' },
  { key: 'reload', nameKey: 'chat.slashReload', descKey: 'chat.slashReloadDesc' },
]

/**
 * 内置命令的清单与执行。
 *
 * 执行返回的错误话术由调用方显示——这里不弹提示，
 * 否则「谁在提示」会分散到三个文件里。
 */
export function useBuiltinCommands() {
  const chat = useChatStore()
  const app = useAppStore()
  const { t } = useI18n()

  /** 菜单里要显示的内置命令（已翻译）。 */
  function items(): { key: BuiltinKey; name: string; desc: string }[] {
    return DEFS.map((d) => ({ key: d.key, name: t(d.nameKey), desc: t(d.descKey) }))
  }

  /** 执行一条内置命令。返回成功提示话术（空串表示不需要提示）。 */
  async function run(key: BuiltinKey): Promise<string> {
    switch (key) {
      case 'new':
        await chat.newChat()
        return t('chat.newChatStarted')

      case 'compact':
        if (!chat.sessionFile) return t('chat.slashNeedSession')
        await chat.compact()
        return t('chat.slashCompactDone')

      case 'model':
        // 模型面板就在输入框底栏上：直接叫它打开，比再弹一层少一步。
        chat.pickerRequest = 'model'
        return ''

      case 'thinking':
        chat.pickerRequest = 'thinking'
        return ''

      case 'copy':
        return copyLastAnswer()

      case 'export':
        return exportMarkdown()

      case 'reload':
        await app.reloadAll()
        return t('chat.slashReloadDone')
    }
  }

  /** 复制最后一条助手回答的纯文本。 */
  async function copyLastAnswer(): Promise<string> {
    const turns = chat.history
    for (let i = turns.length - 1; i >= 0; i--) {
      const turn = turns[i]
      if (!turn || turn.role !== 'assistant') continue
      const text = turn.blocks
        .filter((b): b is { kind: 'text'; text: string } => b.kind === 'text' && Boolean(b.text))
        .map((b) => b.text)
        .join('\n')
        .trim()
      if (!text) break
      await navigator.clipboard.writeText(text)
      return t('chat.slashCopyDone')
    }
    return t('chat.slashCopyEmpty')
  }

  /**
   * 导出当前会话为 Markdown。
   *
   * 用浏览器下载而不是系统保存对话框：内容是我们自己拼的，走 Blob 少一次
   * 跨层调用；用户还能从下载目录里找到它。
   */
  async function exportMarkdown(): Promise<string> {
    if (!chat.sessionFile) return t('chat.slashNeedSession')
    const lines: string[] = []
    for (const turn of chat.history) {
      lines.push(turn.role === 'user' ? '## 我' : '## 助手')
      lines.push('')
      for (const block of turn.blocks) {
        if (block.kind === 'text' && block.text) lines.push(block.text)
      }
      lines.push('')
    }
    const blob = new Blob([lines.join('\n')], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `session-${Date.now()}.md`
    link.click()
    URL.revokeObjectURL(url)
    return t('chat.slashExportDone')
  }

  return { items, run }
}
