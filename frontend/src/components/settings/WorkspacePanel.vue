<script setup lang="ts">
// 工作区：当前目录 + 最近用过的目录。
//
// 「最近」这份记忆只影响这一份列表——点 × 是从记忆里去掉，
// **不动目录本身**。删别人的目录不该由一个设置页的按钮做。
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import { useAppStore } from '@/stores/app'
import type { WorkspaceRow } from '@/types/api'

const props = defineProps<{
  /** 最近用过的目录（含已不存在的，界面上要标出来） */
  recent: WorkspaceRow[]
  /** 正在「忘记」的目录路径（空表示没有） */
  forgetting: string
}>()

const emit = defineEmits<{
  use: [path: string]
  forget: [path: string]
}>()

const { t } = useI18n()
const app = useAppStore()
</script>

<template>
  <section class="card-pad">
    <div class="omp-section-title">{{ t('settings.groups.workspace') }}</div>
    <div class="set-row">
      <div>
        <div class="set-label">{{ t('settings.workspace') }}</div>
        <div class="set-hint mono selectable">{{ app.workspace }}</div>
      </div>
      <button class="omp-btn" @click="app.chooseWorkspace()">
        <AppIcon name="folder" :size="14" />
        {{ t('chat.pickWorkspace') }}
      </button>
    </div>

    <!-- 最近用过的目录：一行一个，点行即切，× 只从记忆里去掉 -->
    <div v-if="props.recent.length" class="recent-block">
      <div class="set-label">{{ t('settings.recentWorkspaces') }}</div>
      <ul class="recent-list">
        <li v-for="row in props.recent" :key="row.workspace_path" class="recent-row">
          <button
            class="recent-open omp-row-click"
            :class="{ 'is-current': row.workspace_path === app.workspace }"
            :disabled="row.workspace_path === app.workspace || !row.exists"
            :title="row.exists ? row.workspace_path : t('settings.recentMissing')"
            @click="emit('use', row.workspace_path)"
          >
            <AppIcon name="folder" :size="13" />
            <span class="mono recent-path" :class="{ 'is-gone': !row.exists }">
              {{ row.workspace_path }}
            </span>
          </button>
          <button
            class="omp-btn text sm recent-forget"
            :class="{ 'is-loading': props.forgetting === row.workspace_path }"
            :title="t('settings.forgetWorkspace')"
            @click="emit('forget', row.workspace_path)"
          >
            <AppIcon name="close" :size="13" />
          </button>
        </li>
      </ul>
    </div>
    <div v-else class="set-hint">{{ t('settings.recentWorkspacesEmpty') }}</div>
  </section>
</template>

<style scoped>
.set-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--omp-sp-3);
  padding: var(--omp-sp-2) 0;
}

.set-label {
  font-size: var(--omp-fs-sm);
  color: var(--omp-ink);
}

.set-hint {
  margin: 0;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.recent-block {
  margin-top: var(--omp-sp-2);
}

.recent-list {
  list-style: none;
  margin: var(--omp-sp-1) 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.recent-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
}

.recent-open {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex: 1;
  min-width: 0;
  padding: 3px var(--omp-sp-2);
  border: 0;
  background: transparent;
  color: var(--omp-ink-2);
  font-size: var(--omp-fs-xs);
  text-align: left;
  border-radius: var(--omp-r-sm);
  cursor: pointer;
}

.recent-open.is-current {
  color: var(--omp-brand);
  font-weight: 600;
}

.recent-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 目录已经不在了：划掉并禁掉切换——点了也切不过去 */
.recent-path.is-gone {
  text-decoration: line-through;
  color: var(--omp-faint);
}

.recent-forget {
  flex: none;
  opacity: 0;
  transition: opacity var(--omp-dur-fast) var(--omp-ease);
}

.recent-row:hover .recent-forget,
.recent-forget:focus-visible {
  opacity: 1;
}
</style>
