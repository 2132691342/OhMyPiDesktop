<script setup lang="ts">
// 工具卡：一行摘要 + 展开后的参数 / 输出 / 差异。
//
// 三条纪律：
//  1. 默认收起输出——工具输出可能几千行，展开是用户的选择，不是默认代价。
//  2. 差异优先于输出展示。改文件工具的输出是一句人话
//     （"Successfully replaced 3 blocks"），真正要看的是改了哪几行。
//  3. 头是**一行**：状态 + 工具名 + 摘要 + 增删 + 耗时。任何一段被挤压时
//     截断的是摘要，不是工具名（工具名挤成竖排单字是这里最容易犯的错）。
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import DiffRows from '@/components/common/DiffRows.vue'
import { openFilePeek } from '@/composables/useFilePeek'
import type { ChatBlock, DiffLine } from '@/types/api'

const props = defineProps<{ block: ChatBlock }>()
const { t } = useI18n()

const open = ref(false)
watch(
  () => props.block.status,
  (status, prev) => {
    if (prev === 'running' && status === 'error') open.value = true
    // 差异到货就把卡片展开：用户最想看的就是「它改了什么」，
    // 而展开这一步由他们自己点会漏掉。
    if (prev !== 'done' && status === 'done' && props.block.diff?.length) open.value = true
  },
)

const status = computed(() => props.block.status ?? 'pending')
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

/** 差异行：引擎改了什么。没有差异的工具（读文件、跑命令）没有这一段。 */
const diffLines = computed<DiffLine[]>(() => props.block.diff ?? [])

/** 差异的增删行数。 */
const diffStat = computed(() => {
  let adds = 0
  let dels = 0
  for (const line of diffLines.value) {
    if (line.line_kind === 'add') adds++
    else if (line.line_kind === 'del') dels++
  }
  return { adds, dels }
})

/** 被改动的文件：只取末段，路径通常很长。 */
const fileName = computed(() => {
  const path = props.block.file_path ?? ''
  if (!path) return ''
  const parts = path.split(/[\\/]/).filter(Boolean)
  return parts.length ? parts[parts.length - 1] : path
})

/**
 * 有没有「这个文件被改过」的证据：引擎给的 diff，或它自己报的文件路径。
 *
 * 只认 diff 是不够的：写入 / 补丁类工具在引擎那边不一定带 diff 帧，
 * 那时界面上完全没有差异入口，用户只能去工作区页自己找。
 */
const touched = computed(() => Boolean(props.block.file_path) || diffLines.value.length > 0)

/**
 * 能不能「看工作区里的真实差异」。
 *
 * 只要知道文件路径就行。之前的判据还要求文件落在当前工作区内，
 * 而那把最常见的情形挡掉了：引擎常在别的项目目录里改东西。
 */
const canPeek = computed(() => touched.value)

/** 摘要那一行显示什么：优先文件名（用户认文件，不认命令）。 */
const summaryText = computed(() => fileName.value || headline.value)
</script>

<template>
  <div class="tool" :class="[status, { 'is-open': open }]">
    <button class="tool-head" type="button" :aria-expanded="open" @click="open = !open">
      <span class="tool-dot" :class="status" :title="label">
        <AppIcon v-if="status === 'running'" name="refresh" :size="11" class="spin" />
        <AppIcon v-else-if="status === 'error'" name="close" :size="11" />
        <AppIcon v-else-if="status === 'done'" name="check" :size="11" />
        <span v-else class="idle-dot" />
      </span>

      <span class="tool-name">{{ block.tool_name || 'tool' }}</span>

      <span class="tool-summary" :title="summaryText">{{ summaryText }}</span>

      <span v-if="diffLines.length" class="tool-stat">
        <b class="is-add">+{{ diffStat.adds }}</b>
        <b class="is-del">−{{ diffStat.dels }}</b>
      </span>

      <span v-if="duration" class="tool-time">{{ duration }}</span>

      <AppIcon name="chevron" :size="11" class="tool-chevron" :class="{ open }" />
    </button>

    <!-- 文件类工具：不必展开就能就地看现在的真实差异 -->
    <div v-if="canPeek" class="tool-acts">
      <button class="tool-peek" type="button" :title="block.file_path" @click.stop="openFilePeek(block.file_path ?? '', fileName)">
        <AppIcon name="git" :size="11" />
        {{ t('chat.peekInWorkspace') }}
      </button>
    </div>

    <div v-if="open" class="tool-body">
      <!-- 差异先于输出：改文件工具的输出只是一句人话。 -->
      <div v-if="touched">
        <div class="tool-label">{{ t('chat.toolChanges') }}</div>
        <div v-if="diffLines.length" class="tool-diff">
          <DiffRows :lines="diffLines" />
        </div>
        <p v-else class="tool-nodiff">{{ t('chat.toolNoInlineDiff') }}</p>
      </div>
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
      <div v-if="!args && !output && !diffLines.length" class="tool-label">{{ t('common.empty') }}</div>
    </div>
  </div>
</template>

<style scoped>
/* 头是严格一行：除摘要以外每一段都 flex: none，被挤的只能是摘要。
   之前工具名没有 flex: none，长名字（powershell）会被压成一字一行竖排。 */
.tool-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: 5px var(--omp-sp-3);
  border: none;
  background: transparent;
  text-align: left;
  color: var(--omp-ink);
  font-size: var(--omp-fs-xs);
  transition: background-color var(--omp-dur-fast) var(--omp-ease);
}
.tool-head:hover {
  background: var(--omp-hover);
}

.tool-dot {
  display: grid;
  place-items: center;
  width: 16px;
  height: 16px;
  flex: none;
  border-radius: var(--omp-r-full);
  background: var(--omp-hover);
  color: var(--omp-mute);
}
.tool-dot.done {
  background: var(--omp-ok-soft);
  color: var(--omp-ok);
}
.tool-dot.error {
  background: var(--omp-bad-soft);
  color: var(--omp-bad);
}
.tool-dot.running {
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
}
.spin {
  animation: tool-spin 900ms linear infinite;
}
@keyframes tool-spin {
  to {
    transform: rotate(360deg);
  }
}
.idle-dot {
  width: 5px;
  height: 5px;
  border-radius: var(--omp-r-full);
  background: var(--omp-faint);
}

/* 工具名：等宽、不换行、不参与收缩 */
.tool-name {
  flex: none;
  max-width: 40%;
  font-family: var(--omp-font-mono);
  font-weight: 650;
  font-size: var(--omp-fs-3xs);
  letter-spacing: 0.01em;
  color: var(--omp-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 摘要：唯一允许被挤压的一段 */
.tool-summary {
  flex: 1;
  min-width: 0;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-mute);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 增删统计：跟在摘要后面，是用户扫一眼就想知道的数字 */
.tool-stat {
  display: flex;
  gap: var(--omp-sp-1);
  flex: none;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  font-variant-numeric: tabular-nums;
}
.tool-stat .is-add {
  color: var(--omp-ok);
}
.tool-stat .is-del {
  color: var(--omp-bad);
}

.tool-time {
  flex: none;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  font-variant-numeric: tabular-nums;
}

.tool-chevron {
  flex: none;
  color: var(--omp-faint);
  transition: transform var(--omp-dur-fast) var(--omp-ease);
}
.tool-chevron.open {
  transform: rotate(90deg);
}

/* 「看改动」贴在头的右下：不展开也能点，展开时也在同一位置 */
.tool-acts {
  display: flex;
  justify-content: flex-end;
  padding: 0 var(--omp-sp-3) 3px;
}

.tool-peek {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 1px 6px;
  border: none;
  border-radius: var(--omp-r-xs);
  background: transparent;
  color: var(--omp-brand);
  font-size: var(--omp-fs-3xs);
  font-weight: 650;
  transition:
    background-color var(--omp-dur-fast) var(--omp-ease),
    transform var(--omp-dur-fast) var(--omp-ease);
}
.tool-peek:hover {
  background: var(--omp-brand-soft);
  transform: scale(var(--omp-press-hover));
}
.tool-peek:active {
  transform: scale(var(--omp-press-active));
}

.tool-body {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-2) var(--omp-sp-3) var(--omp-sp-3);
  border-top: 1px solid var(--omp-line);
  animation: tool-unfold var(--omp-dur) var(--omp-ease);
}
@keyframes tool-unfold {
  from {
    opacity: 0;
    transform: translateY(-3px);
  }
}

.tool-label {
  font-size: var(--omp-fs-3xs);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--omp-faint);
  margin-bottom: 3px;
}

.tool-nodiff {
  margin: 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-faint);
  line-height: 1.6;
}

/* 差异表：三列（旧行号 / 新行号 / 正文），与工作区的差异视图同一套列宽。
   行本身的渲染在 DiffRows 里：两处共用一份，就不会有一处行号的列宽不同。 */
.tool-diff {
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r-sm);
  overflow: hidden;
  background: var(--omp-code-bg);
  max-height: 320px;
  overflow-y: auto;
}
</style>
