<script setup lang="ts">
// 服务商清单：一张卡一条，卡上是它落盘的事实（地址 / 协议 / 有没有密钥 / 模型）。
//
// 密钥只显示「有没有」，不显示原值：那是引擎打过码之后给回来的，
// 界面再解一层等于把凭据摆到屏幕上。
//
// 卡上带引擎标签是因为两个引擎的配置完全独立，同名可以各有一份——
// 不标出来，删的时候不知道删的是哪一份。
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import type { ProviderView } from '@/types/api'

const props = defineProps<{
  providers: ProviderView[]
  loading: boolean
  /** 配置文件路径，显示在标题右侧 */
  configPath: string
}>()

const emit = defineEmits<{
  create: []
  edit: [view: ProviderView]
  remove: [view: ProviderView]
}>()

const { t } = useI18n()
</script>

<template>
  <section>
    <div class="omp-sec-head">
      <span class="omp-sec-title">
        {{ t('models.providers') }} <span class="count">{{ props.providers.length }}</span>
      </span>
      <span v-if="props.configPath" class="omp-mono registry">{{ props.configPath }}</span>
    </div>

    <div v-if="!props.providers.length && !props.loading" class="omp-empty">
      <span class="omp-tile omp-tile--lg omp-tile--neutral"><AppIcon name="cpu" :size="22" /></span>
      <p class="omp-empty-title">{{ t('models.emptyTitle') }}</p>
      <p class="omp-empty-hint">{{ t('models.emptyHint') }}</p>
      <button class="omp-btn sm primary" @click="emit('create')">{{ t('models.addProvider') }}</button>
    </div>

    <div v-else class="grid">
      <article
        v-for="view in props.providers"
        :key="`${view.engine_kind}:${view.provider_name}`"
        class="provider-card card-pad"
      >
        <div class="provider-head">
          <span class="provider-name">{{ view.provider_name }}</span>
          <span class="tag engine-tag">{{ view.engine_kind }}</span>
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
          <div v-if="view.api_key" class="fact">
            <span class="fact-label">{{ t('models.apiKey') }}</span>
            <span class="fact-value mono">{{ view.api_key }}</span>
          </div>
        </div>

        <div class="model-chips">
          <span v-for="model in view.models" :key="model.model_id" class="tag model-chip">
            {{ model.model_id }}
            <!-- 能力标记：选模型时真会看的两个属性，直接标在 id 旁边 -->
            <b v-if="model.reasoning" class="chip-cap" :title="t('models.reasoning')">R</b>
            <b v-if="model.input?.includes('image')" class="chip-cap" :title="t('models.inputImage')">IMG</b>
          </span>
          <span v-if="!view.models?.length" class="tag warn">{{ t('models.addModel') }}</span>
        </div>

        <div class="provider-actions">
          <button class="omp-btn text sm" @click="emit('edit', view)">{{ t('common.edit') }}</button>
          <button class="omp-btn text sm danger" @click="emit('remove', view)">
            {{ t('common.delete') }}
          </button>
        </div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: var(--omp-sp-3);
}

.provider-card {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
}

.provider-card:hover {
  border-color: var(--omp-line-strong);
}

.provider-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex-wrap: wrap;
}

.provider-name {
  font-family: var(--omp-font-display);
  font-size: var(--omp-fs-base);
  font-weight: 650;
  color: var(--omp-ink);
}

/* 引擎标签：两个引擎的配置完全独立，同名可以各有一份 */
.engine-tag {
  font-family: var(--omp-font-mono);
}

.provider-facts {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.fact {
  display: flex;
  align-items: baseline;
  gap: var(--omp-sp-2);
  min-width: 0;
}

.fact-label {
  flex: none;
  width: 76px;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.fact-value {
  min-width: 0;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-ink-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.model-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.model-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-family: var(--omp-font-mono);
}

/* 能力标记：两个字母的窄条，比图标省位置且不需要额外解释 */
.chip-cap {
  padding: 0 3px;
  border-radius: var(--omp-r-xs);
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
  font-size: var(--omp-fs-3xs);
  font-weight: 700;
}

.provider-actions {
  display: flex;
  gap: var(--omp-sp-2);
  margin-top: auto;
}

.count {
  font-family: var(--omp-font-mono);
  color: var(--omp-faint);
  letter-spacing: 0;
  text-transform: none;
}

.registry {
  margin-left: auto;
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.omp-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-8);
  text-align: center;
}

.omp-empty-title {
  margin: 0;
  font-size: var(--omp-fs-sm);
  color: var(--omp-ink);
}

.omp-empty-hint {
  margin: 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-faint);
}
</style>
