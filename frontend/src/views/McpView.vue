<script setup lang="ts">
// MCP 页：增删启停 MCP 服务器。
// 两引擎共用 mcp.json 的 mcpServers 形状，一份代码管两边；停用落在哪个键是后端的事。
// 写文件不转发 CLI：只改我们掌管的键，用户手配的 headers / auth 原样保留。
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppIcon from '@/components/common/AppIcon.vue'
import EngineScopeBar from '@/components/common/EngineScopeBar.vue'
import McpDialog from '@/components/mcp/McpDialog.vue'
import McpTable from '@/components/mcp/McpTable.vue'
import { useEngineScope } from '@/composables/useEngineScope'
import { api } from '@/api/http'
import { useAppStore } from '@/stores/app'
import { useToasts } from '@/composables/useToasts'
import type { EngineKind, McpListData, McpServer } from '@/types/api'

const { t } = useI18n()
const app = useAppStore()
const toasts = useToasts()

const data = ref<McpListData | null>(null)
const loading = ref(false)
const saving = ref(false)
const busyName = ref('')
const testing = ref(false)
const dialog = ref(false)
const editing = ref<McpServer | null>(null)
// 草稿类型定义在 McpDialog 里，这里只声明同一形状（弹窗与父组件共享一份）。

// 表单：一条命令或一个 URL，二选一
const form = ref({
  server_name: '',
  mode: 'command' as 'command' | 'url',
  command_text: '',
  server_url: '',
  transport_kind: 'http',
  desc: '',
  work_dir: '',
  env_text: '',
})

// 本页作用域（omp / pi / 全部），不跟对话引擎联动。
const { scope, set: setScope } = useEngineScope('mcp')
const installedKinds = computed(() => app.installed.map((e) => e.kind))
const writeKind = computed<EngineKind>(() => (scope.value === 'all' ? 'omp' : scope.value))
const engineParam = computed<EngineKind | undefined>(() => (scope.value === 'all' ? undefined : scope.value))
// live 只对单引擎有意义：两个引擎的实时状态拿不到一致快照。
const scopeAll = computed(() => scope.value === 'all')

/** 一行属于哪个引擎：「全部」下靠行自身的 engine_kind 定向写操作。 */
function rowEngine(row: McpServer): EngineKind {
  return (row.engine_kind || writeKind.value) as EngineKind
}
const servers = computed<McpServer[]>(() => data.value?.servers ?? [])

async function load(live = false) {
  loading.value = true
  try {
    data.value = await api.mcpList(engineParam.value, live)
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  form.value = {
    server_name: '',
    mode: 'command',
    command_text: '',
    server_url: '',
    transport_kind: 'http',
    desc: '',
    work_dir: '',
    env_text: '',
  }
  dialog.value = true
}

function openEdit(row: McpServer) {
  editing.value = row
  form.value = {
    server_name: row.server_name,
    mode: row.server_url ? 'url' : 'command',
    command_text: row.command_line ?? '',
    server_url: row.server_url ?? '',
    transport_kind: row.transport_kind === 'sse' ? 'sse' : 'http',
    desc: row.desc ?? '',
    work_dir: '',
    env_text: '',
  }
  dialog.value = true
}

async function save() {
  if (!form.value.server_name.trim()) return
  saving.value = true
  try {
    const envList = form.value.env_text
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)
    data.value = await api.mcpSave({
      engine_kind: writeKind.value,
      server_name: form.value.server_name.trim(),
      command_text: form.value.mode === 'command' ? form.value.command_text : undefined,
      server_url: form.value.mode === 'url' ? form.value.server_url : undefined,
      transport_kind: form.value.mode === 'url' ? form.value.transport_kind : undefined,
      desc: form.value.desc || undefined,
      env_list: envList.length ? envList : undefined,
      work_dir: form.value.work_dir || undefined,
    })
    dialog.value = false
    ElMessage.success(t('common.saved'))
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    saving.value = false
  }
}

async function toggle(row: McpServer, enabled: boolean) {
  busyName.value = row.server_name
  try {
    data.value = await api.mcpToggle(row.server_name, enabled, rowEngine(row))
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    busyName.value = ''
  }
}

async function remove(row: McpServer) {
  try {
    await ElMessageBox.confirm(
      t('mcp.removeConfirm', { name: row.server_name }),
      t('mcp.removeTitle'),
      { confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel'), type: 'warning' },
    )
  } catch {
    return
  }
  busyName.value = row.server_name
  try {
    data.value = await api.mcpRemove(row.server_name, rowEngine(row))
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    busyName.value = ''
  }
}

// 探测要起一次引擎子进程（几秒），所以单独一个按钮，不跟着列表自动跑
async function probe() {
  if (scopeAll.value) {
    ElMessage.info(t('mcp.probeScopeOnly'))
    return
  }
  testing.value = true
  try {
    data.value = await api.mcpList(engineParam.value, true)
    if (!data.value.live_checked) {
      ElMessage.info(t('mcp.probeUnavailable'))
    }
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    testing.value = false
  }
}

watch(scope, () => {
  data.value = null
  void load()
})

onMounted(() => void load())
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">{{ t('mcp.title') }}</h1>
        <p class="page-sub">{{ t('mcp.scopedSubtitle') }}</p>
      </div>
      <div class="head-actions">
        <EngineScopeBar :scope="scope" :installed="installedKinds" @pick="setScope" />
        <button class="omp-btn" :class="{ 'is-loading': testing }" :disabled="testing" @click="probe">
          <AppIcon name="search" :size="14" />
          {{ t('mcp.probe') }}
        </button>
        <button class="omp-btn primary" @click="openCreate">
          <AppIcon name="plus" :size="14" />
          {{ t('mcp.add') }}
        </button>
        <button class="omp-btn icon ghost" :class="{ 'is-loading': loading }" :title="t('common.refresh')" @click="load()">
          <AppIcon name="refresh" :size="15" />
        </button>
      </div>
    </header>

    <div class="page-body">
      <section class="omp-card card-pad path-card">
        <div class="omp-sec-head">
          <span class="omp-sec-title">{{ t('mcp.file') }}</span>
        </div>
        <dl class="path-list">
          <div class="omp-kv">
            <dt>{{ t('mcp.userLevel') }}</dt>
            <dd class="omp-mono">{{ data?.user_path || '—' }}</dd>
          </div>
          <div v-if="data?.project_path" class="omp-kv">
            <dt>{{ t('mcp.projectLevel') }}</dt>
            <dd class="omp-mono">{{ data.project_path }}</dd>
          </div>
        </dl>
        <p class="hint">{{ t('mcp.fileHint') }}</p>
      </section>

      <p v-if="data?.read_errors?.length" class="notice">
        <AppIcon name="alert" :size="14" />
        {{ data.read_errors.join('；') }}
      </p>

      <McpTable
        :servers="servers"
        :loading="loading"
        :busy-name="busyName"
        @create="openCreate"
        @edit="openEdit"
        @remove="remove"
        @toggle="toggle"
      />
    </div>

    <McpDialog v-model:open="dialog" :form="form" :editing="editing?.server_name ?? ''" :saving="saving" @save="save" />
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

.path-card {
  max-width: 720px;
}

.path-list {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-1);
  margin: 0;
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

.count {
  font-family: var(--omp-font-mono);
  color: var(--omp-faint);
  letter-spacing: 0;
  text-transform: none;
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

.row-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--omp-sp-2);
}
</style>
