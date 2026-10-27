<script setup lang="ts">
// 插件页：装 / 卸 / 重启。全部走引擎自己的 CLI（omp plugin / pi install），
// 本应用不自己解 node_modules——那会造出第二套真相。
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import AppIcon from '@/components/common/AppIcon.vue'
import { api } from '@/api/http'
import { useAppStore } from '@/stores/app'
import { useToasts } from '@/composables/useToasts'
import type { EngineKind, PluginRow } from '@/types/api'

const { t } = useI18n()
const app = useAppStore()
const toasts = useToasts()

const rows = ref<PluginRow[]>([])
const loading = ref(false)
const busyName = ref('')
const source = ref('')
const installing = ref(false)
const output = ref('')
const restarting = ref(false)
const kind = computed<EngineKind>(() => app.active)
const caps = computed(() => app.capsOf(kind.value))

async function load() {
  loading.value = true
  try {
    const data = await api.plugins(kind.value)
    rows.value = data.plugins ?? []
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

async function install() {
  const target = source.value.trim()
  if (!target) return
  installing.value = true
  output.value = ''
  try {
    const res = await api.installPlugin(target, kind.value)
    output.value = res.output
    source.value = ''
    toasts.push('ok', t('common.saved'))
    await load()
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    installing.value = false
  }
}

async function uninstall(row: PluginRow) {
  try {
    await ElMessageBox.confirm(t('plugins.uninstallConfirm', { name: row.name }), t('plugins.uninstallTitle'), {
      confirmButtonText: t('common.uninstall'),
      cancelButtonText: t('common.cancel'),
      type: 'warning',
    })
  } catch {
    return
  }
  busyName.value = row.name
  try {
    const res = await api.uninstallPlugin(row.name, kind.value)
    output.value = res.output
    await load()
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    busyName.value = ''
  }
}

async function restart() {
  restarting.value = true
  try {
    await api.restartEngines(kind.value)
    await app.refreshEngines()
    toasts.push('ok', t('common.saved'))
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    restarting.value = false
  }
}

watch(kind, () => {
  rows.value = []
  output.value = ''
  void load()
})

onMounted(load)
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">{{ t('plugins.title') }}</h1>
        <p class="page-sub">{{ t('plugins.subtitle') }}</p>
      </div>
      <div class="head-actions">
        <button class="omp-btn" :class="{ 'is-loading': restarting }" :disabled="restarting" @click="restart">
          <AppIcon name="refresh" :size="14" />
          {{ t('plugins.restart') }}
        </button>
        <button class="omp-btn icon ghost" :title="t('common.refresh')" :class="{ 'is-loading': loading }" @click="load">
          <AppIcon name="refresh" :size="15" />
        </button>
      </div>
    </header>

    <div v-if="!caps?.extensions" class="empty-block">
      <div class="empty-mark"><AppIcon name="plug" :size="22" /></div>
      <p class="empty-title">{{ t('plugins.unsupported') }}</p>
      <p class="empty-hint">{{ t('plugins.unsupportedHint', { engine: kind }) }}</p>
    </div>

    <div v-else class="page-body">
      <section class="install-card card-pad">
        <div class="omp-section-title">{{ t('plugins.installTitle') }}</div>
        <div class="install-row">
          <input
            v-model="source"
            class="omp-input"
            :placeholder="t('plugins.installPlaceholder')"
            :disabled="installing"
            @keyup.enter="install"
          />
          <button class="omp-btn primary" :class="{ 'is-loading': installing }" :disabled="installing || !source.trim()" @click="install">
            <AppIcon name="download" :size="14" />
            {{ t('common.install') }}
          </button>
        </div>
        <p class="hint">{{ t('plugins.restartHint') }}</p>
      </section>

      <section v-if="output" class="card-pad output-card">
        <div class="omp-section-title">{{ t('plugins.output') }}</div>
        <pre class="output selectable">{{ output }}</pre>
      </section>

      <section>
        <div class="omp-section-title">{{ t('nav.plugins') }} <span class="count">{{ rows.length }}</span></div>
        <div v-if="!rows.length && !loading" class="empty">
          <div class="empty-mark"><AppIcon name="plug" :size="22" /></div>
          <p class="empty-title">{{ t('plugins.emptyTitle') }}</p>
          <p class="empty-hint">{{ t('plugins.emptyHint') }}</p>
        </div>
        <div v-else class="grid">
          <article v-for="row in rows" :key="row.name" class="plugin-card card-pad">
            <div class="plugin-head">
              <div class="plugin-title">
                <span class="plugin-name">{{ row.name }}</span>
                <span class="tag">{{ row.version }}</span>
              </div>
              <span class="tag brand">{{ row.kind }}</span>
            </div>
            <div class="plugin-meta">
              <span v-if="row.source" class="tag mono">{{ row.source }}</span>
            </div>
            <div class="plugin-path selectable">{{ row.path }}</div>
            <div class="plugin-actions">
              <button
                class="omp-btn text sm danger"
                :class="{ 'is-loading': busyName === row.name }"
                :disabled="Boolean(busyName)"
                @click="uninstall(row)"
              >
                {{ t('common.uninstall') }}
              </button>
            </div>
          </article>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.empty-block {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-10);
}

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

.install-card {
  max-width: 720px;
}

.install-row {
  display: flex;
  gap: var(--omp-sp-2);
}

.hint {
  margin: var(--omp-sp-2) 0 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-faint);
}

.output {
  margin: 0;
  max-height: 240px;
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

.count {
  font-family: var(--omp-font-mono);
  color: var(--omp-faint);
  letter-spacing: 0;
  text-transform: none;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: var(--omp-sp-3);
}

.plugin-card {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  transition:
    transform var(--omp-dur-fast) var(--omp-ease),
    box-shadow var(--omp-dur-fast) var(--omp-ease);
}

.plugin-card:hover {
  transform: translateY(-1px);
  box-shadow: var(--omp-shadow-lift);
}

.plugin-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--omp-sp-2);
}

.plugin-title {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  min-width: 0;
}

.plugin-name {
  font-size: var(--omp-fs-sm);
  font-weight: 650;
  color: var(--omp-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.plugin-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.plugin-path {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.plugin-actions {
  display: flex;
  justify-content: flex-end;
}

.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-10);
  text-align: center;
}

.empty-mark {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  border-radius: var(--omp-r-lg);
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
}

.empty-title {
  margin: 0;
  font-size: var(--omp-fs-sm);
  font-weight: 600;
  color: var(--omp-ink);
}

.empty-hint {
  margin: 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
}
</style>
