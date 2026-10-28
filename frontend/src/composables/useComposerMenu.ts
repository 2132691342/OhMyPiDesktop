// 输入区的两个弹出菜单：`/` 命令 与 `@` 文件引用，同一时刻只开一个。
//
// 拆出来的理由：菜单的开关时机（输入里有没有 @ / 是否以 / 开头）与输入区
// 的发送逻辑无关。放在一起会让「什么时候弹菜单」和「回车干什么」纠缠不清。
import { computed, ref, watch } from 'vue'
import type { ComputedRef, Ref } from 'vue'
import { useChatStore } from '@/stores/chat'
import { useBuiltinCommands, type BuiltinKey } from '@/composables/useBuiltinCommands'
import type { SlashCommand } from '@/types/api'

/** 菜单里的一项。builtin 由界面执行，engine 只是填进输入框。 */
export interface MenuItem {
  /** 稳定键：内置命令用 BuiltinKey，引擎命令用 name */
  key: string
  /** 显示名（不带斜杠） */
  name: string
  /** 一句话说明 */
  desc: string
  /** 来源决定选中后做什么 */
  source: 'builtin' | 'engine'
}

type MenuKind = 'slash' | 'mention' | null

/**
 * 绑定两个菜单到草稿。
 *
 * pickPrompt 在选中引擎命令时被调用，由输入区决定「怎么填进框、焦点放哪」；
 * 内置命令由这里直接执行（它不需要输入框参与）。
 */
export function useComposerMenu(
  draft: Ref<string>,
  pickPrompt: (name: string) => void,
) {
  const chat = useChatStore()
  const builtins = useBuiltinCommands()

  const menu = ref<MenuKind>(null)
  const menuIndex = ref(0)
  const busyCmd = ref('')

  /** 输入到一半的 @ 触发引用菜单；`/` 开头且还没空格时触发命令菜单。 */
  function detect(value: string): void {
    if (/@\S*$/.test(value)) {
      menu.value = 'mention'
      return
    }
    if (value.startsWith('/') && !value.includes(' ')) {
      menu.value = 'slash'
      menuIndex.value = 0
      return
    }
    menu.value = null
  }

  watch(draft, detect)

  const mentionOpen = computed(() => menu.value === 'mention')

  /** 合并后的命令表：内置在前、引擎在后，按输入过滤。 */
  const slashItems: ComputedRef<MenuItem[]> = computed(() => {
    const query = draft.value.slice(1).toLowerCase()
    const merged: MenuItem[] = [
      ...builtins.items().map((b) => ({ ...b, source: 'builtin' as const })),
      ...chat.commands.map((c: SlashCommand) => ({
        key: c.name,
        name: c.name,
        desc: c.description || c.input?.hint || '',
        source: 'engine' as const,
      })),
    ]
    if (!query) return merged
    return merged.filter((item) => item.name.toLowerCase().includes(query))
  })

  const slashOpen = computed(() => menu.value === 'slash' && slashItems.value.length > 0)

  /** 跑一条内置命令，把结果话术转成提示。 */
  async function runBuiltin(key: string): Promise<void> {
    busyCmd.value = key
    try {
      const note = await builtins.run(key as BuiltinKey)
      // 空串表示「不需要提示」（例如只是把目光引到工具条）。
      if (note) chat.pushNotice('info', note)
    } catch (err) {
      chat.promptError = err instanceof Error ? err.message : String(err)
    } finally {
      busyCmd.value = ''
    }
  }

  /** 选中一条：内置的直接跑，引擎的交给输入区去填。 */
  function applyItem(item: MenuItem): void {
    menu.value = null
    if (item.source === 'engine') {
      pickPrompt(item.name)
      return
    }
    void runBuiltin(item.key)
  }

  /** 上下移动选中项。 */
  function move(delta: number): void {
    const total = slashItems.value.length
    if (total === 0) return
    menuIndex.value = (menuIndex.value + delta + total) % total
  }

  function close(): void {
    menu.value = null
  }

  function openMention(): void {
    menu.value = 'mention'
  }

  return {
    menu,
    menuIndex,
    busyCmd,
    mentionOpen,
    slashOpen,
    slashItems,
    applyItem,
    move,
    close,
    openMention,
  }
}
