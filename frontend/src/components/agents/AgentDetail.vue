<script setup lang="ts">
// 子代理详情的读数区：阶段、进度条、六个读数、正文尾部。
//
// 实时数据由一个外部 ticker 驱动（父组件每秒拉一次），而**进度条不该跟着跳**：
// 每 900ms 从 12% 跳到 13% 会被读成「卡住了」。所以这里只呈现已到货的值。
import type { AgentRow } from '@/types/api'

const props = defineProps<{
  detail: AgentRow
  /** 展开态（宽面板）才显示正文尾部 */
  wide: boolean
  /** 正文尾部若干条消息（已抽好纯文本，渲染层不再解结构） */
  transcript: string[]
}>()

function money(v: number): string {
  if (!v) return '$0'
  return v < 0.01 ? `$${v.toFixed(5)}` : `$${v.toFixed(3)}`
}

function tokens(n: number): string {
  if (!n) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(n)
}

function seconds(ms: number): string {
  if (!ms) return '0s'
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m${s % 60}s`
}
</script>

<template>
  <div class="detail">
    <div class="detail-stage">
      {{ props.detail.progress.stage || props.detail.description || props.detail.task }}
    </div>

    <div class="bar">
      <div
        class="bar-fill"
        :style="{ width: `${Math.min(100, Math.max(0, props.detail.progress.completion))}%` }"
      />
    </div>

    <div class="readouts">
      <span>{{ props.detail.progress.completion }}%</span>
      <span v-if="props.detail.progress.current_tool" class="mono">
        {{ props.detail.progress.current_tool }}
      </span>
      <span>{{ money(props.detail.progress.cost) }}</span>
      <span>{{ tokens(props.detail.progress.tokens) }} tok</span>
      <span v-if="props.detail.progress.context_max">
        {{ Math.round((props.detail.progress.context_used / props.detail.progress.context_max) * 100) }}%
      </span>
      <span>{{ seconds(props.detail.progress.duration_ms) }}</span>
    </div>

    <div v-if="props.wide" class="transcript selectable">
      <p v-for="(line, i) in props.transcript" :key="i" class="turn">{{ line }}</p>
    </div>
  </div>
</template>

<style scoped>
.detail {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-2) var(--omp-sp-3) var(--omp-sp-3);
  border-top: 1px solid var(--omp-line);
}

.detail-stage {
  font-size: var(--omp-fs-2xs);
  color: var(--omp-ink-2);
  line-height: 1.55;
  overflow-wrap: anywhere;
}

.bar {
  height: 4px;
  border-radius: var(--omp-r-full);
  background: var(--omp-hover);
  overflow: hidden;
}

.bar-fill {
  height: 100%;
  background: var(--omp-brand-2);
  transition: width var(--omp-dur) var(--omp-ease);
}

.readouts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--omp-sp-2);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-mute);
}

.mono {
  color: var(--omp-brand);
}

/* 正文尾部：只给最后几条，且自己滚。 */
.transcript {
  max-height: 220px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-2);
  border-radius: var(--omp-r-sm);
  background: var(--omp-code-bg);
}

.turn {
  margin: 0;
  font-size: var(--omp-fs-3xs);
  line-height: 1.6;
  color: var(--omp-ink-5);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
