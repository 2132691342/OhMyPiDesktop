<script setup lang="ts">
// 应用页：装 / 升级 / 卸载两个引擎。
//
// 我们不改自己实现安装：跑的就是引擎的官方安装器或包管理器命令，
// 界面上显示的命令与真正执行的是同一个字符串。所以这一页的每一行都同时
// 回答三件事：装的什么版本、能不能装、装了会跑哪条命令。
//
// 一行一个 AppCard：这里只做分组与派发，渲染细节在组件里。
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import AppCard from '@/components/apps/AppCard.vue'
import { api } from '@/api/http'
import { useAppStore } from '@/stores/app'
import { useToasts } from '@/composables/useToasts'
import type { AppRow } from '@/types/api'

const { t } = useI18n()
const app = useAppStore()
const toasts = useToasts()

const loading = ref(false)
const busy = ref<Record<string, boolean>>({})
const copied = ref('')

const groups = computed(() => [
  { key: 'installed', rows: app.apps.filter((row) => row.installed) },
  { key: 'missing', rows: app.apps.filter((row) => !row.installed) },
])

async function load(refresh = false) {
  loading.value = true
  try {
    await app.loadApps(refresh)
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = text
    window.setTimeout(() => {
      if (copied.value === text) copied.value = ''
    }, 1600)
  } catch {
    toasts.push('warn', t('apps.copyFailed'))
  }
}

/** 三个动作共用一个入口：只有按钮文案与端点不同。 */
async function run(row: AppRow, action: 'install' | 'update' | 'uninstall') {
  if (busy.value[row.kind]) return
  busy.value = { ...busy.value, [row.kind]: true }
  try {
    if (action === 'install') await api.installApp(row.kind)
    else if (action === 'update') await api.updateApp(row.kind)
    else await api.uninstallApp(row.kind)
    await app.loadApps(true)
    await app.refreshEngines()
    toasts.push('ok', t(`apps.done.${action}`, { name: row.app_name }))
  } catch (err) {
    // 失败不弹窗：卡片里已有命令原文与输出，消息只补一句「为什么失败」。
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    busy.value = { ...busy.value, [row.kind]: false }
  }
}

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">{{ t('apps.title') }}</h1>
        <p class="page-sub">{{ t('apps.subtitle') }}</p>
      </div>
      <div class="head-actions">
        <button
          class="omp-btn icon ghost"
          :title="t('apps.recheckLatest')"
          :class="{ 'is-loading': loading }"
          :disabled="loading"
          @click="load(true)"
        >
          <AppIcon name="refresh" :size="15" />
        </button>
      </div>
    </header>

    <div class="page-body">
      <section v-for="group in groups" :key="group.key" v-show="group.rows.length">
        <div class="omp-sec-head">
          <span class="omp-sec-title">{{ t(`apps.group.${group.key}`) }} <span class="count">{{ group.rows.length }}</span></span>
        </div>
        <div class="app-list">
          <AppCard
            v-for="row in group.rows"
            :key="row.kind"
            :row="row"
            :progress="app.appProgress[row.kind]"
            :busy="Boolean(busy[row.kind])"
            :copied="copied"
            @run="run(row, $event)"
            @copy="copy"
          />
        </div>
      </section>

      <p class="note">{{ t('apps.boundary') }}</p>
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
  gap: var(--omp-sp-2);
}

.page-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: var(--omp-sp-5) var(--omp-sp-6) var(--omp-sp-10);
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-5);
}

.count {
  font-family: var(--omp-font-mono);
  color: var(--omp-faint);
  letter-spacing: 0;
  text-transform: none;
}

.app-list {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-3);
}

.note {
  max-width: 860px;
  margin: 0;
  font-size: var(--omp-fs-2xs);
  line-height: 1.7;
  color: var(--omp-faint);
}
</style>
