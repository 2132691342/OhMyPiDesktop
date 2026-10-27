<script setup lang="ts">
// 输入区：Enter 发送；正在回答时 Enter 变插话、Shift+Enter 排队。
// 斜杠菜单只列引擎报上来的命令——内置动作（压缩/新建）另行做成按钮，不混进同一张表。
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import { useChatStore } from '@/stores/chat'
import { useAppStore } from '@/stores/app'
import type { EngineKind, SlashCommand } from '@/types/api'

const chat = useChatStore()
const app = useAppStore()
const { t } = useI18n()

const draft = ref('')
const pickingEngine = ref(false)
const textarea = ref<HTMLTextAreaElement | null>(null)
const sending = ref(false)
const queueing = ref(false)
const slashIndex = ref(0)

const slashOpen = computed(() => draft.value.startsWith('/') && !draft.value.includes(' ') && commands.value.length > 0)

const commands = computed<SlashCommand[]>(() => {
  const query = draft.value.slice(1).toLowerCase()
  if (!query) return chat.commands.slice(0, 40)
  return chat.commands.filter((c) => c.name.toLowerCase().includes(query)).slice(0, 40)
})

watch(slashOpen, (open) => {
  if (open) slashIndex.value = 0
})

// 引擎要求回填输入框（set_editor_text）：只消费一次，用完就清，
// 否则用户手改之后又会被下一次刷新覆盖。
watch(
  () => chat.draftOverride,
  (text) => {
    if (!text) return
    draft.value = text
    chat.draftOverride = ''
    void nextTick(() => {
      autosize()
      textarea.value?.focus()
    })
  },
)

function applySlash(command: SlashCommand) {
  draft.value = `/${command.name} `
  void nextTick(() => textarea.value?.focus())
}

function autosize() {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${Math.min(260, el.scrollHeight)}px`
}

async function submit() {
  const text = draft.value.trim()
  if (!text || sending.value) return
  if (text.startsWith('/') && !text.includes(' ')) {
    const exact = chat.commands.find((c) => `/${c.name}` === text)
    if (exact) {
      applySlash(exact)
      return
    }
  }
  sending.value = true
  try {
    if (chat.streaming) {
      await chat.steer(text)
      chat.pushNotice('info', t('chat.queueSteer'))
    } else {
      await chat.send(text)
    }
    draft.value = ''
    await nextTick(autosize)
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  } finally {
    sending.value = false
  }
}

async function queue() {
  const text = draft.value.trim()
  if (!text || !chat.streaming) return
  queueing.value = true
  try {
    await chat.followUp(text)
    draft.value = ''
    await nextTick(autosize)
    chat.pushNotice('info', t('chat.queueAdded'))
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  } finally {
    queueing.value = false
  }
}

const stopping = ref(false)
async function stopTurn() {
  stopping.value = true
  try {
    await chat.stop()
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  } finally {
    stopping.value = false
  }
}

async function startNewChat() {
  try {
    await chat.newChat()
    chat.pushNotice('info', t('chat.newChatStarted'))
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  }
}

// 切引擎：只换「这句话发给谁」。不切目录、不切会话。
async function pickEngine(kind: EngineKind) {
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

async function clearQueued() {
  try {
    await chat.clearQueue()
    chat.pushNotice('info', t('chat.queueCleared'))
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  }
}

async function pickModel(provider: string, id: string) {
  try {
    await chat.setModel(provider, id)
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  }
}

async function pickThinking(level: string) {
  try {
    await chat.setThinking(level)
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  }
}

async function pickAutoCompactionToolbar(enabled: boolean) {
  try {
    await chat.setAutoCompaction(enabled)
    chat.pushNotice('info', enabled ? t('chat.autoCompactOn') : t('chat.autoCompactOff'))
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  }
}

function onKeydown(event: KeyboardEvent) {
  if (slashOpen.value) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      slashIndex.value = (slashIndex.value + 1) % commands.value.length
      return
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      slashIndex.value = (slashIndex.value - 1 + commands.value.length) % commands.value.length
      return
    }
    if (event.key === 'Enter' || event.key === 'Tab') {
      event.preventDefault()
      const command = commands.value[slashIndex.value]
      if (command) applySlash(command)
      return
    }
    if (event.key === 'Escape') {
      draft.value = ''
      return
    }
  }
  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault()
    if (event.altKey && chat.streaming) {
      void queue()
      return
    }
    void submit()
  }
}

const hint = computed(() => {
  if (!app.hasEngine) return t('chat.noEngine')
  if (chat.streaming) return t('chat.placeholderBusy')
  return t('chat.placeholder')
})
</script>

<template>
  <div class="composer">
    <!-- 引擎选择：只列探测到的引擎，只有一个时不显示（不占位置） -->
    <div v-if="app.showEnginePicker" class="engine-strip">
      <div class="seg" role="tablist">
        <button
          v-for="item in app.installed"
          :key="item.kind"
          class="seg-btn"
          :class="{ active: chat.kind === item.kind }"
          :disabled="pickingEngine || Boolean(chat.switching)"
          :title="item.exe"
          @click="pickEngine(item.kind)"
        >
          <span class="dot" :class="item.kind === chat.kind ? 'ok' : ''" />
          {{ item.kind }}
        </button>
      </div>
      <span v-if="pickingEngine" class="engine-switching">
        <span class="dot busy" />
        {{ t('chat.switchingEngine') }}
      </span>
    </div>

    <div v-if="!app.hasEngine" class="no-engine">
      <span class="dot warn" />
      <span>{{ t('chat.noEngine') }}</span>
      <code class="install-cmd selectable">{{ app.installHint(chat.kind) }}</code>
      <button class="omp-btn text sm" :class="{ 'is-loading': app.detecting }" :disabled="app.detecting" @click="app.detectEngines()">
        {{ t('chat.redetect') }}
      </button>
    </div>

    <div v-if="chat.widgets.length || chat.statusText" class="status-strip">
      <span v-if="chat.statusText" class="status-chip">{{ chat.statusText }}</span>
      <span v-for="widget in chat.widgets" :key="widget.key" class="status-chip">
        {{ widget.lines.join(' · ') }}
      </span>
    </div>

    <div v-if="chat.queue.steering.length || chat.queue.followUp.length" class="queue-strip">
      <span class="dot busy" />
      {{ t('chat.queuedCount', { n: chat.queue.steering.length + chat.queue.followUp.length }) }}
      <button
        v-if="chat.caps?.queue_edit"
        class="omp-btn text sm"
        :title="t('chat.clearQueue')"
        @click="clearQueued"
      >
        {{ t('chat.clearQueue') }}
      </button>
    </div>

    <div v-if="chat.retry" class="retry-strip">
      <span class="dot warn" />
      {{ t('chat.retrying') }} {{ chat.retry.attempt }}/{{ chat.retry.max }}
    </div>

    <div class="composer-inner">
      <el-popover v-if="slashOpen" placement="top-start" :width="420" trigger="manual" :visible="true" popper-class="slash-pop">
        <div class="slash">
          <div
            v-for="(command, index) in commands"
            :key="command.name"
            class="slash-item"
            :class="{ active: index === slashIndex }"
            @mouseenter="slashIndex = index"
            @click="applySlash(command)"
          >
            <span class="slash-name">/{{ command.name }}</span>
            <span class="slash-desc">{{ command.description || command.input?.hint || '' }}</span>
          </div>
          <p class="slash-hint">{{ t('chat.slashHint') }}</p>
        </div>
        <template #reference>
          <div class="slash-anchor" />
        </template>
      </el-popover>

      <textarea
        ref="textarea"
        v-model="draft"
        :placeholder="hint"
        :disabled="!app.hasEngine"
        rows="1"
        @input="autosize"
        @keydown="onKeydown"
      />
      <div class="composer-bar">
        <button
          class="omp-btn icon ghost"
          :class="{ 'is-loading': chat.switching === 'new' }"
          :disabled="Boolean(chat.switching)"
          :title="t('chat.newChat')"
          @click="startNewChat"
        >
          <AppIcon name="plus" :size="15" />
        </button>

        <el-dropdown trigger="click" max-height="360px" :disabled="!chat.models.length">
          <button
            class="omp-btn sm ghost"
            :class="{ 'is-loading': chat.switching === 'model' }"
            :disabled="!chat.model || Boolean(chat.switching)"
          >
            <AppIcon name="cpu" :size="13" />
            <span class="model-label">{{ chat.model?.id || t('common.unknown') }}</span>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item
                v-for="m in chat.models"
                :key="`${m.provider}/${m.id}`"
                @click="pickModel(m.provider, m.id)"
              >
                <span class="drop-row">
                  <span class="drop-id">{{ m.id }}</span>
                  <span class="drop-prov">{{ m.provider }}</span>
                  <span v-if="m.provider === chat.model?.provider && m.id === chat.model?.id" class="tag brand">
                    {{ t('chat.current') }}
                  </span>
                </span>
              </el-dropdown-item>
              <el-dropdown-item v-if="!chat.models.length" disabled>
                {{ t('chat.noModels') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>

        <el-dropdown trigger="click" max-height="320px" :disabled="!chat.levels.length">
          <button
            class="omp-btn sm ghost"
            :class="{ 'is-loading': chat.switching === 'thinking' }"
            :disabled="!chat.levels.length || Boolean(chat.switching)"
          >
            <AppIcon name="spark" :size="13" />
            <span class="model-label">{{ chat.thinkingLevel || t('chat.thinkingDefault') }}</span>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item v-for="level in chat.levels" :key="level" @click="pickThinking(level)">
                <span class="drop-row">
                  <span>{{ level }}</span>
                  <span v-if="level === chat.thinkingLevel" class="tag brand">{{ t('chat.current') }}</span>
                </span>
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>

        <button
          v-if="chat.streaming"
          class="omp-btn sm ghost"
          :class="{ 'is-loading': queueing }"
          :disabled="!draft.trim() || queueing"
          :title="t('chat.queueHint')"
          @click="queue"
        >
          <AppIcon name="clock" :size="13" />
          {{ t('chat.queue') }}
        </button>

        <button
          class="omp-btn sm ghost"
          :class="{ on: chat.autoCompaction, 'is-loading': chat.switching === 'compaction' }"
          :disabled="chat.busy || Boolean(chat.switching) || !app.hasEngine"
          :title="t('chat.autoCompact')"
          @click="pickAutoCompactionToolbar(!chat.autoCompaction)"
        >
          <AppIcon name="refresh" :size="13" />
          {{ t('chat.autoCompact') }}
        </button>

        <button
          v-if="chat.streaming"
          class="omp-btn sm"
          :class="{ 'is-loading': stopping }"
          :disabled="stopping"
          :title="t('chat.stop')"
          @click="stopTurn"
        >
          <AppIcon name="stop" :size="13" />
          {{ t('chat.stop') }}
        </button>

        <span class="composer-hint">{{ draft.length ? `${draft.length}` : '' }}</span>
        <button
          class="omp-btn primary sm"
          :class="{ 'is-loading': sending }"
          :disabled="!draft.trim() || sending || !app.hasEngine"
          @click="submit"
        >
          <AppIcon name="send" :size="13" />
          {{ chat.streaming ? t('chat.sendSteer') : t('chat.send') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.engine-strip {
  max-width: var(--omp-col);
  margin: 0 auto var(--omp-sp-2);
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
}

.engine-switching {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
}

.no-engine {
  max-width: var(--omp-col);
  margin: 0 auto var(--omp-sp-2);
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
  flex-wrap: wrap;
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border: 1px solid var(--omp-warn);
  border-radius: var(--omp-r);
  background: var(--omp-warn-soft);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-ink);
}

.install-cmd {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  padding: 2px 6px;
  border-radius: var(--omp-r-xs);
  background: var(--omp-surface);
  border: 1px solid var(--omp-line);
  user-select: all;
}

.queue-strip {
  max-width: var(--omp-col);
  margin: 0 auto var(--omp-sp-2);
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
}

.retry-strip {
  max-width: var(--omp-col);
  margin: 0 auto var(--omp-sp-2);
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-warn);
}

.slash-anchor {
  position: absolute;
  top: 0;
  left: 0;
  width: 1px;
  height: 1px;
  pointer-events: none;
}

.slash-hint {
  margin: var(--omp-sp-2) 0 0;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  font-family: var(--omp-font-mono);
}

.model-label {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--omp-font-mono);
}

.drop-id {
  font-family: var(--omp-font-mono);
}

.drop-prov {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  font-family: var(--omp-font-mono);
}

.drop-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--omp-sp-4);
  min-width: 120px;
}
</style>
