<script setup lang="ts">
// 差异视图：把后端折好的行结构渲染成等宽行（旧 / 新行号 + 正文）。
// 只做展示——提交与回滚都交给版本工具，界面不替用户决定不可逆操作。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GitDiff } from '@/types/api'

const props = defineProps<{
  diff: GitDiff | null
  path?: string
  loading?: boolean
}>()

const { t } = useI18n()

const lines = computed(() => props.diff?.diff_lines ?? [])
const adds = computed(() => lines.value.filter((l) => l.line_kind === 'add').length)
const dels = computed(() => lines.value.filter((l) => l.line_kind === 'del').length)
const blank = computed(() => !props.loading && lines.value.length === 0)
const blankText = computed(() => (props.path ? t('workspace.diffEmpty') : t('workspace.noChanges')))
</script>

<template>
  <section class="diff-card card">
    <header class="dc-head">
      <span class="dc-path" :title="path">{{ path || t('workspace.changes') }}</span>
      <span v-if="lines.length" class="dc-stat">
        <b class="is-add">+{{ adds }}</b>
        <b class="is-del">−{{ dels }}</b>
      </span>
    </header>

    <div v-if="loading" class="dc-state">
      <span class="dot busy" />
      {{ t('common.loading') }}
    </div>

    <div v-else-if="blank" class="dc-state">{{ blankText }}</div>

    <div v-else class="diff dc-body">
      <div
        v-for="(l, i) in lines"
        :key="i"
        class="diff-row"
        :class="{ 'is-add': l.line_kind === 'add', 'is-del': l.line_kind === 'del', 'is-hunk': l.line_kind === 'hunk' }"
      >
        <span class="ln">{{ l.old_no || '' }}</span>
        <span class="ln">{{ l.new_no || '' }}</span>
        <span class="tx">{{ l.line_text }}</span>
      </div>
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
  gap: 0;
  padding: 0 var(--omp-sp-2);
  white-space: pre;
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
.dc-trunc {
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border-top: 1px solid var(--omp-line);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-warn);
  flex: none;
}
</style>
