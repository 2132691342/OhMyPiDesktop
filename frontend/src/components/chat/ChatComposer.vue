<script setup lang="ts">
// 输入区：多行输入 + 待发送附件 + @ 文件引用 + / 命令菜单 + 模型与思考档 + 发送/停止。
//
// 菜单有两组来源，界面上合成一张表但分开标注：
//   - 界面内置命令（新建 / 换模型 / 压缩 / 导出…）——这些不依赖引擎；
//   - 引擎报上来的命令（扩展、提示模板、技能）——只有引擎知道有哪些。
// 两组都走同一条「选中即填进输入框」的路，因为引擎那组要靠 prompt 才生效。
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import MentionBrowser from '@/components/chat/MentionBrowser.vue'
import AttachStrip from '@/components/chat/AttachStrip.vue'
import ComposerBar from '@/components/chat/ComposerBar.vue'
import ComposerStatus from '@/components/chat/ComposerStatus.vue'
import { useChatStore } from '@/stores/chat'
import { useAppStore } from '@/stores/app'
import { useComposerDraft } from '@/composables/useComposerDraft'
import { useAttachments, baseName, toWorkspaceRelative } from '@/composables/useAttachments'
import { useComposerMenu } from '@/composables/useComposerMenu'
import { pickFiles } from '@/api/wails'
import type { EngineKind } from '@/types/api'

const chat = useChatStore()
const app = useAppStore()
const { t } = useI18n()

const draft = ref('')
const pickingEngine = ref(false)
const textarea = ref<HTMLTextAreaElement | null>(null)
const sending = ref(false)
const queueing = ref(false)

// 草稿按会话存：切走再切回来还在，关掉窗口也不丢。
const draftStore = useComposerDraft(computed(() => chat.sessionFile), draft)

// 附件：粘贴图片 / 回形针选文件 / @ 引用工作区文件，三种来源一个列表。
const attachments = useAttachments()
const attachError = ref('')

// 菜单：/ 命令 与 @ 引用。开关时机与选中后的动作都在 composable 里。
const composerMenu = useComposerMenu(draft, (name) => {
  draft.value = `/${name} `
  void nextTick(() => textarea.value?.focus())
})
const { menuIndex, busyCmd, mentionOpen, slashOpen, slashItems, applyItem } = composerMenu

// ── @ 引用 ──
function pickMention(relPath: string, name: string): void {
  const err = attachments.addWorkspaceFile(relPath, name)
  if (err) {
    attachError.value = err
    return
  }
  // 把触发菜单的那段 @xxx 从输入里去掉：引用已经变成附件条目了。
  draft.value = draft.value.replace(/@\S*$/, '').trimEnd()
  composerMenu.close()
  void nextTick(() => textarea.value?.focus())
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
      if (item) applyItem(item)
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
      applyItem(exact)
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

const stopping = ref(false)
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

async function startNewChat(): Promise<void> {
  try {
    await chat.newChat()
    chat.pushNotice('info', t('chat.newChatStarted'))
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  }
}

// 切引擎：只换「这句话发给谁」。不切目录、不切会话。
async function pickEngine(kind: EngineKind): Promise<void> {
  if (kind === chat.kind) return
  pickingEngine.value = true
  try {
    await app.setEngine(kind)
    chat.pushNotice('info', t('chat.engineSwitched', { engine: kind }))
    await chat.loadModels()
    await chat.refresh('')
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  } finally {
    pickingEngine.value = false
  }
}

async function pickModel(provider: string, id: string): Promise<void> {
  try {
    await chat.setModel(provider, id)
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  }
}

async function pickThinking(level: string): Promise<void> {
  try {
    await chat.setThinking(level)
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  }
}

async function pickAutoCompactionToolbar(enabled: boolean): Promise<void> {
  try {
    await chat.setAutoCompaction(enabled)
    chat.pushNotice('info', enabled ? t('chat.autoCompactOn') : t('chat.autoCompactOff'))
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
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

    <div class="composer-inner">
      <div class="composer-box">
        <AttachStrip :items="attachments.items.value" @remove="attachments.remove" />

        <textarea
          ref="textarea"
          v-model="draft"
          :placeholder="hint"
          :disabled="!app.hasEngine"
          rows="1"
          @input="autosize"
          @keydown="onKeydown"
          @paste="onPaste"
        />

        <ComposerBar
          :can-send="canSend"
          :sending="sending"
          :queueing="queueing"
          :stopping="stopping"
          :busy-cmd="busyCmd"
          :images-allowed="imagesAllowed"
          :count="draft.length"
          @new-chat="startNewChat"
          @browse-files="browseFiles"
          @pick-model="pickModel"
          @pick-thinking="pickThinking"
          @queue="queue"
          @toggle-compact="pickAutoCompactionToolbar"
          @stop="stopTurn"
          @submit="submit"
        />
      </div>

      <!-- / 命令菜单：贴着输入框上沿，不是跟着输入框左侧 -->
      <div v-if="slashOpen" class="pop menu-pop" @pointerdown.prevent>
        <div class="pop-list">
          <button
            v-for="(item, index) in slashItems"
            :key="item.source + item.key"
            type="button"
            class="pop-row"
            :class="{ 'is-hi': index === menuIndex }"
            @mouseenter="menuIndex = index"
            @click="applyItem(item)"
          >
            <span class="pop-grow">
              <b class="pop-t">/{{ item.name }}</b>
              <span class="pop-d">{{ item.desc }}</span>
            </span>
            <span v-if="item.source === 'builtin'" class="pop-tag">{{ t('chat.slashBuiltin') }}</span>
          </button>
        </div>
        <p class="pop-hint">{{ t('chat.slashHint') }}</p>
      </div>

      <!-- @ 引用：工作区文件逐级浏览 -->
      <div v-else-if="mentionOpen" class="pop menu-pop is-mention" @pointerdown.prevent>
        <MentionBrowser @pick="pickMention" />
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 输入区根：它自己在页面底部，菜单要从它内部往上弹。
   这里不需要 position —— composer-inner 才是定位上下文，
   把定位放在根上会让菜单相对整块（含状态条）定位，往上弹时盖住更多东西。 */
.composer {
  flex: none;
  padding: var(--omp-sp-3) var(--omp-sp-5) var(--omp-sp-4);
}

/* 菜单的定位上下文：菜单贴着输入框上沿往上弹，必须相对「输入框那一块」定位。
   少了这一行 position，绝对定位的子元素会往上找第一个已定位的祖先，
   菜单就跑到左上角（或者整页的某个角落）去了。 */
.composer-inner {
  position: relative;
  width: 100%;
  max-width: var(--omp-col);
  margin: 0 auto;
}

/* 菜单面板：贴着输入框上沿往上弹，左右各留一点边距。
   bottom: calc(100% - sp-2) 让它压在输入框边框上 2px，视觉上像从框里长出来的。
   不用 el-popover：那是给「相对某个小控件弹出」用的，这里要的是
   「贴着整块输入区、宽度跟着走」——与输入框同宽的清单更像它的延伸部分。 */
.pop {
  position: absolute;
  left: 0;
  right: 0;
  bottom: calc(100% - var(--omp-sp-2));
  max-height: 340px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: var(--omp-sp-1);
  border: 1px solid var(--omp-line-strong);
  border-radius: var(--omp-r-lg);
  background: var(--omp-surface);
  /* 用 lift 而不是普通 shadow：菜单浮在内容之上，阴影要能说明层级。 */
  box-shadow: var(--omp-shadow-lift);
  animation: pop-in var(--omp-dur-fast) var(--omp-ease);
  z-index: 20;
}

@keyframes pop-in {
  from {
    opacity: 0;
    transform: translateY(4px);
  }
}

/* 滚动在列表层而不是弹层上：键盘高亮要靠它自己的 scrollTop 算可视区 */
.pop-list {
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
}

.pop-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: 5px var(--omp-sp-2);
  border: 0;
  border-radius: var(--omp-r-sm);
  background: transparent;
  color: var(--omp-ink-2);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background-color var(--omp-dur-fast) var(--omp-ease);
}

.pop-row:hover,
.pop-row.is-hi {
  background: var(--omp-brand-soft);
}

.pop-grow {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
  flex: 1;
}

.pop-t {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-xs);
  font-weight: 650;
  color: var(--omp-brand);
}

.pop-d {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-mute);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pop-tag {
  flex: none;
  font-size: var(--omp-fs-3xs);
  padding: 0 5px;
  border-radius: var(--omp-r-full);
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
}

.pop-hint {
  margin: var(--omp-sp-1) 0 0;
  padding: 2px var(--omp-sp-2) 0;
  border-top: 1px solid var(--omp-line);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  font-family: var(--omp-font-mono);
}

/* 输入框容器：工具条与附件条都挂在里面，所以它只管外壳。 */
.composer-box {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border: 1px solid var(--omp-line-strong);
  border-radius: var(--omp-r-lg);
  background: var(--omp-surface);
  transition: border-color var(--omp-dur-fast) var(--omp-ease);
}

.composer-box:focus-within {
  border-color: var(--omp-brand-2);
}
</style>
