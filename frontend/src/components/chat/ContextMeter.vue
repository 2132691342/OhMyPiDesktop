<script setup lang="ts">
// 上下文水位：一条细进度条 + 两个读数，常驻在输入框底栏。
//
// 为什么不是纯文字：token 数与窗口大小两个数字本身不说明「还能聊多少」，
// 而水位是这一轮会不会突然被压缩的唯一预兆。超过 75% 转警告色，超过 90% 转危险色。
//
// 它是读数不是控件：不套描边胶囊、不做悬停缩放，只换字色。
// 有盒子又没动作等于请用户去点一个点不动的东西。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'

const { t } = useI18n()
const chat = useChatStore()

const percent = computed(() => Math.min(1, Math.max(0, chat.contextPercent)))
const shownPercent = computed(() => Math.round(percent.value * 100))
const level = computed(() => {
  if (percent.value >= 0.9) return 'bad'
  if (percent.value >= 0.75) return 'warn'
  return 'ok'
})

const tokens = computed(() => formatTokens(chat.context?.tokens ?? 0))
const window = computed(() => formatTokens(chat.context?.contextWindow ?? 0))

/** 有没有水位数据：引擎还没报上来时整条不渲染（空白比 0 更诚实）。 */
const known = computed(() => Boolean(chat.context && chat.context.contextWindow > 0))

const tip = computed(() =>
  known.value
    ? `${t('chat.contextHint', { used: tokens.value, win: window.value })} · ${shownPercent.value}%`
    : t('chat.contextUnknown'),
)

/** token 数按量级压缩：界面上要能一眼比较，不是要精确到个位。 */
function formatTokens(n: number): string {
  if (!n) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 100_000 ? 0 : 1)}k`
  return String(n)
}
</script>

<template>
  <div v-if="known" class="meter" :class="level" :title="tip">
    <div class="meter-track">
      <div class="meter-fill" :style="{ width: `${shownPercent}%` }" />
    </div>
    <span class="meter-text">{{ tokens }} / {{ window }}</span>
  </div>
</template>

<style scoped>
.meter {
  display: inline-flex;
  align-items: center;
  gap: var(--omp-sp-2);
  min-width: 0;
  flex: none;
  /* 读数不可选、不画焦点框：拖选正文时扫过它，浏览器会给它套一个淡蓝方块，
     比读数本身显眼得多。 */
  -webkit-user-select: none;
  user-select: none;
}

/* 进度条是读数的一部分：不做悬停缩放（有动作才缩放，读数不冒充控件） */
.meter-track {
  width: 64px;
  height: 4px;
  flex: none;
  border-radius: var(--omp-r-full);
  background: var(--omp-sunken);
  overflow: hidden;
}

.meter-fill {
  height: 100%;
  border-radius: var(--omp-r-full);
  background: var(--omp-ok);
  transition:
    width var(--omp-dur-slow) var(--omp-ease-out),
    background var(--omp-dur-fast) var(--omp-ease);
}

.meter.warn .meter-fill {
  background: var(--omp-warn);
}

.meter.bad .meter-fill {
  background: var(--omp-bad);
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
</style>
