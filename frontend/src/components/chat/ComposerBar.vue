<script setup lang="ts">
// 输入框底栏：两行。
//
//  第一行是「这句话的参数」：工作目录 → 引擎 → 模型 → 思考档 …… 上下文水位；
//  第二行是动作：新建 / 附件 / 自动压缩 / 排队 …… 停止 / 发送。
//
// 为什么拆成两行而不是一行自适应换行：一行时窗口稍窄就把水位挤到下一行、
// 把发送按钮推到中间，位置每天都不一样。两行之后左对齐的一列恒在左边，
// 动作恒在右边，用户不需要每次重新找按钮。
//
// 底栏的读数（上下文水位）不套描边胶囊：它只读，有盒子又没动作等于请用户
// 去点一个点不动的东西。
import { computed } from 'vue'
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
  /** 字符计数（0 时不显示） */
  count: number
}>()

const emit = defineEmits<{
  newChat: []
  browseFiles: []
  queue: []
  toggleCompact: [enabled: boolean]
  stop: []
  submit: []
}>()

const chat = useChatStore()
const app = useAppStore()
const { t } = useI18n()

/** 任一动作在进行中：新建 / 压缩。 */
const busy = computed(() => Boolean(chat.switching))
</script>

<template>
  <div class="composer-bar">
    <!-- 第一行：参数。水位贴在行末，它属于这一行的读数 -->
    <div class="bar-row">
      <WorkspaceChip />
      <EngineChip />
      <ModelPicker />
      <ThinkingChip />
      <span class="bar-sp" />
      <ContextMeter />
    </div>

    <!-- 第二行：动作 -->
    <div class="bar-row bar-actions">
      <button
        class="omp-btn icon ghost"
        :class="{ 'is-loading': chat.switching === 'new' }"
        :disabled="busy"
        :title="t('chat.newChat')"
        @click="emit('newChat')"
      >
        <AppIcon name="plus" :size="15" />
      </button>

      <button
        class="omp-btn icon ghost"
        :title="props.imagesAllowed ? t('chat.attachPick') : t('chat.attachNoImageModel')"
        :disabled="!app.hasEngine || !props.imagesAllowed"
        @click="emit('browseFiles')"
      >
        <AppIcon name="file" :size="14" />
      </button>

      <button
        class="omp-btn sm ghost"
        :class="{ on: chat.autoCompaction, 'is-loading': chat.switching === 'compaction' }"
        :disabled="chat.busy || busy || !app.hasEngine"
        :title="t('chat.autoCompact')"
        @click="emit('toggleCompact', !chat.autoCompaction)"
      >
        <AppIcon name="refresh" :size="13" />
        {{ t('chat.autoCompact') }}
      </button>

      <span v-if="props.count" class="composer-hint">{{ props.count }}</span>
      <span class="bar-sp" />

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
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-1);
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

.bar-actions {
  padding-top: var(--omp-sp-1);
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
