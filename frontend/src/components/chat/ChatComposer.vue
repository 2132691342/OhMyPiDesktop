<script setup lang="ts">
// 输入区：多行输入 + 待发送附件 + @ 引用 + / 命令菜单 + 工具行 + 发送/停止。
//
// 分区照搬 WorkBaby：
//   - 输入块给 68px 的最小高度，空态也有一个大目标可点，点空白即落光标；
//   - 工具行与输入块之间有一条分隔线，工具行不再抢输入框的横向空间；
//   - 待发送附件占独立一行，超过两行就在区内滚动，不把输入框顶出屏。
//
// 本文件只做编排（持有状态、把事件接到各处），三块各自在自己的组件里：
//   ComposerStatus 状态条 / ComposerMenu 弹出菜单 / ComposerBar 工具行。
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ComposerStatus from '@/components/chat/ComposerStatus.vue'
import ComposerMenu from '@/components/chat/ComposerMenu.vue'
import ComposerBar from '@/components/chat/ComposerBar.vue'
import AttachStrip from '@/components/chat/AttachStrip.vue'
import { useChatStore } from '@/stores/chat'
import { useAppStore } from '@/stores/app'
import { useComposerDraft } from '@/composables/useComposerDraft'
import { useAttachments, baseName, toWorkspaceRelative } from '@/composables/useAttachments'
import { useComposerMenu } from '@/composables/useComposerMenu'
import { useComposerActions } from '@/composables/useComposerActions'
import { pickFiles } from '@/api/wails'
import type { EngineKind } from '@/types/api'
import type { MenuItem } from '@/composables/useComposerMenu'

const chat = useChatStore()
const app = useAppStore()
const { t } = useI18n()

const draft = ref('')
const pickingEngine = ref(false)
const textarea = ref<HTMLTextAreaElement | null>(null)
const sending = ref(false)
const queueing = ref(false)
const stopping = ref(false)
const attachError = ref('')

// 草稿按会话存：切走再切回来还在，关掉窗口也不丢。
const draftStore = useComposerDraft(computed(() => chat.sessionFile), draft)

// 附件：粘贴图片 / 回形针选文件 / @ 引用工作区文件，三种来源一个列表。
const attachments = useAttachments()

// 菜单：/ 命令 与 @ 引用。开关时机与选中后的动作都在 composable 里。
const composerMenu = useComposerMenu(draft, (name) => {
  draft.value = `/${name} `
  composerMenu.close()
  void nextTick(() => textarea.value?.focus())
})
const { menuIndex, busyCmd, mentionOpen, slashOpen, slashItems, applyItem } = composerMenu

// ── 菜单选中 ──
function onPickItem(item: MenuItem): void {
  applyItem(item)
  if (item.source === 'engine') void nextTick(autosize)
}

function onPickFile(relPath: string, name: string): void {
  const err = attachments.addWorkspaceFile(relPath, name)
  if (err) {
    attachError.value = err
    return
  }
  // 把触发菜单的那段 @xxx 从输入里去掉：引用已经变成附件条目了。
  draft.value = draft.value.replace(/@\S*$/, '').trimEnd()
  composerMenu.close()
  void nextTick(() => {
    autosize()
    textarea.value?.focus()
  })
}

// ── 附件 ──
async function onPaste(event: ClipboardEvent): Promise<void> {
  const errors = await attachments.addFromClipboard(event.clipboardData)
  if (errors.length) {
    attachError.value = errors.join('；')
    return
  }
  // 有图片时吃掉这次粘贴，避免同一张图既进附件又变成一段二进制文本；
  // 没有图片就放行——普通文本粘进输入框是正常操作。
  if (attachments.images.value.length) event.preventDefault()
}

/** 回形针：选文件。工作区内的变成相对路径引用，工作区外的给绝对路径。 */
async function browseFiles(): Promise<void> {
  const picked = await pickFiles(t('chat.attachPick'))
  if (!picked.length) return
  const errors: string[] = []
  for (const path of picked) {
    const rel = toWorkspaceRelative(path, app.workspace)
    const err = attachments.addWorkspaceFile(rel ?? path, baseName(path))
    if (err) errors.push(err)
  }
  if (errors.length) attachError.value = errors.join('；')
  void nextTick(() => textarea.value?.focus())
}

// ── 输入与键盘 ──
function autosize(): void {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${Math.min(220, el.scrollHeight)}px`
}

/** 回车发送、Shift+回车换行。输入法组合中的回车不算发送。 */
function onKeydown(event: KeyboardEvent): void {
  if (event.isComposing) return

  if (slashOpen.value) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      composerMenu.move(1)
      return
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      composerMenu.move(-1)
      return
    }
    if (event.key === 'Enter' || event.key === 'Tab') {
      event.preventDefault()
      const item = slashItems.value[menuIndex.value]
      if (item) onPickItem(item)
      return
    }
    if (event.key === 'Escape') {
      event.preventDefault()
      composerMenu.close()
      return
    }
  }

  if (mentionOpen.value && event.key === 'Escape') {
    event.preventDefault()
    composerMenu.close()
    return
  }

  // 运行中按 Esc = 停止；只在输入框为空时响应，免得误按清掉上下文
  if (event.key === 'Escape' && chat.streaming && !draft.value.trim()) {
    event.preventDefault()
    void stopTurn()
    return
  }

  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault()
    void submit()
  }
}

const canSend = computed(
  () => (draft.value.trim().length > 0 || !attachments.isEmpty.value) && !sending.value,
)

async function submit(): Promise<void> {
  if (!canSend.value || !app.hasEngine) return
  const text = draft.value.trim()
  // 只有 `/xxx` 这种完整命令时，回车应当是「执行它」而不是把它当成一句话发出去。
  if (!attachments.isEmpty.value && text.startsWith('/') && !text.includes(' ')) {
    const exact = slashItems.value.find((item) => `/${item.name}` === text)
    if (exact) {
      onPickItem(exact)
      return
    }
  }
  sending.value = true
  attachError.value = ''
  try {
    const payload = attachments.payloads()
    if (chat.streaming) {
      await chat.steer(text, payload)
      chat.pushNotice('info', t('chat.queueSteer'))
    } else {
      await chat.send(text, payload)
    }
    draft.value = ''
    attachments.clear()
    draftStore.flush()
    await nextTick(autosize)
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  } finally {
    sending.value = false
  }
}

async function queue(): Promise<void> {
  if (!canSend.value || !chat.streaming) return
  queueing.value = true
  try {
    await chat.followUp(draft.value.trim(), attachments.payloads())
    draft.value = ''
    attachments.clear()
    draftStore.flush()
    await nextTick(autosize)
    chat.pushNotice('info', t('chat.queueAdded'))
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  } finally {
    queueing.value = false
  }
}

async function stopTurn(): Promise<void> {
  stopping.value = true
  try {
    await chat.stop()
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  } finally {
    stopping.value = false
  }
}

const actions = useComposerActions()

// 切引擎的 loading 单独挂在这一层：用户点的是状态条上的那颗按钮，
// 而 setEngine 是一次真往返（工程师是瞬时的，后端那一跳还在）。
async function pickEngine(kind: EngineKind): Promise<void> {
  if (kind === chat.kind) return
  pickingEngine.value = true
  try {
    await actions.pickEngine(kind)
  } finally {
    pickingEngine.value = false
  }
}

const hint = computed(() => {
  if (!app.hasEngine) return t('chat.noEngine')
  if (chat.streaming) return t('chat.placeholderBusy')
  return t('chat.placeholder')
})

/** 当前模型收不收图片：收才让用户粘图，否则那条提示会一直失败。 */
const imagesAllowed = computed(() => {
  const input = chat.model?.input
  if (!input || input.length === 0) return true
  return input.includes('image')
})

onMounted(() => void nextTick(autosize))

function focus(): void {
  textarea.value?.focus()
}

defineExpose({ focus })
</script>

<template>
  <div class="composer">
    <ComposerStatus
      :picking-engine="pickingEngine"
      :attach-error="attachError"
      @pick-engine="pickEngine"
      @clear-attach-error="attachError = ''"
    />

    <!-- 菜单的定位上下文：菜单贴着输入框上沿往上弹，必须相对「输入框那一块」定位。 -->
    <div class="composer-inner">
      <div class="composer-box">
        <!-- 附件占独立一行：超过两行就在区内滚动，不把输入框顶出屏 -->
        <AttachStrip :items="attachments.items.value" @remove="attachments.remove" />

        <!-- 输入块：撑满高度，点击空白区即可落光标 -->
        <div class="ta-shell" @click="focus()">
          <textarea
            ref="textarea"
            v-model="draft"
            class="ta"
            :placeholder="hint"
            :disabled="!app.hasEngine"
            rows="2"
            @input="autosize"
            @keydown="onKeydown"
            @paste="onPaste"
          />
        </div>

        <!-- 工具行：与输入块用一条分隔线切开，不再抢输入框的横向空间 -->
        <ComposerBar
          class="is-divided"
          :can-send="canSend"
          :sending="sending"
          :queueing="queueing"
          :stopping="stopping"
          :busy-cmd="busyCmd"
          :images-allowed="imagesAllowed"
          :count="draft.length"
          @new-chat="actions.startNewChat"
          @browse-files="browseFiles"
          @pick-model="actions.pickModel"
          @pick-thinking="actions.pickThinking"
          @queue="queue"
          @toggle-compact="actions.pickAutoCompaction"
          @stop="stopTurn"
          @submit="submit"
        />
      </div>

      <ComposerMenu
        v-if="slashOpen"
        kind="slash"
        :items="slashItems"
        :index="menuIndex"
        @hover="menuIndex = $event"
        @pick="onPickItem"
        @close="composerMenu.close()"
      />
      <ComposerMenu
        v-else-if="mentionOpen"
        kind="mention"
        :items="[]"
        :index="0"
        @pick-file="onPickFile"
        @close="composerMenu.close()"
      />
    </div>
  </div>
</template>

<style scoped>
/* 输入区根：它自己在页面底部，菜单要从它内部往上弹。 */
.composer {
  flex: none;
  padding: var(--omp-sp-3) var(--omp-sp-5) var(--omp-sp-4);
}

.composer-inner {
  position: relative;
  width: 100%;
  max-width: var(--omp-col);
  margin: 0 auto;
}

/* 输入框容器：只管外壳，三块内容各自管自己的排布。 */
.composer-box {
  display: flex;
  flex-direction: column;
  padding: var(--omp-sp-1);
  border: 1px solid var(--omp-line-strong);
  border-radius: var(--omp-r-lg);
  background: var(--omp-surface);
  transition: border-color var(--omp-dur-fast) var(--omp-ease);
}

.composer-box:focus-within {
  border-color: var(--omp-brand-2);
}

/* 输入块：给一个 68px 的最小高度，空态也有一个大目标可点。
   之前 textarea 只有 1 行高，整块输入区看着就是一条缝。 */
.ta-shell {
  display: flex;
  min-height: calc(68px * var(--omp-vw-scale));
  padding: var(--omp-sp-1);
  cursor: text;
}

.ta {
  flex: 1;
  min-width: 0;
  border: 0;
  outline: none;
  resize: none;
  background: transparent;
  color: var(--omp-ink);
  font-family: inherit;
  font-size: var(--omp-fs-md);
  line-height: var(--omp-lh-base);
  padding: var(--omp-sp-2) var(--omp-sp-3);
  max-height: calc(240px * var(--omp-vw-scale));
}

.ta:disabled {
  cursor: not-allowed;
}

.ta::placeholder {
  color: var(--omp-faint);
}

/* 工具行与输入块用一条分隔线切开 */
.is-divided {
  margin-top: var(--omp-sp-1);
  padding-top: var(--omp-sp-2);
  border-top: 1px solid var(--omp-line);
}
</style>
