<script setup lang="ts">
// 会话页：列表复用 SessionList（与对话页左栏同一份实现），这里只补范围切换与汇总读数。
// 汇总读 app.sessions——它恒为「当前目录」的快照，所以标题行明确标出这是当前目录的合计，
// 不随上面的列表范围变化，避免把两件事读成一件。
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import AppIcon from '@/components/common/AppIcon.vue'
import SessionList from '@/components/sessions/SessionList.vue'
import { useAppStore } from '@/stores/app'
import { useToasts } from '@/composables/useToasts'

const { t } = useI18n()
const app = useAppStore()
const router = useRouter()
const toasts = useToasts()

const scope = ref<'workspace' | 'all'>('workspace')
const refreshing = ref(false)
// 换范围或点刷新都靠重挂列表：SessionList 只在挂载时取数，没有对外的重新取数入口。
const listKey = ref(0)

const counts = computed(() => {
  let messages = 0
  let tokens = 0
  let cost = 0
  for (const row of app.sessions) {
    messages += row.messages ?? 0
    tokens += row.tokens ?? 0
    cost += row.cost ?? 0
  }
  return { sessions: app.sessions.length, messages, tokens, cost }
})

function formatTokens(n: number): string {
  if (!n) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 100_000 ? 0 : 1)}k`
  return String(n)
}

async function refresh() {
  refreshing.value = true
  try {
    await app.refreshSessions()
    listKey.value += 1
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    refreshing.value = false
  }
}

// 会话的落点是对话页：带上 path，由对话页按它载入这一条。
async function openSession(path: string) {
  try {
    await router.push({ name: 'chat', query: { session: path } })
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  }
}

function onChanged() {
  void app.refreshSessions().catch((err: unknown) => {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  })
}
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">{{ t('sessions.title') }}</h1>
        <p class="page-sub">{{ t('sessions.subtitle') }}</p>
      </div>
      <div class="head-actions">
        <div class="seg">
          <button class="seg-btn" :class="{ active: scope === 'workspace' }" @click="scope = 'workspace'">
            {{ t('sessions.thisWorkspace') }}
          </button>
          <button class="seg-btn" :class="{ active: scope === 'all' }" @click="scope = 'all'">
            {{ t('sessions.allWorkspace') }}
          </button>
        </div>
        <button
          class="omp-btn icon ghost"
          :class="{ 'is-loading': refreshing }"
          :title="t('common.refresh')"
          :disabled="refreshing"
          @click="refresh"
        >
          <AppIcon name="refresh" :size="15" />
        </button>
      </div>
    </header>

    <div class="page-body">
      <section class="summary">
        <div class="summary-head">
          <span class="omp-section-title">{{ t('sessions.thisWorkspace') }}</span>
          <span class="summary-scope selectable" :title="app.workspace">{{ app.workspace }}</span>
        </div>
        <div class="stat-row">
          <div class="stat">
            <span class="stat-label">{{ t('nav.sessions') }}</span>
            <span class="stat-value">{{ counts.sessions }}</span>
          </div>
          <div class="stat">
            <span class="stat-label">{{ t('sessions.messages') }}</span>
            <span class="stat-value">{{ counts.messages }}</span>
          </div>
          <div class="stat">
            <span class="stat-label">{{ t('sessions.tokens') }}</span>
            <span class="stat-value">{{ formatTokens(counts.tokens) }}</span>
          </div>
          <div class="stat">
            <span class="stat-label">{{ t('sessions.cost') }}</span>
            <span class="stat-value">${{ counts.cost.toFixed(2) }}</span>
          </div>
        </div>
      </section>

      <section class="list-panel">
        <SessionList
          :key="`${scope}-${listKey}`"
          :workspace-only="scope === 'workspace'"
          @open="openSession"
          @changed="onChanged"
        />
      </section>
    </div>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}

.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--omp-sp-4);
  padding: var(--omp-sp-5) var(--omp-sp-6) var(--omp-sp-4);
  border-bottom: 1px solid var(--omp-line);
  flex: none;
}

.page-title {
  margin: 0;
  font-family: var(--omp-font-display);
  font-size: var(--omp-fs-xl);
  font-weight: 650;
  color: var(--omp-ink);
}

.page-sub {
  margin: 4px 0 0;
  max-width: var(--omp-w-desc, 620px);
  font-size: var(--omp-fs-sm);
  line-height: 1.65;
  color: var(--omp-mute);
}

.head-actions {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
}

.seg {
  display: flex;
  gap: 2px;
  padding: 2px;
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r);
  background: var(--omp-window);
}

/* 分段控件不缩放：缩放会让相邻按钮互相挤动 */




.page-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-4);
  padding: var(--omp-sp-4) var(--omp-sp-6) var(--omp-sp-6);
}

.summary {
  flex: none;
}

.summary-head {
  display: flex;
  align-items: baseline;
  gap: var(--omp-sp-3);
  margin-bottom: var(--omp-sp-2);
}

.summary-head .omp-section-title {
  margin-bottom: 0;
}

.summary-scope {
  flex: 1;
  min-width: 0;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.stat-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: var(--omp-sp-3);
}

/* 读数是纯文本：不套描边胶囊、不做悬停反馈 */
.stat {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.stat-label {
  font-size: var(--omp-fs-3xs);
  font-weight: 650;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--omp-faint);
}

.stat-value {
  font-family: var(--omp-font-display);
  font-size: var(--omp-fs-xl);
  font-weight: 600;
  color: var(--omp-ink);
  font-variant-numeric: tabular-nums;
}

.list-panel {
  flex: 1;
  min-height: 0;
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r-lg);
  background: var(--omp-surface);
  overflow: hidden;
}
</style>
