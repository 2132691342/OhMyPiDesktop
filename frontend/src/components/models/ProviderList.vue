<script setup lang="ts">
// 服务商清单：一张卡一条，卡上是它落盘的事实（地址 / 协议 / 有没有密钥 / 模型）。
// 密钥只显示「有没有」（引擎打过码，再解一层等于把凭据摆上屏幕）；
// 卡带引擎标签（两引擎同名各一份，删时才知道删哪份）；拉模型只在编辑弹窗里做。
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import type { ProviderView } from '@/types/api'

const props = defineProps<{
  providers: ProviderView[]
  loading: boolean
  /** 配置文件路径，显示在标题右侧 */
  configPath: string
  /** 正在跑的动作名（'test:<服务商>'）；空表示空闲 */
  pending: string
}>()

const emit = defineEmits<{
  create: []
  edit: [view: ProviderView]
  remove: [view: ProviderView]
  test: [view: ProviderView]
}>()

const { t } = useI18n()

/** 每个卡是否展开了全部模型。默认只列前六个。 */
const expanded = ref<Set<string>>(new Set())

/** 一张卡的键：两个引擎可能同名。 */
function keyOf(view: ProviderView): string {
  return `${view.engine_kind}:${view.provider_name}`
}

const SHOWN = 6

/** 卡片上列几个模型：展开时全列，否则前六个。 */
function shownModels(view: ProviderView) {
  return expanded.value.has(keyOf(view)) ? view.models : view.models.slice(0, SHOWN)
}

function restCount(view: ProviderView): number {
  if (expanded.value.has(keyOf(view))) return 0
  return Math.max(0, view.models.length - SHOWN)
}

function toggleMore(view: ProviderView): void {
  const next = new Set(expanded.value)
  const key = keyOf(view)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  expanded.value = next
}

/** 某个服务商的地址里的主机名：卡片上比整条地址好读。 */
function host(url: string): string {
  if (!url) return '—'
  try {
    return new URL(url).host
  } catch {
    return url
  }
}
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
        class="provider-card"
      >
        <!-- 卡头：名字 + 引擎标签 + 密钥状态。
             名字与主机名占一行，下面才是事实字段——层级清楚，
             扫一眼就知道「哪个服务商、连到哪、能不能用」。 -->
        <header class="provider-head">
          <span class="provider-avatar">{{ view.provider_name.slice(0, 1).toUpperCase() }}</span>
          <div class="provider-title">
            <span class="provider-name">{{ view.provider_name }}</span>
            <span class="provider-host mono">{{ host(view.base_url) }}</span>
          </div>
          <span class="tag engine-tag">{{ view.engine_kind }}</span>
        </header>

        <div class="provider-facts">
          <span class="fact">
            <span class="fact-label">{{ t('models.api') }}</span>
            <span class="fact-value mono">{{ view.api || '—' }}</span>
          </span>
          <span class="fact">
            <span class="fact-label">{{ t('models.apiKey') }}</span>
            <span class="fact-value">
              <span class="tag" :class="view.has_key ? 'ok' : 'warn'">
                {{ view.has_key ? t('models.keyHidden') : t('models.apiKeyMissing') }}
              </span>
            </span>
          </span>
        </div>

        <!-- 模型：默认只列前六个，多了给一个可点的 +N。
             全列出来会让一张卡被几十个标签撑开，而这一栏只是回答问题
             「这个服务商底下大概是什么规模」。 -->
        <div class="model-chips">
          <span v-for="model in shownModels(view)" :key="model.model_id" class="tag model-chip">
            {{ model.model_id }}
            <!-- 能力标记：选模型时真会看的两个属性，直接标在 id 旁边 -->
            <b v-if="model.reasoning" class="chip-cap" :title="t('models.reasoning')">R</b>
            <b v-if="model.input?.includes('image')" class="chip-cap" :title="t('models.inputImage')">IMG</b>
          </span>
          <button v-if="restCount(view)" class="tag more-chip" type="button" @click="toggleMore(view)">
            +{{ restCount(view) }}
          </button>
          <button
            v-else-if="view.models.length > SHOWN"
            class="tag more-chip"
            type="button"
            @click="toggleMore(view)"
          >
            {{ t('models.collapseModels') }}
          </button>
          <span v-if="!view.models.length" class="tag warn">{{ t('models.addModel') }}</span>
        </div>

        <footer class="provider-actions">
          <button
            class="omp-btn sm ghost"
            :class="{ 'is-loading': props.pending === `test:${view.provider_name}` }"
            :disabled="Boolean(props.pending)"
            :title="t('models.testProvider')"
            @click="emit('test', view)"
          >
            <AppIcon name="check" :size="12" />
            {{ t('models.testProvider') }}
          </button>
          <span class="sp" />
          <button class="omp-btn text sm" @click="emit('edit', view)">{{ t('common.edit') }}</button>
          <button class="omp-btn text sm danger" @click="emit('remove', view)">
            {{ t('common.delete') }}
          </button>
        </footer>
      </article>
    </div>
  </section>
</template>

<style scoped>
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: var(--omp-sp-3);
}

/* 一张卡：淡底 + 描边 + 极轻投影。
   悬停时投影略抬、描边变深——它是一个可以点的对象，
   但不缩放（缩放会让整排卡片一起抖）。 */
.provider-card {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-3);
  padding: var(--omp-sp-4);
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r-lg);
  background: var(--omp-surface);
  box-shadow: var(--omp-card-shadow);
  transition:
    border-color var(--omp-dur) var(--omp-ease),
    box-shadow var(--omp-dur) var(--omp-ease);
}

.provider-card:hover {
  border-color: var(--omp-line-strong);
  box-shadow: var(--omp-card-shadow-hover);
}

.provider-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
  min-width: 0;
}

/* 厂商头像：首字母方块。颜色统一走品牌淡底，
   不给每个厂商一个专属色——那会引入第六种颜色源。 */
.provider-avatar {
  width: var(--omp-tile-sm);
  height: var(--omp-tile-sm);
  flex: none;
  display: grid;
  place-items: center;
  border-radius: var(--omp-r-sm);
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
  font-size: var(--omp-fs-sm);
  font-weight: 700;
}

.provider-title {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.provider-name {
  font-family: var(--omp-font-display);
  font-size: var(--omp-fs-base);
  font-weight: 650;
  color: var(--omp-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 主机名：比整条地址好读，完整地址在编辑弹窗里看 */
.provider-host {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 引擎标签：两个引擎的配置完全独立，同名可以各有一份 */
.engine-tag {
  flex: none;
  font-family: var(--omp-font-mono);
}

.provider-facts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--omp-sp-2) var(--omp-sp-5);
}

.fact {
  display: inline-flex;
  align-items: center;
  gap: var(--omp-sp-2);
  min-width: 0;
}

.fact-label {
  flex: none;
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
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
}

.more-chip {
  font-family: var(--omp-font-mono);
  cursor: pointer;
  transition:
    background-color var(--omp-dur-fast) var(--omp-ease),
    color var(--omp-dur-fast) var(--omp-ease);
}

.more-chip:hover {
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
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
  align-items: center;
  gap: var(--omp-sp-2);
  margin-top: auto;
  padding-top: var(--omp-sp-1);
}

.sp {
  flex: 1;
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
