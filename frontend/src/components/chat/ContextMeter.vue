<script setup lang="ts">
// 上下文水位：一条细进度条 + 两个读数。
//
// 为什么不是纯文字：token 数与窗口大小两个数字本身不说明「还能聊多少」，
// 而水位是这一轮会不会突然被压缩的唯一预兆。超过 80% 转成警告色。
import { computed } from 'vue'
import { useChatStore } from '@/stores/chat'

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

/** token 数按量级压缩：界面上要能一眼比较，不是要精确到个位。 */
function formatTokens(n: number): string {
  if (!n) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 100_000 ? 0 : 1)}k`
  return String(n)
}
</script>

<template>
  <div v-if="chat.context" class="meter" :class="level" :title="`${tokens} / ${window}`">
    <div class="meter-track">
      <div class="meter-fill" :style="{ width: `${shownPercent}%` }" />
    </div>
    <span class="meter-text">{{ tokens }} / {{ window }}</span>
  </div>
</template>

<style scoped>
.meter {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  min-width: 0;
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

.meter.bad .meter-text {
  color: var(--omp-bad);
}
</style>
