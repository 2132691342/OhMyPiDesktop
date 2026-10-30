<script setup lang="ts">
// 上下文水位：一枚环 + 两个读数，常驻在输入框底栏；点开有详情弹层。
//
// 为什么是环不是进度条：底栏一行里要挤下参数、动作与读数，
// 环只占一个图标的位置，比例却比 64px 的细条更一眼可读。
// 环用 conic-gradient 画：一个元素、不用 SVG，弧长直接拿百分比当停点。
// 平头弧 + 浅轨，避免低占比时被读成 spinner。超过 75% 转警告色，超过 90% 转危险色。
//
// 为什么有详情弹层：底栏只放得下「用了多少」，而「还能聊多少、
// 这一轮为什么快」的回答（剩余、缓存命中、生成速度）要点开才看得到。
// 于是它从读数变成了控件：可点、悬停有反馈。没有数据时整个不渲染
// （空白比 0 更诚实）。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TurnUsage } from '@/composables/useMessageFlow'
import { useChatStore } from '@/stores/chat'

const { t } = useI18n()
const chat = useChatStore()

const open = ref(false)
const root = ref<HTMLElement | null>(null)

const percent = computed(() => Math.min(1, Math.max(0, chat.contextPercent)))
const shownPercent = computed(() => Math.round(percent.value * 100))
const level = computed(() => {
  if (percent.value >= 0.9) return 'bad'
  if (percent.value >= 0.75) return 'warn'
  return 'ok'
})

const tokens = computed(() => formatTokens(chat.context?.tokens ?? 0))
const windowTok = computed(() => formatTokens(chat.context?.contextWindow ?? 0))
const left = computed(() =>
  formatTokens(Math.max(0, (chat.context?.contextWindow ?? 0) - (chat.context?.tokens ?? 0))),
)

/** 有没有水位数据：引擎还没报上来时整条不渲染（空白比 0 更诚实）。 */
const known = computed(() => Boolean(chat.context && chat.context.contextWindow > 0))

// 缓存命中率取「最近一轮」的用量：直播中取直播轮，闲时取上一轮收尾的那次。
const cacheRate = computed(() => {
  const u: TurnUsage | undefined = chat.turn
    ? chat.usageOf(chat.turn.run_id)
    : chat.usageOf(chat.lastFinishedRunId)
  if (!u) return ''
  const prompt = u.input + u.cacheRead + u.cacheWrite
  if (prompt <= 0) return ''
  return `${Math.round((u.cacheRead / prompt) * 100)}%`
})

const speed = computed(() => (chat.tokensPerSecond ? `${Math.round(chat.tokensPerSecond)} tok/s` : ''))
const cost = computed(() => (chat.stats && chat.stats.cost > 0 ? `$${chat.stats.cost.toFixed(3)}` : ''))

const tip = computed(() =>
  known.value
    ? `${t('chat.contextHint', { used: tokens.value, win: windowTok.value })} · ${shownPercent.value}%`
    : t('chat.contextUnknown'),
)

function toggle(): void {
  open.value = !open.value
}

/** 点外面就收起：弹层是临时读数，不占用户的注意力预算。 */
function onDocClick(event: MouseEvent): void {
  if (!open.value) return
  if (root.value && !root.value.contains(event.target as Node)) open.value = false
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') open.value = false
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  window.removeEventListener('keydown', onKeydown)
})

/** token 数按量级压缩：界面上要能一眼比较，不是要精确到个位。 */
function formatTokens(n: number): string {
  if (!n) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 100_000 ? 0 : 1)}k`
  return String(n)
}
</script>

<template>
  <div v-if="known" ref="root" class="meter-wrap">
    <button type="button" class="meter" :class="level" :title="tip" @click="toggle">
      <span class="ring" :style="{ '--ctx-arc': `${shownPercent}%` }" />
      <span class="meter-text">{{ tokens }} / {{ windowTok }}</span>
    </button>

    <!-- 详情弹层：底栏放不下的那几个答案。只列有事实的行，没有就不占位。 -->
    <div v-if="open" class="ctx-panel">
      <div class="ctx-row">
        <span class="ctx-label">{{ t('chat.ctxUsed') }}</span>
        <span class="ctx-value">{{ tokens }} · {{ shownPercent }}%</span>
      </div>
      <div class="ctx-row">
        <span class="ctx-label">{{ t('chat.ctxWindow') }}</span>
        <span class="ctx-value">{{ windowTok }}</span>
      </div>
      <div class="ctx-row">
        <span class="ctx-label">{{ t('chat.ctxLeft') }}</span>
        <span class="ctx-value">{{ left }}</span>
      </div>
      <div v-if="cacheRate" class="ctx-row">
        <span class="ctx-label">{{ t('chat.ctxCache') }}</span>
        <span class="ctx-value">{{ cacheRate }}</span>
      </div>
      <div v-if="speed" class="ctx-row">
        <span class="ctx-label">{{ t('chat.ctxSpeed') }}</span>
        <span class="ctx-value">{{ speed }}</span>
      </div>
      <div v-if="cost" class="ctx-row">
        <span class="ctx-label">{{ t('chat.ctxCost') }}</span>
        <span class="ctx-value">{{ cost }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.meter-wrap {
  position: relative;
  display: inline-flex;
  flex: none;
}

/* 它现在是个控件：可点。但仍然克制——悬停只抬字色与描边，不做缩放胶囊，
   毕竟它一半的身份还是读数。 */
.meter {
  display: inline-flex;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: 2px 6px;
  border: none;
  border-radius: var(--omp-r-sm);
  background: transparent;
  cursor: pointer;
  -webkit-user-select: none;
  user-select: none;
  transition:
    background-color var(--omp-dur-fast) var(--omp-ease),
    transform var(--omp-dur-fast) var(--omp-ease);
}

.meter:hover {
  background: var(--omp-sunken);
}

.meter:active {
  transform: scale(var(--omp-press-active));
}

/* 环：conic-gradient 画弧 + radial mask 挖内圈，一个元素完成。
   弧长直接拿百分比当停点，不用算圆周长。 */
.ring {
  width: 13px;
  height: 13px;
  flex: none;
  border-radius: 50%;
  background: conic-gradient(from -90deg, var(--omp-ok) var(--ctx-arc), var(--omp-line-strong) 0);
  -webkit-mask: radial-gradient(closest-side, #0000 70%, #000 72%);
  mask: radial-gradient(closest-side, #0000 70%, #000 72%);
}

.meter.warn .ring {
  background: conic-gradient(from -90deg, var(--omp-warn) var(--ctx-arc), var(--omp-line-strong) 0);
}

.meter.bad .ring {
  background: conic-gradient(from -90deg, var(--omp-bad) var(--ctx-arc), var(--omp-line-strong) 0);
}

.meter-text {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}

.meter.warn .meter-text {
  color: var(--omp-warn);
}

.meter.bad .meter-text {
  color: var(--omp-bad);
}

/* 详情弹层：贴着底栏往上弹，右对齐（它就在行末） */
.ctx-panel {
  position: absolute;
  bottom: calc(100% + 8px);
  right: 0;
  z-index: var(--omp-z-pop);
  min-width: 200px;
  padding: var(--omp-sp-3);
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  border: 1px solid var(--omp-line-strong);
  border-radius: var(--omp-r-lg);
  background: var(--omp-raised);
  box-shadow: var(--omp-shadow-lift);
}

.ctx-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--omp-sp-4);
}

.ctx-label {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.ctx-value {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-xs);
  color: var(--omp-ink);
  font-variant-numeric: tabular-nums;
}
</style>
