<script setup lang="ts">
// 差异视图：把后端折好的行结构渲染成等宽行。
//
// 比原来多两件事，都是「改一个几百行的文件」时真的会用到的：
//
//  1. 折叠未改动区段。上下文行默认只留 hunk 前后各 3 行，
//     中间那段折叠成一行「展开 N 行」——否则改一行要看完整个文件。
//  2. 并排 / 内联切换。内联（旧行在上、新行在下）适合窄栏，
//     并排适合看「这一行到底改成了什么」。
//
// 只做展示——提交与回滚都交给版本工具，界面不替用户决定不可逆操作。
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import type { DiffLine, GitDiff } from '@/types/api'

const props = defineProps<{
  diff: GitDiff | null
  path?: string
  loading?: boolean
}>()

const { t } = useI18n()

/** 内联 / 并排。默认内联：窄栏才是这个 pane 的常态。 */
const sideBySide = ref(false)

/** 被展开的折叠段编号集合。键是段起始下标。 */
const opened = ref<Set<number>>(new Set())

// 换了文件或换了差异，之前展开的段落编号就没有意义了，全部收起。
watch(
  () => [props.path, props.diff],
  () => {
    opened.value = new Set()
  },
)

/** 上下文保留行数：hunk 前后各留这么多行，多的折起来。 */
const contextLines = 3

interface Row {
  kind: 'line' | 'fold'
  line?: DiffLine
  /** 折叠段：藏了多少行，从第几行开始（用于 key 与展开） */
  count?: number
  at?: number
}

/**
 * 把原始行折成「行 / 折叠段」的序列。
 *
 * 只折连续的上下文行：增删行永远要显示——那正是用户要看的东西。
 * 头尾各留 contextLines 行作为锚点，中间的折起来。
 */
const rows = computed<Row[]>(() => {
  const src = props.diff?.diff_lines ?? []
  const out: Row[] = []
  let run: { start: number; items: DiffLine[] } | null = null

  const flush = (): void => {
    if (!run) return
    const n = run.items.length
    // 短到不值得折：折起来只省一两行，还多一次点击。
    if (n <= contextLines * 2 + 1) {
      for (const item of run.items) out.push({ kind: 'line', line: item })
    } else {
      for (const item of run.items.slice(0, contextLines)) out.push({ kind: 'line', line: item })
      out.push({ kind: 'fold', count: n - contextLines * 2, at: run.start + contextLines })
      for (const item of run.items.slice(n - contextLines)) out.push({ kind: 'line', line: item })
    }
    run = null
  }

  src.forEach((line, i) => {
    if (line.line_kind === 'ctx') {
      if (!run) run = { start: i, items: [] }
      run.items.push(line)
      return
    }
    flush()
    out.push({ kind: 'line', line })
  })
  flush()
  return out
})

const adds = computed(
  () => (props.diff?.diff_lines ?? []).filter((l) => l.line_kind === 'add').length,
)
const dels = computed(
  () => (props.diff?.diff_lines ?? []).filter((l) => l.line_kind === 'del').length,
)
const blank = computed(() => !props.loading && (props.diff?.diff_lines?.length ?? 0) === 0)
const blankText = computed(() => (props.path ? t('workspace.diffEmpty') : t('workspace.noChanges')))

/** 折叠段是不是展开的。 */
function isOpen(at: number): boolean {
  return opened.value.has(at)
}

function toggle(at: number): void {
  const next = new Set(opened.value)
  if (next.has(at)) next.delete(at)
  else next.add(at)
  opened.value = next
}

/** 展开全部 / 全部收起。 */
const allFolds = computed(() => rows.value.filter((r) => r.kind === 'fold'))
const allOpen = computed(() => allFolds.value.every((r) => isOpen(r.at ?? -1)))

function toggleAll(): void {
  if (allOpen.value) {
    opened.value = new Set()
    return
  }
  opened.value = new Set(allFolds.value.map((r) => r.at ?? -1))
}

/**
 * 可见行：把展开的折叠段还原回原始行。
 *
 * 折叠段只记录「藏了多少行」与起始下标，展开时按这两个数把原始行切回来，
 * 不必在 Row 里再存一份副本。
 */
const visible = computed<Row[]>(() =>
  rows.value.flatMap((row) => {
    if (row.kind !== 'fold') return [row]
    if (!isOpen(row.at ?? -1)) return [row]
    const from = row.at ?? 0
    const src = props.diff?.diff_lines ?? []
    return src.slice(from, from + (row.count ?? 0)).map((line) => ({ kind: 'line' as const, line }))
  }),
)

function rowClass(line: DiffLine): string {
  if (line.line_kind === 'add') return 'is-add'
  if (line.line_kind === 'del') return 'is-del'
  if (line.line_kind === 'hunk') return 'is-hunk'
  if (line.line_kind === 'file') return 'is-file'
  return ''
}
</script>

<template>
  <section class="diff-card">
    <header class="dc-head">
      <span class="dc-path" :title="path">{{ path || t('workspace.changes') }}</span>
      <span v-if="adds || dels" class="dc-stat">
        <b class="is-add">+{{ adds }}</b>
        <b class="is-del">−{{ dels }}</b>
      </span>
      <button
        v-if="allFolds.length"
        class="omp-btn text sm"
        :title="allOpen ? t('workspace.foldAll') : t('workspace.unfoldAll')"
        @click="toggleAll"
      >
        {{ allOpen ? t('workspace.foldAll') : t('workspace.unfoldAll') }}
      </button>
      <button
        class="omp-btn text sm"
        :title="t('workspace.diffMode')"
        @click="sideBySide = !sideBySide"
      >
        {{ sideBySide ? t('workspace.diffInline') : t('workspace.diffSide') }}
      </button>
    </header>

    <div v-if="loading" class="dc-state">
      <span class="dot busy" />
      {{ t('common.loading') }}
    </div>

    <div v-else-if="blank" class="dc-state">{{ blankText }}</div>

    <div v-else class="diff dc-body" :class="{ 'is-side': sideBySide }">
      <template v-for="(row, i) in visible" :key="`${row.kind}-${row.at ?? i}`">
        <!-- 折叠段：一行说明藏了多少行，点一下展开 -->
        <button v-if="row.kind === 'fold'" class="fold-row" @click="toggle(row.at ?? -1)">
          <AppIcon name="chevron" :size="11" class="fold-ic" />
          {{ t('workspace.foldedLines', { n: row.count }) }}
        </button>
        <div v-else-if="row.line" class="diff-row" :class="rowClass(row.line)">
          <span class="ln">{{ row.line.old_no || '' }}</span>
          <span class="ln">{{ row.line.new_no || '' }}</span>
          <span class="tx">{{ row.line.line_text }}</span>
        </div>
      </template>
    </div>

    <footer v-if="diff?.truncated" class="dc-trunc">
      {{ t('workspace.diffTruncated', { n: diff.dropped }) }}
    </footer>
  </section>
</template>

<style scoped>
.diff-card {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r-lg);
  background: var(--omp-surface);
}
.dc-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border-bottom: 1px solid var(--omp-line);
  flex: none;
}
.dc-path {
  flex: 1;
  min-width: 0;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-xs);
  color: var(--omp-mute);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.dc-stat {
  display: flex;
  gap: var(--omp-sp-2);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-2xs);
  flex: none;
}
.dc-stat .is-add {
  color: var(--omp-ok);
}
.dc-stat .is-del {
  color: var(--omp-bad);
}
.dc-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-2xs);
  line-height: 1.5;
}
.dc-state {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-10) var(--omp-sp-5);
  color: var(--omp-faint);
  font-size: var(--omp-fs-sm);
}

.diff-row {
  display: grid;
  grid-template-columns: 48px 48px 1fr;
  padding: 0 var(--omp-sp-2);
  white-space: pre;
}

/* 并排：行号列收窄，正文分左右两栏。
   增删行在并排下只占自己那一半，另一半留空——
   这正是并排比内联好读的地方：一眼看出「改成了什么」。 */
.dc-body.is-side .diff-row {
  grid-template-columns: 40px 1fr 40px 1fr;
}
.dc-body.is-side .diff-row.is-add {
  grid-template-columns: 40px 1fr;
}
.dc-body.is-side .diff-row.is-add .ln:first-child {
  display: none;
}

.diff-row .ln {
  color: var(--omp-faint);
  text-align: right;
  padding-right: var(--omp-sp-2);
  user-select: none;
}
.diff-row .tx {
  word-break: break-all;
  white-space: pre-wrap;
}
.diff-row.is-add {
  background: color-mix(in srgb, var(--omp-ok) 10%, transparent);
}
.diff-row.is-add .tx {
  color: var(--omp-ink);
}
.diff-row.is-del {
  background: color-mix(in srgb, var(--omp-bad) 10%, transparent);
}
.diff-row.is-del .tx {
  color: var(--omp-ink);
}
.diff-row.is-hunk {
  color: var(--omp-brand);
  background: var(--omp-hover);
}
.diff-row.is-file {
  color: var(--omp-faint);
}

/* 折叠段：一条可点的横条，说明藏了多少行 */
.fold-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: 2px var(--omp-sp-3);
  border: 0;
  border-top: 1px solid var(--omp-line);
  border-bottom: 1px solid var(--omp-line);
  background: var(--omp-hover);
  color: var(--omp-mute);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition:
    background-color var(--omp-dur-fast) var(--omp-ease),
    color var(--omp-dur-fast) var(--omp-ease);
}
.fold-row:hover {
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
}
.fold-ic {
  transform: rotate(90deg);
  flex: none;
}

.dc-trunc {
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border-top: 1px solid var(--omp-line);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-warn);
  flex: none;
}
</style>
