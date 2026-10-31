<script setup lang="ts">
// 关于：版本号、引擎版本、运行日志。
// 日志不自动刷新：排错看的是「刚才那几秒」，定时刷新会把正在看的那段顶走。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import type { EngineInfo } from '@/types/api'

const props = defineProps<{
  engines: EngineInfo[]
  logs: string[]
}>()

const emit = defineEmits<{ refreshLogs: [] }>()

const { t } = useI18n()
const app = useAppStore()

/** 已装引擎的「kind 版本」清单；都没装时给一句「未知」。 */
const engineLine = computed(() => {
  const list = props.engines.filter((e) => e.installed)
  if (!list.length) return t('common.unknown')
  return list.map((e) => `${e.kind} ${e.version}`).join(' · ')
})
</script>

<template>
  <section class="card-pad">
    <div class="omp-section-title">{{ t('settings.groups.about') }}</div>
    <div class="set-row">
      <span class="set-label">{{ t('settings.aboutVersion') }}</span>
      <span class="fact-value">{{ app.version }}</span>
    </div>
    <div class="set-row">
      <span class="set-label">{{ t('settings.aboutEngine') }}</span>
      <span class="fact-value">{{ engineLine }}</span>
    </div>

    <div class="set-row top">
      <div>
        <div class="set-label">{{ t('settings.log') }}</div>
        <div class="set-hint">{{ t('settings.logHint') }}</div>
      </div>
      <button class="omp-btn sm" @click="emit('refreshLogs')">{{ t('common.refresh') }}</button>
    </div>
    <pre class="log selectable">{{ props.logs.join('\n') || t('common.empty') }}</pre>
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

.set-row.top {
  align-items: flex-start;
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

.fact-value {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-sm);
  color: var(--omp-ink);
}

.log {
  margin: var(--omp-sp-2) 0 0;
  max-height: 220px;
  overflow: auto;
  padding: var(--omp-sp-3);
  border-radius: var(--omp-r);
  background: var(--omp-code-bg);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  line-height: 1.6;
  color: var(--omp-ink-5);
  white-space: pre-wrap;
}
</style>
