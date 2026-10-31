<script setup lang="ts">
// 引擎芯片：在输入框底栏切「这句话发给谁」——它决定这一句话的归宿，
// 属于输入的一部分，放页头会让人以为在切整个应用。
// 只列探测到的引擎；只有一个时不渲染（不占位置）。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import { useAppStore } from '@/stores/app'
import { useChatStore } from '@/stores/chat'
import { useToasts } from '@/composables/useToasts'
import type { EngineKind } from '@/types/api'

const { t } = useI18n()
const app = useAppStore()
const chat = useChatStore()
const toasts = useToasts()

const open = ref(false)
const busy = ref<EngineKind | ''>('')
const root = ref<HTMLElement | null>(null)

/** 只列探测到的引擎。 */
const installed = computed(() => app.installed)

/** 当前引擎的名字（展示用大写标签）。 */
const label = computed(() => chat.kind.toUpperCase())

async function pick(kind: EngineKind): Promise<void> {
  if (busy.value || kind === chat.kind) {
    open.value = false
    return
  }
  // 正在回答时不能换引擎：这一轮会落到原引擎的会话里，上下文就错位了。
  // 不做灰按钮（那像没权限），点下去给一句话说明。
  if (chat.streaming) {
    toasts.push('warn', t('chat.engineLockedWhileRunning'))
    open.value = false
    return
  }
  busy.value = kind
  try {
    // 切引擎：不阻塞在往返上。界面这一侧立刻换，
    // 真正的会话状态由后台补齐（见 chat.switchEngine）。
    await app.setEngine(kind)
    toasts.push('info', t('chat.engineSwitched', { engine: kind }))
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    busy.value = ''
    open.value = false
  }
}
function onDocPointerDown(event: PointerEvent): void {
  if (root.value?.contains(event.target as Node)) return
  open.value = false
}

function onEsc(event: KeyboardEvent): void {
  if (event.key === 'Escape') open.value = false
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocPointerDown)
  document.addEventListener('keydown', onEsc)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointerDown)
  document.removeEventListener('keydown', onEsc)
})
</script>

<template>
  <div v-if="installed.length > 1" ref="root" class="ec">
    <button
      class="ec-chip"
      :class="{ 'is-on': open }"
      type="button"
      :title="t('chat.pickEngine')"
      @click="open = !open"
    >
      <span class="dot" :class="chat.kind === 'omp' ? 'ok' : 'ok'" />
      <span class="ec-t">{{ label }}</span>
      <AppIcon name="chevron" :size="12" class="caret" :class="{ up: open }" />
    </button>

    <Transition name="pop">
      <div v-if="open" class="ec-panel">
        <div class="ec-head">{{ t('chat.pickEngine') }}</div>
        <button
          v-for="item in installed"
          :key="item.kind"
          class="ec-row"
          :class="{ 'is-on': item.kind === chat.kind }"
          type="button"
          :disabled="Boolean(busy)"
          @click="pick(item.kind)"
        >
          <span class="dot" :class="item.kind === chat.kind ? 'ok' : ''" />
          <span class="ec-name">{{ item.kind }}</span>
          <span class="ec-ver mono">{{ item.version || '—' }}</span>
          <AppIcon v-if="item.kind === chat.kind" name="check" :size="12" class="ec-check" />
          <span v-else-if="busy === item.kind" class="ec-spin" />
        </button>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.ec {
  position: relative;
}

.ec-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--omp-sp-1);
  height: var(--omp-ctl-sm);
  padding: 0 var(--omp-sp-2);
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r-sm);
  background: transparent;
  color: var(--omp-mute);
  font-family: inherit;
  font-size: var(--omp-fs-3xs);
  font-weight: 700;
  letter-spacing: 0.04em;
  transition:
    background-color var(--omp-dur-fast) var(--omp-ease),
    border-color var(--omp-dur-fast) var(--omp-ease),
    color var(--omp-dur-fast) var(--omp-ease),
    transform var(--omp-dur-fast) var(--omp-ease);
}

.ec-chip:hover:not(:disabled) {
  background: var(--omp-hover);
  border-color: var(--omp-line-strong);
  color: var(--omp-ink-2);
  transform: scale(var(--omp-press-hover));
}

.ec-chip:active:not(:disabled) {
  transform: scale(var(--omp-press-active));
  transition-duration: 60ms;
}

.ec-chip.is-on {
  background: var(--omp-brand-soft);
  border-color: var(--omp-brand-3);
  color: var(--omp-brand);
}

.ec-t {
  font-family: var(--omp-font-mono);
}

.caret {
  flex: none;
  opacity: 0.55;
  transform: rotate(90deg);
  transition: transform var(--omp-dur-fast) var(--omp-ease);
}

.caret.up {
  transform: rotate(-90deg);
}

.ec-panel {
  position: absolute;
  bottom: calc(100% + var(--omp-sp-2));
  left: 0;
  z-index: var(--omp-z-pop);
  width: 240px;
  padding: var(--omp-sp-2);
  border: 1px solid var(--omp-line-strong);
  border-radius: var(--omp-r-lg);
  background: var(--omp-raised);
  box-shadow: var(--omp-shadow-lift);
}

.ec-head {
  padding: 0 var(--omp-sp-2) var(--omp-sp-2);
  font-size: var(--omp-fs-3xs);
  font-weight: 700;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--omp-faint);
}

.ec-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: var(--omp-sp-2);
  border: 0;
  border-radius: var(--omp-r-sm);
  background: transparent;
  color: var(--omp-ink-2);
  font-family: inherit;
  font-size: var(--omp-fs-2xs);
  text-align: left;
  cursor: pointer;
  transition:
    background-color var(--omp-dur-fast) var(--omp-ease),
    color var(--omp-dur-fast) var(--omp-ease);
}

.ec-row:hover:not(:disabled) {
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
}

.ec-row.is-on {
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
  font-weight: 700;
}

.ec-row:disabled {
  opacity: 0.55;
  cursor: progress;
}

.ec-name {
  flex: 1;
  min-width: 0;
  font-family: var(--omp-font-mono);
}

.ec-ver {
  flex: none;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.ec-check {
  flex: none;
  color: var(--omp-brand);
}

.ec-spin {
  width: 11px;
  height: 11px;
  flex: none;
  border: 1.6px solid var(--omp-line-strong);
  border-top-color: var(--omp-brand);
  border-radius: 50%;
  animation: omp-spin 700ms linear infinite;
}

.pop-enter-active,
.pop-leave-active {
  transition:
    opacity var(--omp-dur-fast) var(--omp-ease),
    transform var(--omp-dur-fast) var(--omp-ease);
}

.pop-enter-from,
.pop-leave-to {
  opacity: 0;
  transform: translateY(4px);
}
</style>
