<script setup lang="ts">
// 工具卡：参数 / 输出 / 耗时。运行中状态灯呼吸，失败换色。
// 默认收起输出——工具输出可能几千行，展开是用户的选择，不是默认代价。
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChatBlock } from '@/types/api'

const props = defineProps<{ block: ChatBlock }>()
const { t } = useI18n()

const open = ref(false)
watch(
  () => props.block.status,
  (status, prev) => {
    if (prev === 'running' && status === 'error') open.value = true
  },
)

const status = computed(() => props.block.status ?? 'pending')
const statusClass = computed(() =>
  status.value === 'running' ? 'busy' : status.value === 'error' ? 'bad' : status.value === 'done' ? 'ok' : '',
)
const label = computed(() => {
  if (status.value === 'running') return t('chat.toolRunning')
  if (status.value === 'error') return t('chat.toolFailed')
  if (status.value === 'done') return t('chat.toolDone')
  return ''
})

const args = computed(() => {
  const raw = props.block.args
  if (raw === undefined || raw === null) return ''
  if (typeof raw === 'string') return raw
  try {
    return JSON.stringify(raw, null, 2)
  } catch {
    return String(raw)
  }
})

const headline = computed(() => props.block.intent || summarise(props.block.args))

/** 一行摘要：从参数里挑最像「在做什么」的那个字段。 */
function summarise(raw: unknown): string {
  if (!raw || typeof raw !== 'object') return typeof raw === 'string' ? raw : ''
  const record = raw as Record<string, unknown>
  for (const key of ['command', 'path', 'file_path', 'pattern', 'query', 'url', 'i']) {
    const value = record[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  const first = Object.values(record).find((v) => typeof v === 'string')
  return typeof first === 'string' ? first : ''
}

const duration = computed(() => {
  const ms = props.block.duration_ms
  if (!ms || ms < 0) return ''
  if (ms < 1000) return `${Math.round(ms)}ms`
  return `${(ms / 1000).toFixed(ms < 10000 ? 2 : 1)}s`
})

const output = computed(() => props.block.output ?? '')
</script>

<template>
  <div class="tool" :class="status">
    <button class="tool-head" type="button" @click="open = !open">
      <span class="dot" :class="statusClass" />
      <span class="tool-name">{{ block.tool_name || 'tool' }}</span>
      <span v-if="headline" class="tool-intent">{{ headline }}</span>
      <span v-if="label" class="tag" :class="status === 'error' ? 'bad' : status === 'done' ? 'ok' : 'brand'">{{ label }}</span>
      <span v-if="duration" class="tool-time">{{ duration }}</span>
      <svg
        class="tool-chevron"
        :class="{ open }"
        viewBox="0 0 24 24"
        width="12"
        height="12"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
      >
        <path d="M9 6l6 6-6 6" />
      </svg>
    </button>
    <div v-if="open" class="tool-body">
      <template v-if="args">
        <div>
          <div class="tool-label">{{ t('chat.toolArgs') }}</div>
          <pre class="tool-pre selectable">{{ args }}</pre>
        </div>
      </template>
      <template v-if="output">
        <div>
          <div class="tool-label">{{ t('chat.toolOutput') }}</div>
          <pre class="tool-pre selectable">{{ output }}</pre>
        </div>
      </template>
      <div v-if="!args && !output" class="tool-label">{{ t('common.empty') }}</div>
    </div>
  </div>
</template>

<style scoped>
.tool-chevron {
  margin-left: auto;
  flex: none;
  color: var(--omp-faint);
  transition: transform var(--omp-dur-fast) var(--omp-ease);
}
.tool-chevron.open {
  transform: rotate(90deg);
}
</style>
