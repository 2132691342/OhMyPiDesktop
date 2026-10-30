<script setup lang="ts">
// 模型页：服务商（models.yml，可写）/ 引擎眼下的模型（只读）/ 登录（OAuth）。
// 三块的分界线就是「我们能不能改」：能从界面改的，和只能读的，不该混在一张表里。
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import AppIcon from '@/components/common/AppIcon.vue'
import ProviderDialog from '@/components/models/ProviderDialog.vue'
import ModelTable from '@/components/models/ModelTable.vue'
import LoginPanel from '@/components/models/LoginPanel.vue'
import ProviderList from '@/components/models/ProviderList.vue'
import EngineScopeBar from '@/components/common/EngineScopeBar.vue'
import { useEngineScope } from '@/composables/useEngineScope'
import { api } from '@/api/http'
import { useToasts } from '@/composables/useToasts'
import { useAppStore } from '@/stores/app'
import type { EngineKind, LoginProvider, ModelEntry, ModelRow, ProviderView } from '@/types/api'

const { t } = useI18n()
const toasts = useToasts()
const app = useAppStore()

// 本页的作用域（omp / pi / 全部），不跟对话引擎联动。
const { scope, set: setScope } = useEngineScope('models')
const installedKinds = computed(() => app.installed.map((e) => e.kind))

/** 写操作必须落在一个具体引擎上：作用域是「全部」时靠行上的 engine_kind 定向。 */
const writeKind = computed<EngineKind>(() => (scope.value === 'all' ? 'omp' : scope.value))
const caps = computed(() => app.capsOf(writeKind.value))

const providers = ref<ProviderView[]>([])
const configPath = ref('')
const models = ref<ModelRow[]>([])
const logins = ref<LoginProvider[]>([])
const loadingProviders = ref(false)
const loadingModels = ref(false)
const loadingLogins = ref(false)
const engineMissing = ref(false)

const dialogOpen = ref(false)
const saving = ref(false)
const loggingIn = ref('')

// 表单是「一份可编辑副本」，脏数据不往 providers 里写，保存成功才用返回值整体替换
const form = ref<{ name: string; base_url: string; api: string; api_key: string; models: ModelEntry[] }>({
  name: '',
  base_url: '',
  api: 'openai-completions',
  api_key: '',
  models: [],
})
const editing = ref('')
// 正在编辑的那一行属于哪个引擎（"全部"作用域下新建时取当前作用域）。
const editingKind = ref<EngineKind>('omp')

const apiOptions = [
  'openai-completions',
  'openai-responses',
  'anthropic-messages',
  'google-generative-ai',
  'openai-codex-responses',
]

// api 的 engine 参数：'all' 时省略，后端会两个都返回。
const engineParam = computed<EngineKind | undefined>(() => (scope.value === 'all' ? undefined : scope.value))

async function loadProviders() {
  loadingProviders.value = true
  try {
    const view = await api.providers(engineParam.value)
    providers.value = view.providers ?? []
    configPath.value = view.config_path
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    loadingProviders.value = false
  }
}

/** 引擎没起来时读不到模型列表，这是预期状态，不当失败报警。 */
async function loadModels() {
  loadingModels.value = true
  try {
    const data = await api.models(engineParam.value)
    models.value = data.models ?? []
    engineMissing.value = false
  } catch {
    models.value = []
    engineMissing.value = true
  } finally {
    loadingModels.value = false
  }
}

async function loadLogins() {
  // 登录只有 omp 有 RPC 面；作用域是 pi / 全部时不发这个请求。
  if (scope.value !== 'omp' && scope.value !== 'all') {
    logins.value = []
    return
  }
  loadingLogins.value = true
  try {
    const data = await api.loginProviders('omp')
    logins.value = data.providers ?? []
  } catch {
    logins.value = []
  } finally {
    loadingLogins.value = false
  }
}

function openCreate() {
  editing.value = ''
  editingKind.value = writeKind.value
  form.value = { name: '', base_url: '', api: 'openai-completions', api_key: '', models: [] }
  dialogOpen.value = true
}

function openEdit(view: ProviderView) {
  editing.value = view.provider_name
  // 记住这一行属于哪个引擎：编辑同名服务商时不能写到另一个引擎的文件里。
  editingKind.value = (view.engine_kind || writeKind.value) as EngineKind
  form.value = {
    name: view.provider_name,
    base_url: view.base_url,
    api: view.api || 'openai-completions',
    // 打码后的密钥不回填：用户没改就提交空串，后端会保留原值
    api_key: '',
    models: (view.models ?? []).map((m) => ({ ...m })),
  }
  dialogOpen.value = true
}

// 模型表里的就地编辑（加 / 删 / 改能力勾选）随 ProviderDialog 搬走了：
// 它们只在弹窗打开期间存在，不该由这一页持有。
async function save() {
  const name = form.value.name.trim()
  if (!name) return
  saving.value = true
  try {
    const payload: Partial<ProviderView> & { models?: ModelEntry[] } = {
      base_url: form.value.base_url.trim(),
      api: form.value.api,
      api_key: form.value.api_key.trim(),
      models: form.value.models.filter((m) => m.model_id.trim()),
    }
    // 写入必须落到一个具体引擎：编辑已有行时用它自己的 engine_kind，
    // 新建时用当前作用域（「全部」时退回 omp）。
    const target = editing.value ? editingKind.value : writeKind.value
    await api.upsertProvider(name, payload, target)
    dialogOpen.value = false
    toasts.push('ok', t('common.saved'))
    await Promise.all([loadProviders(), loadModels()])
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    saving.value = false
  }
}

async function remove(view: ProviderView) {
  try {
    await ElMessageBox.confirm(t('models.deleteConfirm', { name: view.provider_name }), t('models.deleteTitle'), {
      confirmButtonText: t('common.delete'),
      cancelButtonText: t('common.cancel'),
      type: 'warning',
    })
  } catch {
    return
  }
  try {
    // 删除必须落在行自己的引擎上："全部"下两个引擎可能同名。
    const target = (view.engine_kind || writeKind.value) as EngineKind
    await api.deleteProvider(view.provider_name, target)
    toasts.push('ok', t('common.saved'))
    await Promise.all([loadProviders(), loadModels()])
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  }
}

async function login(provider: LoginProvider) {
  loggingIn.value = provider.id
  try {
    await api.startLogin(provider.id, 'omp')
    toasts.push('info', t('models.loginHint'))
    await loadLogins()
    await loadModels()
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    loggingIn.value = ''
  }
}

const authenticated = computed(() => logins.value.filter((p) => p.authenticated).length)

async function reload() {
  await Promise.all([loadProviders(), loadModels(), loadLogins()])
}

watch(scope, () => {
  providers.value = []
  models.value = []
  logins.value = []
  void reload()
})

onMounted(() => {
  void reload()
})
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">{{ t('models.title') }}</h1>
        <p class="page-sub">{{ t('models.scopedSubtitle') }}</p>
      </div>
      <div class="head-actions">
        <EngineScopeBar :scope="scope" :installed="installedKinds" @pick="setScope" />
        <button class="omp-btn icon ghost" :class="{ 'is-loading': loadingProviders }" :title="t('common.refresh')" @click="loadProviders">
          <AppIcon name="refresh" :size="15" />
        </button>
        <button class="omp-btn primary" @click="openCreate">
          <AppIcon name="plus" :size="14" />
          {{ t('models.addProvider') }}
        </button>
      </div>
    </header>

    <div class="page-body">
      <!-- 服务商 -->
      <ProviderList
        :providers="providers"
        :loading="loadingProviders"
        :config-path="configPath"
        @create="openCreate"
        @edit="openEdit"
        @remove="remove"
      />

      <!-- 登录：只有支持界面登录的引擎才渲染（pi 的登录在终端里做） -->
      <LoginPanel
        v-if="caps?.login"
        :logins="logins"
        :logging-in="loggingIn"
        :authenticated="authenticated"
        @login="login"
      />

      <!-- 可用模型（只读） -->
      <ModelTable :models="models" :engine-missing="engineMissing" />
    </div>

    <!-- 服务商编辑 -->
    <ProviderDialog
      v-model="dialogOpen"
      :form="form"
      :editing="editing"
      :saving="saving"
      :api-options="apiOptions"
      @save="save"
    />
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
  max-width: 680px;
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
  gap: var(--omp-sp-6);
}

/* 服务商卡 / 模型胶囊 / 登录 / 编辑弹窗的样式都随各自的子组件搬走了，
   这一页只管页头与各节之间的间距。 */
</style>
