<script setup lang="ts">
// 会话列表：对话页的窄栏与会话页的主栏共用。
// 数据来自磁盘上的会话文件，所以终端里聊过的会话这里也看得到。
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import AppIcon from '@/components/common/AppIcon.vue'
import { useAppStore } from '@/stores/app'
import { useToasts } from '@/composables/useToasts'
import { api } from '@/api/http'
import type { SessionRow } from '@/types/api'

const props = withDefaults(defineProps<{ active?: string; compact?: boolean; workspaceOnly?: boolean }>(), {
  active: '',
  compact: false,
  workspaceOnly: false,
})

const emit = defineEmits<{ open: [path: string]; changed: [] }>()

const app = useAppStore()
const toasts = useToasts()
const { t } = useI18n()

// 会话属于某个引擎（由文件所在目录决定），列表跟着当前引擎走。
const kind = computed(() => app.active)

const filter = ref('')
const rows = ref<SessionRow[]>([])
const loading = ref(false)

/** 引擎范围：列表默认只显示当前引擎的会话（两个引擎的会话不能互换接着聊）。 */
const scope = computed(() => (props.workspaceOnly ? app.workspace : ''))

const filtered = computed(() => {
  const query = filter.value.trim().toLowerCase()
  if (!query) return rows.value
  return rows.value.filter((row) =>
    [row.title, row.model, row.cwd].some((field) => (field ?? '').toLowerCase().includes(query)),
  )
})

async function load() {
  loading.value = true
  try {
    const data = await api.sessions(scope.value || undefined, kind.value)
    rows.value = data.sessions ?? []
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

async function remove(row: SessionRow) {
  try {
    await ElMessageBox.confirm(t('sessions.deleteConfirm', { title: row.title }), t('sessions.deleteTitle'), {
      confirmButtonText: t('common.delete'),
      cancelButtonText: t('common.cancel'),
      type: 'warning',
    })
  } catch {
    return
  }
  try {
    await api.deleteSession(row.path, kind.value)
    toasts.push('ok', t('common.saved'))
    await load()
    emit('changed')
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  }
}

async function rename(row: SessionRow) {
  try {
    const result = await ElMessageBox.prompt(t('sessions.renamePlaceholder'), t('sessions.renameTitle'), {
      inputValue: row.title,
      confirmButtonText: t('common.save'),
      cancelButtonText: t('common.cancel'),
    })
    await api.renameSession(row.path, result.value, kind.value)
    await load()
    emit('changed')
  } catch {
    /* 取消或失败：取消不该弹错 */
  }
}

function relative(iso: string): string {
  if (!iso) return ''
  const time = new Date(iso).getTime()
  if (Number.isNaN(time)) return ''
  const diff = Date.now() - time
  const minute = 60_000
  if (diff < minute) return t('history.justNow')
  if (diff < 60 * minute) return t('history.minutesAgo', { n: Math.floor(diff / minute) })
  if (diff < 24 * 60 * minute) return t('history.hoursAgo', { n: Math.floor(diff / (60 * minute)) })
  return t('history.daysAgo', { n: Math.floor(diff / (24 * 60 * minute)) })
}

watch(kind, () => {
  rows.value = []
  void load()
})

onMounted(load)
defineExpose({ load })
</script>

<template>
  <div class="session-list" :class="{ compact }">
    <div class="list-head">
      <input v-model="filter" class="omp-input sm" :placeholder="t('sessions.filterPlaceholder')" />
      <button class="omp-btn icon ghost" :title="t('common.refresh')" :class="{ 'is-loading': loading }" @click="load">
        <AppIcon name="refresh" :size="14" />
      </button>
    </div>

    <div class="list-body">
      <div v-if="!filtered.length && !loading" class="list-empty">
        <p class="empty-title">{{ t('sessions.emptyTitle') }}</p>
        <p class="empty-hint">{{ t('sessions.emptyHint') }}</p>
      </div>

      <div
        v-for="row in filtered"
        :key="row.path"
        class="session-row omp-row-click"
        :class="{ 'is-active': row.path === active }"
        @click="emit('open', row.path)"
      >
        <div class="row-top">
          <span class="row-title">{{ row.title }}</span>
          <span class="row-time">{{ relative(row.updated_at) }}</span>
        </div>
        <div class="row-meta">
          <span v-if="row.model" class="row-model">{{ row.model }}</span>
          <span class="row-dot">·</span>
          <span>{{ row.messages }} {{ t('sessions.messages') }}</span>
          <span v-if="row.cost" class="row-dot">·</span>
          <span v-if="row.cost">${{ row.cost.toFixed(3) }}</span>
        </div>
        <div v-if="!compact" class="row-path">{{ row.cwd }}</div>
        <div class="row-actions">
          <button class="omp-btn text sm" @click.stop="rename(row)">{{ t('sessions.rename') }}</button>
          <button class="omp-btn text sm danger" @click.stop="remove(row)">{{ t('common.delete') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.session-list {
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
}

.list-head {
  display: flex;
  gap: var(--omp-sp-1);
  padding: var(--omp-sp-2);
  flex: none;
}

.list-head .omp-input {
  height: var(--omp-ctl-sm);
  font-size: var(--omp-fs-xs);
}

.list-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 0 var(--omp-sp-2) var(--omp-sp-3);
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.session-row {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: var(--omp-sp-2) var(--omp-sp-3);
  cursor: pointer;
  position: relative;
}

.row-top {
  display: flex;
  align-items: baseline;
  gap: var(--omp-sp-2);
}

.row-title {
  flex: 1;
  min-width: 0;
  font-size: var(--omp-fs-sm);
  font-weight: 550;
  color: var(--omp-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-time {
  flex: none;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.row-meta {
  display: flex;
  align-items: center;
  gap: 4px;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-mute);
  overflow: hidden;
  white-space: nowrap;
}

.row-model {
  max-width: 150px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.row-dot {
  color: var(--omp-faint);
}

.row-path {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-actions {
  display: none;
  gap: var(--omp-sp-1);
}

.session-row:hover .row-actions {
  display: flex;
}

.compact .row-path,
.compact .row-actions {
  display: none !important;
}

.list-empty {
  padding: var(--omp-sp-6) var(--omp-sp-3);
  text-align: center;
}

.empty-title {
  margin: 0 0 4px;
  font-size: var(--omp-fs-sm);
  font-weight: 600;
  color: var(--omp-ink);
}

.empty-hint {
  margin: 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  line-height: 1.6;
}
</style>
