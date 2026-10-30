<script setup lang="ts">
// 动手之前的确认：把那行命令摆出来。
//
// 用户点的是「升级 / 卸载」，但真正发生的是这条命令。
// 改写系统的事，用户有权先看到它——不摆出来就成了「你点了我就动手」。
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  action: 'update' | 'uninstall'
  /** 将要执行的那条命令原文 */
  command: string
}>()

const emit = defineEmits<{ cancel: []; confirm: [] }>()

const { t } = useI18n()
</script>

<template>
  <div class="confirm">
    <div class="confirm-head">
      {{ props.action === 'update' ? t('apps.confirmUpdate') : t('apps.confirmUninstall') }}
    </div>
    <code v-if="props.command" class="confirm-cmd omp-mono selectable">{{ props.command }}</code>
    <p class="confirm-hint">
      {{ props.action === 'update' ? t('apps.confirmUpdateHint') : t('apps.confirmUninstallHint') }}
    </p>
    <div class="confirm-actions">
      <button class="omp-btn sm ghost" @click="emit('cancel')">{{ t('common.cancel') }}</button>
      <button
        class="omp-btn sm"
        :class="props.action === 'uninstall' ? 'danger' : 'primary'"
        @click="emit('confirm')"
      >
        {{ props.action === 'update' ? t('common.update') : t('common.uninstall') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.confirm {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-3);
  border: 1px solid var(--omp-warn);
  border-radius: var(--omp-r);
  background: var(--omp-warn-soft);
}

.confirm-head {
  font-size: var(--omp-fs-2xs);
  color: var(--omp-ink);
}

.confirm-cmd {
  padding: 2px 6px;
  border-radius: var(--omp-r-xs);
  background: var(--omp-surface);
  border: 1px solid var(--omp-line);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-ink-5);
  overflow-wrap: anywhere;
}

.confirm-hint {
  margin: 0;
  font-size: var(--omp-fs-3xs);
  line-height: 1.6;
  color: var(--omp-mute);
}

.confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--omp-sp-2);
}
</style>
