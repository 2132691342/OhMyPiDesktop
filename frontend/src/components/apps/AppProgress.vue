<script setup lang="ts">
// 一条长任务的实时进度：命令原文 + 逐行输出，可折叠。
// 默认收起：日志几百行，用户要看的是「跑到哪一步」与最后几行；
// 失败也不自动展开——那会把「重试」按钮挤出一屏滚动的黑框。
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import type { AppProgressLog } from '@/stores/app'

const props = defineProps<{
  progress: AppProgressLog | null
  /** 有长任务在跑（决定指示灯颜色） */
  busy: boolean
}>()

const { t } = useI18n()

const open = ref(false)

/** 阶段话术：执行中 / 成功 / 失败。 */
const label = computed(() => {
  if (props.progress?.phase === 'fail') return t('apps.phaseFailed')
  if (props.busy) return t('apps.phaseRunning')
  return t('apps.phaseDone')
})
</script>

<template>
  <div class="progress">
    <button class="progress-head" type="button" @click="open = !open">
      <span class="dot" :class="props.progress?.phase === 'fail' ? 'bad' : props.busy ? 'run' : 'ok'" />
      <span class="progress-label">{{ label }}</span>
      <span class="push" />
      <AppIcon name="chevron" :size="12" :class="['chev', { open }]" />
    </button>
    <div v-if="open" class="progress-body">
      <code v-if="props.progress?.command" class="progress-cmd mono selectable">
        {{ props.progress.command }}
      </code>
      <pre class="progress-log selectable">{{ (props.progress?.lines ?? []).join('\n') }}</pre>
    </div>
  </div>
</template>

<style scoped>
.progress {
  display: flex;
  flex-direction: column;
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
  border: 0;
  background: var(--omp-window);
  color: var(--omp-ink-2);
  font: inherit;
  font-size: var(--omp-fs-2xs);
  text-align: left;
  cursor: pointer;
  transition: background-color var(--omp-dur-fast) var(--omp-ease);
}

.progress-head:hover {
  background: var(--omp-hover);
}

.progress-label {
  font-weight: 600;
}

.push {
  flex: 1;
}

.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
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
  animation: omp-breathe 1.4s var(--omp-ease) infinite;
}

.chev {
  flex: none;
  color: var(--omp-faint);
  transition: transform var(--omp-dur-fast) var(--omp-ease);
}

.chev.open {
  transform: rotate(90deg);
}

.progress-body {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-1);
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border-top: 1px solid var(--omp-line);
  background: var(--omp-code-bg);
}

.progress-cmd {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-brand);
  overflow-wrap: anywhere;
}

.progress-log {
  margin: 0;
  max-height: 200px;
  overflow: auto;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  line-height: 1.55;
  color: var(--omp-ink-5);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
