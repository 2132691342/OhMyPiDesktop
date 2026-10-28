<script setup lang="ts">
// 一个引擎在「应用」页里的卡片：版本对比、三个动作、安装进度。
//
// 它不认识后端：能点什么由 AppRow 里的 can_* 决定，动作由父组件发。
// 这样「按钮该不该存在」只有一处判断（后端），组件不再做第二次。
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import type { AppRow } from '@/types/api'
import type { AppProgressLog } from '@/stores/app'

const props = defineProps<{
  row: AppRow
  /** 这个引擎当前的长任务现场；没有就是 undefined */
  progress?: AppProgressLog
  /** 这个引擎正在跑（父组件发起的请求还没回来） */
  busy: boolean
  copied: string
}>()

const emit = defineEmits<{
  run: [action: 'install' | 'update' | 'uninstall']
  copy: [text: string]
}>()

const { t } = useI18n()
const open = ref(false)

/** 有现场或正在跑才展开；跑完自动收起。 */
const showProgress = computed(() => Boolean(props.progress || props.busy))

const phaseLabel = computed(() => {
  if (props.progress?.phase === 'fail') return t('apps.phaseFail')
  if (props.busy) return t('apps.phaseRunning')
  return t('apps.phaseDone')
})

/** 未知版本、落后、最新 —— 三种状态各有各的说法，绝不混用。 */
const versionState = computed<'outdated' | 'current' | 'unknown' | 'none'>(() => {
  if (!props.row.installed) return 'none'
  if (props.row.outdated) return 'outdated'
  if (props.row.latest_version) return 'current'
  return 'unknown'
})
</script>

<template>
  <article class="app-card card-pad">
    <div class="app-top">
      <span class="app-badge" :class="{ on: row.installed }">{{ row.app_name }}</span>
      <div class="app-version">
        <span class="ver mono">{{ row.installed ? row.version : t('apps.notInstalled') }}</span>
        <template v-if="versionState === 'outdated'">
          <AppIcon name="arrow-left" :size="12" class="ver-arrow" />
          <span class="ver latest mono" :title="t('apps.latestHint')">{{ row.latest_version }}</span>
          <span class="tag brand">{{ t('apps.updateAvailable') }}</span>
        </template>
        <span v-else-if="versionState === 'unknown'" class="tag" :title="row.latest_note">
          {{ t('apps.latestUnknown') }}
        </span>
        <span v-else-if="versionState === 'current'" class="tag ok">{{ t('apps.upToDate') }}</span>
      </div>
      <span class="push" />
      <div class="app-actions">
        <button
          class="omp-btn primary sm"
          :class="{ 'is-loading': busy }"
          :disabled="!row.can_install || busy"
          @click="emit('run', 'install')"
        >
          <AppIcon name="download" :size="13" />
          {{ row.installed ? t('apps.reinstall') : t('common.install') }}
        </button>
        <button
          v-if="row.installed && row.update_command"
          class="omp-btn sm"
          :class="{ 'is-loading': busy }"
          :disabled="!row.can_update || busy"
          @click="emit('run', 'update')"
        >
          <AppIcon name="refresh" :size="13" />
          {{ t('common.update') }}
        </button>
        <button
          v-if="row.can_uninstall"
          class="omp-btn sm danger"
          :class="{ 'is-loading': busy }"
          :disabled="busy"
          @click="emit('run', 'uninstall')"
        >
          <AppIcon name="trash" :size="13" />
          {{ t('common.uninstall') }}
        </button>
      </div>
    </div>

    <!-- 装着的：给出渠道与落点；没装着的：给出要跑的命令 -->
    <div v-if="row.installed" class="app-facts">
      <span class="fact">
        <span class="fact-label">{{ t('apps.channel') }}</span>
        <span class="fact-value">{{ row.channel_label }}</span>
      </span>
      <span class="fact wide">
        <span class="fact-label">{{ t('apps.location') }}</span>
        <code class="fact-value mono selectable">{{ row.exe }}</code>
      </span>
    </div>
    <div v-else class="cmd-row">
      <code class="cmd-text mono selectable">{{ row.install_command }}</code>
      <button class="omp-btn sm ghost" @click="emit('copy', row.install_command)">
        {{ copied === row.install_command ? t('settings.copied') : t('settings.copyCommand') }}
      </button>
    </div>

    <!-- 「为什么这条按钮不能点」比一个灰按钮有用得多 -->
    <p v-if="row.blocker" class="blocker">
      <AppIcon name="alert" :size="12" />
      {{ row.blocker }}
    </p>

    <div v-if="showProgress" class="progress">
      <button class="progress-head" type="button" @click="open = !open">
        <span class="dot" :class="progress?.phase === 'fail' ? 'bad' : busy ? 'run' : 'ok'" />
        <span class="progress-label">{{ phaseLabel }}</span>
        <span class="push" />
        <AppIcon name="chevron" :size="12" :class="['chev', { open }]" />
      </button>
      <div v-if="open" class="progress-body">
        <code v-if="progress?.command" class="progress-cmd mono selectable">{{ progress.command }}</code>
        <pre class="progress-log selectable">{{ (progress?.lines ?? []).join('\n') }}</pre>
      </div>
    </div>

    <p class="app-foot">
      <a class="docs" :href="row.docs_url" target="_blank" rel="noreferrer">{{ t('apps.docs') }}</a>
    </p>
  </article>
</template>

<style scoped>
.app-card {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-3);
  max-width: 860px;
}

.app-top {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
  flex-wrap: wrap;
}

.app-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 9px;
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r-full);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-2xs);
  font-weight: 700;
  letter-spacing: 0.04em;
  color: var(--omp-mute);
}

.app-badge.on {
  color: var(--omp-brand);
  border-color: var(--omp-brand-3);
  background: var(--omp-brand-soft);
}

.app-version {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex-wrap: wrap;
}

.ver {
  font-size: var(--omp-fs-sm);
  color: var(--omp-ink-5);
}

.ver.latest {
  color: var(--omp-brand);
}

.ver-arrow {
  color: var(--omp-faint);
  transform: rotate(180deg);
}

.app-actions {
  display: flex;
  gap: var(--omp-sp-2);
}

.app-facts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--omp-sp-4);
}

.fact {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  min-width: 0;
}

.fact.wide {
  flex: 1 1 320px;
}

.fact-label {
  font-size: var(--omp-fs-3xs);
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--omp-faint);
}

.fact-value {
  font-size: var(--omp-fs-xs);
  color: var(--omp-ink-5);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mono {
  font-family: var(--omp-font-mono);
}

.cmd-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-2) var(--omp-sp-2) var(--omp-sp-2) var(--omp-sp-3);
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r);
  background: var(--omp-code-bg);
}

.cmd-text {
  flex: 1;
  min-width: 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-ink-5);
  overflow-x: auto;
  white-space: nowrap;
}

.blocker {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  font-size: var(--omp-fs-2xs);
  line-height: 1.6;
  color: var(--omp-warn);
}

/* 进度默认收起：装一个引擎的日志能到几百行，
   常驻展开会把「装的什么版本」挤到看不见的地方。 */
.progress {
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r);
  overflow: hidden;
}

.progress-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: var(--omp-sp-2) var(--omp-sp-3);
  background: transparent;
  border: 0;
  cursor: pointer;
  color: var(--omp-ink-5);
  font-size: var(--omp-fs-2xs);
  transition: background var(--omp-dur-fast) var(--omp-ease);
}

.progress-head:hover {
  background: var(--omp-hover);
}

.progress-label {
  font-weight: 600;
}

.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--omp-faint);
  flex: none;
}

.dot.ok {
  background: var(--omp-ok);
}

.dot.bad {
  background: var(--omp-bad);
}

.dot.run {
  background: var(--omp-brand-2);
  animation: pulse 1.4s var(--omp-ease) infinite;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.35;
  }
}

.chev {
  color: var(--omp-faint);
  transform: rotate(180deg);
  transition: transform var(--omp-dur-fast) var(--omp-ease);
}

.chev.open {
  transform: rotate(0deg);
}

.progress-body {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  padding: 0 var(--omp-sp-3) var(--omp-sp-3);
}

.progress-cmd {
  padding: var(--omp-sp-2);
  border-radius: var(--omp-r-sm);
  background: var(--omp-sunken);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-mute);
  overflow-x: auto;
  white-space: nowrap;
}

.progress-log {
  margin: 0;
  max-height: 220px;
  overflow: auto;
  padding: var(--omp-sp-2);
  border-radius: var(--omp-r-sm);
  background: var(--omp-code-bg);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  line-height: 1.6;
  color: var(--omp-ink-5);
  white-space: pre-wrap;
  word-break: break-all;
}

.app-foot {
  margin: 0;
}

.docs {
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  text-decoration: none;
}

.docs:hover {
  color: var(--omp-brand);
  text-decoration: underline;
}
</style>
