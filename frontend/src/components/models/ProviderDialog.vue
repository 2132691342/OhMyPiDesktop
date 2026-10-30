<script setup lang="ts">
// 服务商编辑弹窗：名 / 地址 / 协议 / 密钥，加一张模型表。
//
// 单独一个组件是因为它有自己的一份草稿（form）与三处就地编辑
// （加模型、删模型、改能力勾选），而那些都只在弹窗打开期间存在。
// 放在父组件里会让「列表状态」和「正在编辑的这一份」挤在一起。
//
// 两个勾选项的语义：
//   - 推理（R）：勾上才把 reasoning 写进配置。默认不写——
//     给非推理模型打开它会让上游报错。
//   - 输入模态（TXT / IMG）：两个逐选而不是自由文本，
//     写错一个词会让配置静默失效。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import type { ModelEntry } from '@/types/api'

/** 草稿形态：与 ProviderView 对齐，但密钥是可以改的空串而不是掩码。 */
export interface ProviderForm {
  name: string
  base_url: string
  api: string
  api_key: string
  models: ModelEntry[]
}

const props = defineProps<{
  modelValue: boolean
  form: ProviderForm
  /** 正在编辑的服务商名（空表示新建） */
  editing: string
  saving: boolean
  /** 正在拉上游清单（true 表示按钮转圈） */
  fetching: boolean
  apiOptions: string[]
}>()

const emit = defineEmits<{
  'update:modelValue': [open: boolean]
  save: []
  fetchModels: []
}>()

const { t } = useI18n()

const open = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v),
})

/** 名字在编辑时不可改：它是配置里的键，改了等于新建一条。 */
const nameLocked = computed(() => Boolean(props.editing))

function addModel(): void {
  // 新建默认 text 输入与一组常见窗口：空着提交会让引擎侧静默失败。
  props.form.models.push({
    model_id: '',
    model_name: '',
    context_window: 128000,
    max_tokens: 8192,
    reasoning: false,
    input: ['text'],
  })
}

function removeModel(index: number): void {
  props.form.models.splice(index, 1)
}

function setReasoning(index: number, on: boolean): void {
  props.form.models[index].reasoning = on
}

function hasInput(model: ModelEntry, kind: string): boolean {
  return Boolean(model.input?.includes(kind))
}

/**
 * 勾 / 取消一种输入模态。
 *
 * 至少保留 text：空数组在引擎那边等价于「什么都收不了」，
 * 而那种配置不会报错、只会静默失败——用户只会看到「模型不回话」。
 */
function setInput(index: number, kind: string, on: boolean): void {
  const model = props.form.models[index]
  let next = (model.input ?? []).filter((k) => k !== kind)
  if (on) next = [...next, kind]
  if (!next.length) next = ['text']
  model.input = next
}
</script>

<template>
  <el-dialog
    v-model="open"
    :title="props.editing ? t('models.editProvider') : t('models.addProvider')"
    width="560px"
  >
    <el-form label-position="top" size="small">
      <el-form-item :label="t('common.name')">
        <el-input v-model="props.form.name" :disabled="nameLocked" placeholder="router" />
      </el-form-item>
      <el-form-item :label="t('models.baseUrl')">
        <el-input v-model="props.form.base_url" placeholder="https://api.example.com/v1" />
      </el-form-item>
      <div class="field-row">
        <el-form-item :label="t('models.api')" class="grow">
          <el-select v-model="props.form.api" style="width: 100%">
            <el-option v-for="option in props.apiOptions" :key="option" :label="option" :value="option" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('models.apiKey')" class="grow">
          <el-input v-model="props.form.api_key" :placeholder="t('models.apiKeyHint')" />
        </el-form-item>
      </div>
    </el-form>

    <div class="models-head">
      <span class="field-label">{{ t('models.modelsLabel') }}</span>
      <span class="sp" />
      <!-- 拉上游清单：填了地址与密钥就能问出有哪些模型，
           不用一个个手打 id。拉回来的清单会填进下方的表里。 -->
      <button
        class="omp-btn sm ghost"
        :class="{ 'is-loading': props.fetching }"
        :disabled="props.fetching || props.saving"
        :title="t('models.fetchModelsHint')"
        @click="emit('fetchModels')"
      >
        <AppIcon name="download" :size="12" />
        {{ t('models.fetchModels') }}
      </button>
      <button class="omp-btn text sm" :disabled="props.fetching" @click="addModel">
        {{ t('models.addModel') }}
      </button>
    </div>

    <!-- 表头：四个字段各自的含义写在列头上，不靠占位符（输入后就消失了） -->
    <div v-if="props.form.models.length" class="model-row model-head">
      <span>{{ t('models.modelId') }}</span>
      <span>{{ t('common.name') }}</span>
      <span>{{ t('models.contextWindow') }}</span>
      <span>{{ t('models.maxTokens') }}</span>
      <span>{{ t('models.reasoning') }}</span>
      <span>{{ t('models.input') }}</span>
      <span />
    </div>

    <div v-for="(model, index) in props.form.models" :key="index" class="model-row">
      <input v-model="model.model_id" class="omp-input" :placeholder="t('models.modelId')" />
      <input v-model="model.model_name" class="omp-input" :placeholder="t('common.name')" />
      <input
        v-model.number="model.context_window"
        class="omp-input num-input"
        type="number"
        :placeholder="t('models.contextWindow')"
      />
      <input
        v-model.number="model.max_tokens"
        class="omp-input num-input"
        type="number"
        :placeholder="t('models.maxTokens')"
      />
      <label class="cap-check" :title="t('models.reasoningHint')">
        <input
          type="checkbox"
          :checked="Boolean(model.reasoning)"
          @change="(e: Event) => setReasoning(index, (e.target as HTMLInputElement).checked)"
        />
        <span>R</span>
      </label>
      <div class="cap-inputs">
        <label class="cap-check" :title="t('models.inputText')">
          <input
            type="checkbox"
            :checked="hasInput(model, 'text')"
            @change="(e: Event) => setInput(index, 'text', (e.target as HTMLInputElement).checked)"
          />
          <span>TXT</span>
        </label>
        <label class="cap-check" :title="t('models.inputImage')">
          <input
            type="checkbox"
            :checked="hasInput(model, 'image')"
            @change="(e: Event) => setInput(index, 'image', (e.target as HTMLInputElement).checked)"
          />
          <span>IMG</span>
        </label>
      </div>
      <button
        class="omp-btn icon ghost danger"
        :title="t('common.delete')"
        @click="removeModel(index)"
      >
        <AppIcon name="close" :size="13" />
      </button>
    </div>

    <template #footer>
      <button class="omp-btn" @click="open = false">{{ t('common.cancel') }}</button>
      <button
        class="omp-btn primary"
        :class="{ 'is-loading': props.saving }"
        :disabled="props.saving || !props.form.name.trim()"
        @click="emit('save')"
      >
        {{ t('common.save') }}
      </button>
    </template>
  </el-dialog>
</template>

<style scoped>
.field-row {
  display: flex;
  gap: var(--omp-sp-3);
}

.grow {
  flex: 1;
  min-width: 0;
}

.models-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--omp-sp-2);
  margin: var(--omp-sp-2) 0;
}

.sp {
  flex: 1;
}

.field-label {
  font-size: var(--omp-fs-3xs);
  font-weight: 700;
  letter-spacing: var(--omp-ls-plate);
  text-transform: uppercase;
  color: var(--omp-faint);
}

.model-row {
  display: grid;
  grid-template-columns: 1.4fr 1fr 88px 80px 40px 78px 28px;
  gap: var(--omp-sp-2);
  align-items: center;
  margin-bottom: var(--omp-sp-2);
}

/* 表头那一行没有输入框，靠文字说明每一列是什么 */
.model-head {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  margin-bottom: var(--omp-sp-1);
}

.model-row .omp-input {
  min-width: 0;
  height: var(--omp-ctl-sm);
  font-size: var(--omp-fs-2xs);
}

.num-input {
  text-align: right;
  font-family: var(--omp-font-mono);
}

.cap-check {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-mute);
  cursor: pointer;
  user-select: none;
}

.cap-inputs {
  display: flex;
  gap: var(--omp-sp-2);
}

/* 窄一点：弹窗宽 560px，七列挤不下时先让名字列让位 */
@media (max-width: 620px) {
  .model-row {
    grid-template-columns: 1.2fr 1fr 72px 64px 34px 70px 28px;
  }
}
</style>
