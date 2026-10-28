<script setup lang="ts">
// 模型页：服务商（models.yml，可写）/ 引擎眼下的模型（只读）/ 登录（OAuth）。
// 三块的分界线就是「我们能不能改」：能从界面改的，和只能读的，不该混在一张表里。
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import AppIcon from '@/components/common/AppIcon.vue'
import { api } from '@/api/http'
import { useToasts } from '@/composables/useToasts'
import { useAppStore } from '@/stores/app'
import type { EngineKind, LoginProvider, ModelEntry, ModelRow, ProviderView } from '@/types/api'

const { t } = useI18n()
const toasts = useToasts()
const app = useAppStore()

/** 当前编辑/查看的引擎。两个引擎的配置文件格式完全不同，界面不能混着展示。 */
const kind = computed<EngineKind>(() => app.active)
const caps = computed(() => app.capsOf(kind.value))

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

const apiOptions = [
  'openai-completions',
  'openai-responses',
  'anthropic-messages',
  'google-generative-ai',
  'openai-codex-responses',
]

async function loadProviders() {
  loadingProviders.value = true
  try {
    const view = await api.providers(kind.value)
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
    const data = await api.models(kind.value)
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
  loadingLogins.value = true
  try {
    const data = await api.loginProviders(kind.value)
    logins.value = data.providers ?? []
  } catch {
    logins.value = []
  } finally {
    loadingLogins.value = false
  }
}

function openCreate() {
  editing.value = ''
  form.value = { name: '', base_url: '', api: 'openai-completions', api_key: '', models: [] }
  dialogOpen.value = true
}

function openEdit(view: ProviderView) {
  editing.value = view.provider_name
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

function addModel() {
  form.value.models = [...form.value.models, { model_id: '', model_name: '', context_window: 128000, max_tokens: 8192 }]
}

function removeModel(index: number) {
  form.value.models = form.value.models.filter((_, i) => i !== index)
}

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
    const view = await api.upsertProvider(name, payload, kind.value)
    providers.value = view.providers ?? []
    configPath.value = view.config_path
    dialogOpen.value = false
    toasts.push('ok', t('common.saved'))
    await loadModels()
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
    const next = await api.deleteProvider(view.provider_name, kind.value)
    providers.value = next.providers ?? []
    toasts.push('ok', t('common.saved'))
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  }
}

async function login(provider: LoginProvider) {
  loggingIn.value = provider.id
  try {
    await api.startLogin(provider.id, kind.value)
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

watch(kind, () => {
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
        <p class="page-sub">{{ t('models.subtitle', { engine: kind }) }}</p>
      </div>
      <div class="head-actions">
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
      <section>
        <div class="omp-sec-head">
          <span class="omp-sec-title">{{ t('models.providers') }} <span class="count">{{ providers.length }}</span></span>
          <span v-if="configPath" class="omp-mono registry">{{ configPath }}</span>
        </div>
        <div v-if="!providers.length && !loadingProviders" class="omp-empty">
          <span class="omp-tile omp-tile--lg omp-tile--neutral"><AppIcon name="cpu" :size="22" /></span>
          <p class="omp-empty-title">{{ t('models.emptyTitle') }}</p>
          <p class="omp-empty-hint">{{ t('models.emptyHint') }}</p>
          <button class="omp-btn sm primary" @click="openCreate">{{ t('models.addProvider') }}</button>
        </div>
        <div v-else class="grid">
          <article v-for="view in providers" :key="view.provider_name" class="provider-card card-pad">
            <div class="provider-head">
              <span class="provider-name">{{ view.provider_name }}</span>
              <span class="tag" :class="view.has_key ? 'ok' : 'warn'">
                {{ view.has_key ? t('models.keyHidden') : t('models.apiKey') }}
              </span>
            </div>
            <div class="provider-facts">
              <div class="fact">
                <span class="fact-label">{{ t('models.baseUrl') }}</span>
                <span class="fact-value mono selectable">{{ view.base_url || '—' }}</span>
              </div>
              <div class="fact">
                <span class="fact-label">{{ t('models.api') }}</span>
                <span class="fact-value mono">{{ view.api || '—' }}</span>
              </div>
              <div class="fact" v-if="view.api_key">
                <span class="fact-label">{{ t('models.apiKey') }}</span>
                <span class="fact-value mono">{{ view.api_key }}</span>
              </div>
            </div>
            <div class="model-chips">
              <span v-for="model in view.models" :key="model.model_id" class="tag">{{ model.model_id }}</span>
              <span v-if="!view.models?.length" class="tag warn">{{ t('models.addModel') }}</span>
            </div>
            <div class="provider-actions">
              <button class="omp-btn text sm" @click="openEdit(view)">{{ t('common.edit') }}</button>
              <button class="omp-btn text sm danger" @click="remove(view)">{{ t('common.delete') }}</button>
            </div>
          </article>
        </div>
      </section>

      <!-- 登录：只有支持界面登录的引擎才渲染（pi 的登录在终端里做） -->
      <section v-if="caps?.login">
        <div class="omp-sec-head">
          <span class="omp-sec-title">{{ t('models.login') }} <span class="count">{{ authenticated }}/{{ logins.length }}</span></span>
        </div>
        <p class="hint">{{ t('models.loginHint') }}</p>
        <div v-if="logins.length" class="login-grid">
          <div v-for="item in logins" :key="item.id" class="login-row">
            <div class="login-main">
              <span class="login-name">{{ item.name }}</span>
              <span class="login-id mono">{{ item.id }}</span>
            </div>
            <span class="tag" :class="item.authenticated ? 'ok' : item.available ? '' : 'warn'">
              {{ item.authenticated ? t('models.loggedIn') : item.available ? t('models.notLoggedIn') : t('models.unavailable') }}
            </span>
            <button
              class="omp-btn sm"
              :class="{ 'is-loading': loggingIn === item.id }"
              :disabled="!item.available || Boolean(loggingIn)"
              @click="login(item)"
            >
              {{ t('models.login') }}
            </button>
          </div>
        </div>
        <p v-else-if="!loadingLogins" class="hint">{{ t('chat.engineDown') }}</p>
      </section>

      <!-- 可用模型（只读） -->
      <section>
        <div class="omp-sec-head">
          <span class="omp-sec-title">{{ t('chat.model') }} <span class="count">{{ models.length }}</span></span>
        </div>
        <p v-if="engineMissing" class="hint">{{ t('chat.engineDown') }} — {{ t('models.emptyHint') }}</p>
        <el-table v-else :data="models" size="small" max-height="420">
          <el-table-column prop="provider" :label="t('models.providers')" width="130" />
          <el-table-column :label="t('models.modelId')" min-width="200">
            <template #default="{ row }">
              <span class="omp-mono selectable">{{ row.id }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('models.contextWindow')" width="120" align="right">
            <template #default="{ row }">
              <span class="omp-mono">{{ row.context_window ? row.context_window.toLocaleString() : '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('models.maxTokens')" width="100" align="right">
            <template #default="{ row }">
              <span class="omp-mono">{{ row.max_tokens ? row.max_tokens.toLocaleString() : '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('models.reasoning')" width="92" align="center">
            <template #default="{ row }">
              <span v-if="row.reasoning" class="tag ok">{{ t('common.yes') }}</span>
              <span v-else class="tag">—</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('models.input')" width="120">
            <template #default="{ row }">
              <span class="omp-mono">{{ (row.input ?? []).join(',') || 'text' }}</span>
            </template>
          </el-table-column>
        </el-table>
        <p class="hint">{{ t('models.testHint') }}</p>
      </section>
    </div>

    <!-- 服务商编辑 -->
    <el-dialog v-model="dialogOpen" :title="editing ? t('models.editProvider') : t('models.addProvider')" width="560px">
      <el-form label-position="top" size="small">
        <el-form-item :label="t('common.name')">
          <el-input v-model="form.name" :disabled="Boolean(editing)" placeholder="router" />
        </el-form-item>
        <el-form-item :label="t('models.baseUrl')">
          <el-input v-model="form.base_url" placeholder="https://api.example.com/v1" />
        </el-form-item>
        <div class="field-row">
          <el-form-item :label="t('models.api')" class="grow">
            <el-select v-model="form.api" style="width: 100%">
              <el-option v-for="option in apiOptions" :key="option" :label="option" :value="option" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('models.apiKey')" class="grow">
            <el-input v-model="form.api_key" :placeholder="t('models.apiKeyHint')" />
          </el-form-item>
        </div>
      </el-form>

      <div class="models-head">
        <span class="field-label">{{ t('models.modelsLabel') }}</span>
        <button class="omp-btn text sm" @click="addModel">{{ t('models.addModel') }}</button>
      </div>
      <div v-for="(model, index) in form.models" :key="index" class="model-row">
        <input v-model="model.model_id" class="omp-input" :placeholder="t('models.modelId')" />
        <input v-model="model.model_name" class="omp-input" :placeholder="t('common.name')" />
        <input v-model.number="model.context_window" class="omp-input num-input" type="number" :placeholder="t('models.contextWindow')" />
        <input v-model.number="model.max_tokens" class="omp-input num-input" type="number" :placeholder="t('models.maxTokens')" />
        <button class="omp-btn icon ghost danger" :title="t('common.delete')" @click="removeModel(index)">
          <AppIcon name="close" :size="13" />
        </button>
      </div>
      <template #footer>
        <button class="omp-btn" @click="dialogOpen = false">{{ t('common.cancel') }}</button>
        <button class="omp-btn primary" :class="{ 'is-loading': saving }" :disabled="saving || !form.name.trim()" @click="save">
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

.count {
  font-family: var(--omp-font-mono);
  color: var(--omp-faint);
  letter-spacing: 0;
  text-transform: none;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: var(--omp-sp-3);
}

.provider-card {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  transition:
    transform var(--omp-dur-fast) var(--omp-ease),
    box-shadow var(--omp-dur-fast) var(--omp-ease);
}

.provider-card:hover {
  transform: translateY(-1px);
  box-shadow: var(--omp-shadow-lift);
}

.provider-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--omp-sp-2);
}

.provider-name {
  font-size: var(--omp-fs-base);
  font-weight: 650;
  color: var(--omp-ink);
}

.provider-facts {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.fact {
  display: flex;
  gap: var(--omp-sp-2);
  font-size: var(--omp-fs-3xs);
  min-width: 0;
}

.fact-label {
  flex: none;
  width: 78px;
  color: var(--omp-faint);
}

.fact-value {
  min-width: 0;
  color: var(--omp-ink-5);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mono {
  font-family: var(--omp-font-mono);
}

.model-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.provider-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--omp-sp-1);
}

.registry {
  margin-left: auto;
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.hint {
  margin: 0 0 var(--omp-sp-2);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  line-height: 1.6;
}

.login-grid {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-width: 720px;
}

.login-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: 6px var(--omp-sp-2);
  border-radius: var(--omp-r-sm);
  transition: background-color var(--omp-dur-fast) var(--omp-ease);
}

.login-row:hover {
  background: var(--omp-hover);
}

.login-main {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
  flex: 1;
}

.login-name {
  font-size: var(--omp-fs-sm);
  color: var(--omp-ink);
}

.login-id {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.count {
  font-family: var(--omp-font-mono);
  color: var(--omp-faint);
  letter-spacing: 0;
  text-transform: none;
}

.form {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-3);
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  flex: 1;
}

.field-row {
  display: flex;
  gap: var(--omp-sp-3);
}

.field-label {
  font-size: var(--omp-fs-3xs);
  font-weight: 650;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--omp-faint);
}

.models-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-top: var(--omp-sp-2);
  border-top: 1px solid var(--omp-line);
}

.model-row {
  display: grid;
  grid-template-columns: 1.4fr 1fr 100px 100px auto;
  gap: var(--omp-sp-2);
  align-items: center;
}

.num-input {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-xs);
}
</style>
