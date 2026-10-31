<script setup lang="ts">
// 概览页的「最近会话」卡：一行一条，点开继续。
// 「还在核对」必须显示成还没数完，不能是空列表 + 「还没有会话」——
// 那是一句断言，而磁盘上可能躺着一堆会话文件。
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import type { SessionRow } from '@/types/api'

defineProps<{
  rows: SessionRow[]
  /** 这一屏的事实是否已经全部到手；为假时不说「还没有会话」 */
  ready: boolean
}>()

const emit = defineEmits<{
  open: [path: string]
  all: []
  start: []
}>()

const { t } = useI18n()
</script>

<template>
  <section class="omp-card card-pad">
    <div class="omp-sec-head">
      <span class="omp-sec-title">{{ t('overview.recent') }}</span>
      <button class="omp-btn text sm" @click="emit('all')">
        {{ t('overview.all') }}
        <AppIcon name="chevron" :size="12" />
      </button>
    </div>

    <!-- 还没数完：不给空态，只说一句在核对 -->
    <div v-if="!ready && !rows.length" class="recent-pending">
      <span class="dot busy" />
      {{ t('overview.collecting') }}
    </div>

    <div v-else-if="rows.length" class="recent-list">
      <button v-for="row in rows" :key="row.id" class="recent-row omp-row-click" @click="emit('open', row.path)">
        <span class="omp-tile omp-tile--sm omp-tile--neutral">
          <AppIcon name="chat" :size="14" />
        </span>
        <span class="recent-title">{{ row.title || t('common.untitled') }}</span>
        <span class="recent-meta omp-mono">{{ row.model || '—' }}</span>
        <span class="recent-time omp-mono">{{ row.updated_at?.slice(5, 16) }}</span>
      </button>
    </div>

    <div v-else class="omp-empty">
      <p class="omp-empty-title">{{ t('overview.noSessions') }}</p>
      <p class="omp-empty-hint">{{ t('overview.noSessionsHint') }}</p>
      <button class="omp-btn sm primary" @click="emit('start')">{{ t('overview.startChat') }}</button>
    </div>
  </section>
</template>

<style scoped>
.recent-list {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.recent-pending {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-3) var(--omp-sp-2);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
}

.recent-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: var(--omp-sp-2);
  border: none;
  background: transparent;
  text-align: left;
  border-radius: var(--omp-r-sm);
}

.recent-title {
  flex: 1;
  min-width: 0;
  font-size: var(--omp-fs-sm);
  color: var(--omp-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recent-meta {
  flex: none;
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.recent-time {
  flex: none;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.omp-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-6);
  text-align: center;
}

.omp-empty-title {
  margin: 0;
  font-size: var(--omp-fs-sm);
  color: var(--omp-ink);
}

.omp-empty-hint {
  margin: 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-faint);
}
</style>
