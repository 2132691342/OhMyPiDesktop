<script setup lang="ts">
// 输入框底栏：一行。
//
// 从左到右是「这句话的参数」（工作目录 → 引擎 → 模型 → 思考档），
// 随后是两个低频开关（附件、自动压缩），行末是读数与动作
// （上下文水位 …… 排队 / 停止 / 发送）。新对话不在这一行：
// 它属于会话列表，在左侧列表顶部。
//
// 为什么是一行而不是两行：参数与动作本就是「同一句话的属性」，
// 拆成两行等于把一句话的定语另起一行。发送恒在行末，
// 其余控件各自的省略与 vw-scale 收缩负责让一行装得下。
//
// 底栏的读数（上下文水位）现在是可点的（有详情弹层），但仍不套描边胶囊：
// 它的主要身份还是读数，可点的那部分留给它自己的弹层。
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import ContextMeter from '@/components/chat/ContextMeter.vue'
import EngineChip from '@/components/chat/EngineChip.vue'
import ModelPicker from '@/components/chat/ModelPicker.vue'
import ThinkingChip from '@/components/chat/ThinkingChip.vue'
import WorkspaceChip from '@/components/chat/WorkspaceChip.vue'
import { useChatStore } from '@/stores/chat'
import { useAppStore } from '@/stores/app'

const props = defineProps<{
  /** 有没有内容可发（正文或附件） */
  canSend: boolean
  sending: boolean
  queueing: boolean
  stopping: boolean
  /** 正在执行的内置命令（为空表示空闲） */
  busyCmd: string
  /** 当前模型收不收图片 */
  imagesAllowed: boolean
  /** 字符计数（0 时不显示；直播中也不显示——行末那几位要留给动作） */
  count: number
}>()

const emit = defineEmits<{
  browseFiles: []
  queue: []
  toggleCompact: [enabled: boolean]
  stop: []
  submit: []
}>()

const chat = useChatStore()
const app = useAppStore()
const { t } = useI18n()
</script>

<template>
  <div class="composer-bar">
    <div class="bar-row">
      <WorkspaceChip />
      <EngineChip />
      <ModelPicker />
      <ThinkingChip />

      <button
        class="omp-btn icon ghost"
        :title="props.imagesAllowed ? t('chat.attachPick') : t('chat.attachNoImageModel')"
        :disabled="!app.hasEngine || !props.imagesAllowed"
        @click="emit('browseFiles')"
      >
        <AppIcon name="file" :size="14" />
      </button>

      <button
        class="omp-btn icon ghost"
        :class="{ on: chat.autoCompaction, 'is-loading': chat.switching === 'compaction' }"
        :disabled="chat.busy || chat.switching === 'compaction' || !app.hasEngine"
        :title="chat.autoCompaction ? t('chat.autoCompactOn') : t('chat.autoCompactOff')"
        @click="emit('toggleCompact', !chat.autoCompaction)"
      >
        <AppIcon name="refresh" :size="13" />
      </button>

      <span v-if="props.count && !chat.streaming" class="composer-hint">{{ props.count }}</span>
      <span class="bar-sp" />

      <ContextMeter />

      <button
        v-if="chat.streaming"
        class="omp-btn sm ghost"
        :class="{ 'is-loading': props.queueing }"
        :disabled="!props.canSend || props.queueing"
        :title="t('chat.queueHint')"
        @click="emit('queue')"
      >
        <AppIcon name="clock" :size="13" />
        {{ t('chat.queue') }}
      </button>

      <button
        v-if="chat.streaming"
        class="omp-btn sm"
        :class="{ 'is-loading': props.stopping }"
        :disabled="props.stopping"
        :title="t('chat.stop')"
        @click="emit('stop')"
      >
        <AppIcon name="stop" :size="13" />
        {{ t('chat.stop') }}
      </button>

      <button
        class="omp-btn primary sm"
        :class="{ 'is-loading': props.sending || props.busyCmd !== '' }"
        :disabled="!props.canSend || !app.hasEngine"
        @click="emit('submit')"
      >
        <AppIcon name="send" :size="13" />
        {{ chat.streaming ? t('chat.sendSteer') : t('chat.send') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.composer-bar {
  min-width: 0;
}

/* 一行：不换行，超出的部分交给各自的省略号。
   换行会让「发送」按钮的位置随内容长短漂移。 */
.bar-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  min-width: 0;
}

/* 芯片不参与收缩：它们自己管截断，被父级压窄时反而会把图标挤掉 */
.bar-row > * {
  flex: none;
}

.bar-sp {
  flex: 1;
  min-width: var(--omp-sp-1);
}

.composer-hint {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  font-family: var(--omp-font-mono);
  font-variant-numeric: tabular-nums;
}

/* 自动压缩是个开关：开着的时候给它一点强调，否则「开着」和「关着」看起来一样 */
.omp-btn.on {
  color: var(--omp-brand);
  border-color: var(--omp-brand-line);
  background: var(--omp-brand-soft);
}
</style>
