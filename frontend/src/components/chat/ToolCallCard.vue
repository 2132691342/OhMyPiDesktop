<script setup lang="ts">
// 工具卡：一行摘要 + 展开后的参数 / 输出 / 差异。
//
// 三条纪律：
//  1. 默认收起输出——工具输出可能几千行，展开是用户的选择，不是默认代价。
//  2. 差异优先于输出展示。改文件工具的输出是一句人话
//     （"Successfully replaced 3 blocks"），真正要看的是改了哪几行。
//  3. 块顺序由上层保证，这里不重排。
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
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
 * 之前只有改文件类工具才带 diff，于是「写入 / 编辑」之外那些动了文件的
 * 工具（apply_patch、multiedit…）在界面上完全没有差异入口，
 * 用户只能去工作区页自己找。判据放宽到「有文件路径」——
 * 触到了文件就给一条路，哪怕这份差异是空的。
 */
const touched = computed(() => Boolean(props.block.file_path) || diffLines.value.length > 0)

/**
 * 能不能「看工作区里的真实差异」。
 *
 * 只要知道文件路径就行。
 *
 * 之前的判据还要求文件落在当前工作区内（抽屉会算相对路径，区外算不出来）。
 * 但那把最常见的情形挡掉了：引擎常在别的项目目录里改东西，
 * 那种时候给一个点了报错的按钮确实不如不给——正确的修法不是不给，
 * 而是让抽屉拿绝对路径也能问到差异（git 认绝对路径）。
 */
const canPeek = computed(() => touched.value)
</script>

<template>
  <div class="tool" :class="status">
    <button class="tool-head" type="button" @click="open = !open">
      <span class="dot" :class="statusClass" />
      <span class="tool-name">{{ block.tool_name || 'tool' }}</span>
      <span v-if="fileName" class="tool-file">{{ fileName }}</span>
      <span v-else-if="headline" class="tool-intent">{{ headline }}</span>
      <span v-if="diffLines.length" class="tool-diffstat">
        <b class="is-add">+{{ diffStat.adds }}</b>
        <b class="is-del">−{{ diffStat.dels }}</b>
      </span>
      <span v-if="label" class="tag" :class="status === 'error' ? 'bad' : status === 'done' ? 'ok' : 'brand'">
        {{ label }}
      </span>
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
      <!-- 差异先于输出：改文件工具的输出只是一句人话。
           引擎没给 diff 但报了文件路径时也给这一块：
           那时正文是空的，但「看工作区里的真实差异」这条路仍然通向答案，
           不给就等于告诉用户「这次改动没法看」。 -->
      <div v-if="touched">
        <div class="tool-label-row">
          <span class="tool-label">{{ t('chat.toolChanges') }}</span>
          <!-- 「看工作区」：引擎给的是它当时的改动，
               这里给的是现在的真实差异。两者可能已经不一样了。 -->
          <button
            v-if="canPeek"
            class="omp-btn text sm peek-btn"
            :title="t('chat.peekInWorkspace')"
            @click="openFilePeek(block.file_path ?? '', fileName)"
          >
            {{ t('chat.peekInWorkspace') }}
          </button>
        </div>
        <div v-if="diffLines.length" class="tool-diff">
          <div
            v-for="(line, i) in diffLines"
            :key="i"
            class="diff-row"
            :class="{
              'is-add': line.line_kind === 'add',
              'is-del': line.line_kind === 'del',
              'is-hunk': line.line_kind === 'hunk',
              'is-file': line.line_kind === 'file',
            }"
          >
            <span class="ln">{{ line.old_no || '' }}</span>
            <span class="ln">{{ line.new_no || '' }}</span>
            <span class="tx">{{ line.line_text }}</span>
          </div>
        </div>
        <!-- 引擎没把 diff 塞进这一帧：别画一个空框，
             直接把「去工作区看」这条路指出来（上面那个按钮就是）。 -->
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
.tool-chevron {
  margin-left: auto;
  flex: none;
  color: var(--omp-faint);
  transition: transform var(--omp-dur-fast) var(--omp-ease);
}
.tool-chevron.open {
  transform: rotate(90deg);
}

/* 被改动的文件名：等宽字体，让用户一眼认得出是哪个文件 */
.tool-file {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-mute);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

/* 增删统计：跟在文件名后面，是用户扫一眼就想知道的数字 */
.tool-diffstat {
  display: flex;
  gap: var(--omp-sp-1);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  flex: none;
  font-variant-numeric: tabular-nums;
}
.tool-diffstat .is-add {
  color: var(--omp-ok);
}
.tool-diffstat .is-del {
  color: var(--omp-bad);
}

/* 标签行：左边是标题，右边是动作。
   两者放一行是因为「改动」这个标题本身没有信息量，
   而「看工作区」这个动作才是用户真正会点的。 */
.tool-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--omp-sp-2);
}

.peek-btn {
  color: var(--omp-brand);
}

/* 差异表：三列（旧行号 / 新行号 / 正文），与工作区的差异视图同一套列宽 */
.tool-diff {
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r-sm);
  overflow: hidden;
  background: var(--omp-code-bg);
  max-height: 320px;
  overflow-y: auto;
}
.diff-row {
  display: grid;
  grid-template-columns: 40px 40px 1fr;
  gap: 0;
  padding: 0 var(--omp-sp-2);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  line-height: 1.55;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.diff-row .ln {
  color: var(--omp-faint);
  text-align: right;
  padding-right: var(--omp-sp-2);
  user-select: none;
}
.diff-row.is-add {
  background: color-mix(in srgb, var(--omp-ok) 10%, transparent);
}
.diff-row.is-del {
  background: color-mix(in srgb, var(--omp-bad) 10%, transparent);
}
.diff-row.is-hunk {
  background: var(--omp-hover);
  color: var(--omp-brand);
}
/* 文件头：不是改动，所以不涂成增删色 */
.diff-row.is-file {
  color: var(--omp-faint);
}
</style>
