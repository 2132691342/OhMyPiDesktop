<script setup lang="ts">
// 插件页：列、装、卸。
//
// 列表来自引擎自己的登记清单（pi 是 settings.json 的 packages，
// omp 是 plugins/package.json 的 dependencies），不是扫 node_modules——
// 扫目录会把几百个间接依赖一起当成插件。
//
// 装 / 卸仍然代跑引擎自己的 CLI：只有引擎知道怎么把包装进它的私有目录、
// 怎么同步它自己的锁文件。我们自己去改目录会留下「配置说装了、目录里没有」的脏状态。
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppIcon from '@/components/common/AppIcon.vue'
import EngineScopeBar from '@/components/common/EngineScopeBar.vue'
import { useEngineScope } from '@/composables/useEngineScope'
import { api } from '@/api/http'
import { useAppStore } from '@/stores/app'
import { useToasts } from '@/composables/useToasts'
import type { EngineKind, PluginRow } from '@/types/api'

const { t } = useI18n()
const app = useAppStore()
const toasts = useToasts()

const rows = ref<PluginRow[]>([])
const registry = ref('')
const readError = ref('')
const loading = ref(false)
const busyName = ref('')
const source = ref('')
const installing = ref(false)
const output = ref('')
const restarting = ref(false)

const kind = computed<EngineKind>(() => app.active)
// 本页作用域（omp / pi / 全部），不跟对话引擎联动。
const { scope, set: setScope } = useEngineScope('plugins')
const installedKinds = computed(() => app.installed.map((e) => e.kind))
const writeKind = computed<EngineKind>(() => (scope.value === 'all' ? 'omp' : scope.value))
const caps = computed(() => app.capsOf(writeKind.value))
const engineParam = computed<EngineKind | undefined>(() => (scope.value === 'all' ? undefined : scope.value))

/** 一行属于哪个引擎：「全部」下靠行自身的 engine_kind 定向写操作。 */
function rowEngine(row: PluginRow): EngineKind {
  return (row.engine_kind || writeKind.value) as EngineKind
}

async function load() {
  loading.value = true
  try {
    const data = await api.plugins(engineParam.value)
    rows.value = data.plugins ?? []
    registry.value = data.registry_path ?? ''
    readError.value = data.read_error ?? ''
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
    const res = await api.installPlugin(target, writeKind.value)
    output.value = res.output
    source.value = ''
    ElMessage.success(t('plugins.installed'))
    await load()
  } catch (err) {
    output.value = err instanceof Error ? err.message : String(err)
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    installing.value = false
  }
}

// 卸的是清单里那一行原文而不是裸包名：pi 的 packages 记的是
// `npm:name@1.2.3`，只给裸名会匹配不到。
async function uninstall(row: PluginRow) {
  try {
    await ElMessageBox.confirm(
      t('plugins.uninstallConfirm', { name: row.plugin_name }),
      t('plugins.uninstallTitle'),
      { confirmButtonText: t('common.uninstall'), cancelButtonText: t('common.cancel'), type: 'warning' },
    )
  } catch {
    return
  }
  busyName.value = row.plugin_name
  output.value = ''
  try {
    const res = await api.uninstallPlugin(row.plugin_source, rowEngine(row))
    output.value = res.output
    await load()
  } catch (err) {
    output.value = err instanceof Error ? err.message : String(err)
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    busyName.value = ''
  }
}

async function restart() {
  restarting.value = true
  try {
    await api.restartEngines(writeKind.value)
    await app.refreshEngines()
    ElMessage.success(t('common.saved'))
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    restarting.value = false
  }
}

function resourceLabel(kinds: string[]): string {
  if (!kinds?.length) return '—'
  return kinds.join(' · ')
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
        <p class="page-sub">{{ t('plugins.scopedSubtitle') }}</p>
      </div>
      <div class="head-actions">
        <EngineScopeBar :scope="scope" :installed="installedKinds" @pick="setScope" />
        <button class="omp-btn" :class="{ 'is-loading': restarting }" :disabled="restarting" @click="restart">
          <AppIcon name="refresh" :size="14" />
          {{ t('plugins.restart') }}
        </button>
        <button class="omp-btn icon ghost" :class="{ 'is-loading': loading }" :title="t('common.refresh')" @click="load">
          <AppIcon name="refresh" :size="15" />
        </button>
      </div>
    </header>

    <div v-if="!caps?.extensions" class="omp-empty">
      <span class="omp-tile omp-tile--lg omp-tile--neutral"><AppIcon name="plug" :size="22" /></span>
      <p class="omp-empty-title">{{ t('plugins.unsupported') }}</p>
      <p class="omp-empty-hint">{{ t('plugins.unsupportedHint', { engine: scope }) }}</p>
    </div>

    <div v-else class="page-body">
      <section class="omp-card card-pad install-card">
        <div class="omp-sec-head">
          <span class="omp-sec-title">{{ t('plugins.installTitle') }}</span>
        </div>
        <div class="install-row">
          <el-input
            v-model="source"
            :placeholder="t('plugins.installPlaceholder')"
            :disabled="installing"
            size="small"
            @keyup.enter="install"
          />
          <button
            class="omp-btn primary"
            :class="{ 'is-loading': installing }"
            :disabled="installing || !source.trim()"
            @click="install"
          >
            <AppIcon name="download" :size="14" />
            {{ t('common.install') }}
          </button>
        </div>
        <p class="hint">{{ t('plugins.restartHint') }}</p>
      </section>

      <p v-if="readError" class="notice">
        <AppIcon name="alert" :size="14" />
        {{ readError }}
      </p>

      <section v-if="output" class="omp-card card-pad">
        <div class="omp-sec-head">
          <span class="omp-sec-title">{{ t('plugins.output') }}</span>
        </div>
        <pre class="output selectable">{{ output }}</pre>
      </section>

      <section>
        <div class="omp-sec-head">
          <span class="omp-sec-title">{{ t('nav.plugins') }} <span class="count">{{ rows.length }}</span></span>
          <span v-if="registry" class="omp-mono registry">{{ registry }}</span>
        </div>

        <div v-if="!rows.length && !loading" class="omp-empty">
          <span class="omp-tile omp-tile--lg omp-tile--neutral"><AppIcon name="plug" :size="22" /></span>
          <p class="omp-empty-title">{{ t('plugins.emptyTitle') }}</p>
          <p class="omp-empty-hint">{{ t('plugins.emptyHint') }}</p>
        </div>

        <el-table v-else :data="rows" size="small">
          <el-table-column :label="t('plugins.engine')" width="72">
            <template #default="{ row }">
              <span class="omp-mono">{{ row.engine_kind }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('plugins.name')" min-width="180">
            <template #default="{ row }">
              <div class="cell-name">
                <span class="name">{{ row.plugin_name }}</span>
                <span v-if="row.plugin_desc" class="desc">{{ row.plugin_desc }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('plugins.version')" width="92">
            <template #default="{ row }">
              <span class="omp-mono">{{ row.plugin_version || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('plugins.resources')" width="120">
            <template #default="{ row }">
              <span class="omp-mono">{{ resourceLabel(row.resource_kinds) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('plugins.state')" width="96">
            <template #default="{ row }">
              <span class="tag" :class="row.installed ? 'ok' : 'warn'">
                {{ row.installed ? t('plugins.installedState') : t('plugins.missingState') }}
              </span>
            </template>
          </el-table-column>
          <el-table-column :label="t('plugins.path')" min-width="180">
            <template #default="{ row }">
              <span class="omp-mono clip">{{ row.plugin_path || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="96" align="right">
            <template #default="{ row }">
              <button
                class="omp-btn text sm danger"
                :class="{ 'is-loading': busyName === row.plugin_name }"
                :disabled="Boolean(busyName)"
                @click="uninstall(row)"
              >
                {{ t('common.uninstall') }}
              </button>
            </template>
          </el-table-column>
        </el-table>
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
  max-width: var(--omp-w-desc);
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
  padding: var(--omp-sp-5) var(--omp-sp-6) var(--omp-sp-12);
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-4);
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
  line-height: var(--omp-lh-base);
}

.notice {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  margin: 0;
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border-radius: var(--omp-r);
  background: var(--omp-warn-soft);
  color: var(--omp-warn);
  font-size: var(--omp-fs-xs);
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

.registry {
  margin-left: auto;
  max-width: 340px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cell-name {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.name {
  font-weight: 650;
  color: var(--omp-ink);
}

.desc {
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.clip {
  display: block;
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
