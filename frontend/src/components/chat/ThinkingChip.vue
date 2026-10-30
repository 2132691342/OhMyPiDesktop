<script setup lang="ts">
// 思考档芯片：切推理强度。
//
// 它是一个读数 + 一个入口：档位名常驻在输入框底栏（用户随时看得见
// 这一轮会想多久），点它才展开档位列表。
//
// 档位清单由引擎给出（caps 决定引擎有没有这个概念），没有就整个不渲染。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import { useChatStore } from '@/stores/chat'
import { useToasts } from '@/composables/useToasts'

const { t } = useI18n()
const chat = useChatStore()
const toasts = useToasts()

const open = ref(false)
const applying = ref('')
const root = ref<HTMLElement | null>(null)

/** 引擎给出的档位清单。空数组表示这个引擎没有思考档这个概念。 */
const levels = computed(() => chat.levels)

/** 当前档位的显示名：引擎给的是原始值，界面按 i18n 表翻。 */
const label = computed(() => {
  const level = chat.thinkingLevel
  if (!level) return t('chat.thinkingDefault')
  const key = `thinking.${level}`
  const translated = t(key)
  return translated === key ? level : translated
})

async function choose(level: string): Promise<void> {
  if (applying.value || level === chat.thinkingLevel) {
    open.value = false
    return
  }
  applying.value = level
  try {
    await chat.setThinking(level)
    open.value = false
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    applying.value = ''
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

// `/thinking` 这类内置命令要求打开面板：面板自己 watch 这个意图位。
watch(
  () => chat.pickerRequest,
  (want) => {
    if (want !== 'thinking') return
    chat.pickerRequest = ''
    open.value = true
  },
)
</script>

<template>
  <div v-if="levels.length" ref="root" class="tc">
    <button
      class="tc-chip"
      :class="{ 'is-on': open }"
      type="button"
      :title="t('chat.pickThinking')"
      @click="open = !open"
    >
      <AppIcon name="brain" :size="13" />
      <span class="tc-t">{{ label }}</span>
      <AppIcon name="chevron" :size="12" class="caret" :class="{ up: open }" />
    </button>

    <Transition name="pop">
      <div v-if="open" class="tc-panel">
        <div class="tc-head">{{ t('chat.pickThinking') }}</div>
        <button
          v-for="level in levels"
          :key="level"
          class="tc-row"
          :class="{ 'is-on': level === chat.thinkingLevel }"
          type="button"
          :disabled="Boolean(applying)"
          @click="choose(level)"
        >
          <span class="tc-name">{{ t(`thinking.${level}`) }}</span>
          <AppIcon v-if="level === chat.thinkingLevel" name="check" :size="12" class="tc-check" />
        </button>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.tc {
  position: relative;
}

.tc-chip {
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
  font-weight: 600;
  transition:
    background-color var(--omp-dur-fast) var(--omp-ease),
    border-color var(--omp-dur-fast) var(--omp-ease),
    color var(--omp-dur-fast) var(--omp-ease),
    transform var(--omp-dur-fast) var(--omp-ease);
}

.tc-chip:hover:not(:disabled) {
  background: var(--omp-hover);
  border-color: var(--omp-line-strong);
  color: var(--omp-ink-2);
  transform: scale(var(--omp-press-hover));
}

.tc-chip:active:not(:disabled) {
  transform: scale(var(--omp-press-active));
  transition-duration: 60ms;
}

.tc-chip.is-on {
  background: var(--omp-brand-soft);
  border-color: var(--omp-brand-3);
  color: var(--omp-brand);
}

.tc-t {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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

.tc-panel {
  position: absolute;
  bottom: calc(100% + var(--omp-sp-2));
  left: 0;
  z-index: var(--omp-z-pop);
  width: 180px;
  padding: var(--omp-sp-2);
  border: 1px solid var(--omp-line-strong);
  border-radius: var(--omp-r-lg);
  background: var(--omp-raised);
  box-shadow: var(--omp-shadow-lift);
}

.tc-head {
  padding: 0 var(--omp-sp-2) var(--omp-sp-2);
  font-size: var(--omp-fs-3xs);
  font-weight: 700;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--omp-faint);
}

.tc-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
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

.tc-row:hover:not(:disabled) {
  background: var(--omp-hover);
}

.tc-row.is-on {
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
  font-weight: 700;
}

.tc-row:disabled {
  opacity: 0.55;
  cursor: progress;
}

.tc-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tc-check {
  flex: none;
  color: var(--omp-brand);
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
