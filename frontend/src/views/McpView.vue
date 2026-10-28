<script setup lang="ts">
// MCP 页：增删启停 MCP 服务器。
//
// 两个引擎共用 mcp.json 的 mcpServers 形状，所以这一份代码管两边；
// 差别只有「停用一个服务器落在哪个键上」（omp 用顶层名单，pi 写条目），
// 那是后端的事，界面只发「启 / 停」。
//
// 写文件而不是转发 CLI：界面能表达的参数面比引擎 CLI 的小得多，
// 转发意味着每出一个新参数就要改前端；而写文件只改我们掌管的那几个键，
// 用户手配的 headers / auth / oauth 原样保留。
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppIcon from '@/components/common/AppIcon.vue'
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

const kind = computed<EngineKind>(() => app.active)
const servers = computed<McpServer[]>(() => data.value?.servers ?? [])

async function load(live = false) {
  loading.value = true
  try {
    data.value = await api.mcpList(kind.value, live)
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
      engine_kind: kind.value,
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
    data.value = await api.mcpToggle(row.server_name, enabled, kind.value)
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
    data.value = await api.mcpRemove(row.server_name, kind.value)
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    busyName.value = ''
  }
}

// 探测要起一次引擎子进程（几秒），所以单独一个按钮，不跟着列表自动跑
async function probe() {
  testing.value = true
  try {
    data.value = await api.mcpList(kind.value, true)
    if (!data.value.live_checked) {
      ElMessage.info(t('mcp.probeUnavailable'))
    }
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    testing.value = false
  }
}

function statusTag(row: McpServer): { cls: string; text: string } {
  if (row.live_status === 'connected') return { cls: 'ok', text: t('mcp.connected') }
  if (row.live_status === 'failed') return { cls: 'bad', text: t('mcp.failed') }
  if (row.live_status) return { cls: 'warn', text: row.live_status }
  if (!row.enabled) return { cls: '', text: t('mcp.disabled') }
  return { cls: 'brand', text: t('mcp.configured') }
}

watch(kind, () => {
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
        <p class="page-sub">{{ t('mcp.subtitle', { engine: kind }) }}</p>
      </div>
      <div class="head-actions">
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

      <section>
        <div class="omp-sec-head">
          <span class="omp-sec-title">{{ t('mcp.servers') }} <span class="count">{{ servers.length }}</span></span>
        </div>

        <div v-if="!servers.length && !loading" class="omp-empty">
          <span class="omp-tile omp-tile--lg omp-tile--neutral"><AppIcon name="server" :size="22" /></span>
          <p class="omp-empty-title">{{ t('mcp.emptyTitle') }}</p>
          <p class="omp-empty-hint">{{ t('mcp.emptyHint') }}</p>
          <button class="omp-btn sm primary" @click="openCreate">{{ t('mcp.add') }}</button>
        </div>

        <el-table v-else :data="servers" size="small" class="mcp-table">
          <el-table-column :label="t('mcp.name')" min-width="140">
            <template #default="{ row }">
              <div class="cell-name">
                <span class="name">{{ row.server_name }}</span>
                <span v-if="row.desc" class="desc">{{ row.desc }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('mcp.transport')" width="92">
            <template #default="{ row }">
              <span class="tag">{{ row.transport_kind || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('mcp.target')" min-width="180">
            <template #default="{ row }">
              <span class="omp-mono clip">{{ row.command_line || row.server_url || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('mcp.scope')" width="86">
            <template #default="{ row }">
              <span class="tag">{{ row.scope_kind }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('mcp.status')" width="104">
            <template #default="{ row }">
              <span class="tag" :class="statusTag(row).cls">{{ statusTag(row).text }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="188" align="right">
            <template #default="{ row }">
              <div class="row-actions">
                <el-switch
                  :model-value="row.enabled"
                  :disabled="Boolean(busyName)"
                  @change="(v: boolean) => toggle(row, v)"
                />
                <button class="omp-btn text sm" :disabled="Boolean(busyName)" @click="openEdit(row)">
                  {{ t('common.edit') }}
                </button>
                <button
                  class="omp-btn text sm danger"
                  :class="{ 'is-loading': busyName === row.server_name }"
                  :disabled="Boolean(busyName)"
                  @click="remove(row)"
                >
                  {{ t('common.delete') }}
                </button>
              </div>
            </template>
          </el-table-column>
        </el-table>

        <!-- 停用名单压住时说明原因：否则用户只会以为开关坏了 -->
        <p v-if="servers.some((s) => s.disabled_by_deny)" class="hint">
          {{ t('mcp.denyHint') }}
        </p>
      </section>
    </div>

    <el-dialog v-model="dialog" :title="editing ? t('mcp.editTitle') : t('mcp.addTitle')" width="520">
      <el-form label-position="top" size="small">
        <el-form-item :label="t('mcp.name')">
          <el-input v-model="form.server_name" :disabled="Boolean(editing)" :placeholder="t('mcp.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('mcp.mode')">
          <el-radio-group v-model="form.mode">
            <el-radio-button value="command">{{ t('mcp.modeCommand') }}</el-radio-button>
            <el-radio-button value="url">{{ t('mcp.modeUrl') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="form.mode === 'command'" :label="t('mcp.command')">
          <el-input v-model="form.command_text" :placeholder="t('mcp.commandPlaceholder')" />
        </el-form-item>
        <template v-else>
          <el-form-item :label="t('mcp.url')">
            <el-input v-model="form.server_url" placeholder="https://example.com/mcp" />
          </el-form-item>
          <el-form-item :label="t('mcp.transport')">
            <el-radio-group v-model="form.transport_kind">
              <el-radio-button value="http">http</el-radio-button>
              <el-radio-button value="sse">sse</el-radio-button>
            </el-radio-group>
          </el-form-item>
        </template>
        <el-form-item v-if="form.mode === 'command'" :label="t('mcp.workDir')">
          <el-input v-model="form.work_dir" :placeholder="t('mcp.workDirPlaceholder')" />
        </el-form-item>
        <el-form-item v-if="form.mode === 'command'" :label="t('mcp.env')">
          <el-input v-model="form.env_text" type="textarea" :rows="3" :placeholder="t('mcp.envPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('mcp.desc')">
          <el-input v-model="form.desc" :placeholder="t('mcp.descPlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <button class="omp-btn" @click="dialog = false">{{ t('common.cancel') }}</button>
        <button
          class="omp-btn primary"
          :class="{ 'is-loading': saving }"
          :disabled="saving || !form.server_name.trim()"
          @click="save"
        >
          {{ t('common.save') }}
        </button>
      </template>
    </el-dialog>
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
