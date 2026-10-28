<script setup lang="ts">
// 输入区的工具条：新建 / 附加 / 模型 / 思考档 / 排队 / 自动压缩 / 停止 / 发送。
//
// 为什么单独一个文件：它是一排并列的控件，每个都有自己的 loading 与禁用判据，
// 全塞进输入区会让那个文件既管键盘又管六个下拉的可用性。
// 这里只负责「渲染 + 把点击交上去」，不做任何决策。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
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
  pickModel: [provider: string, id: string]
  pickThinking: [level: string]
  queue: []
  toggleCompact: [enabled: boolean]
  stop: []
  submit: []
}>()

const chat = useChatStore()
const app = useAppStore()
const { t } = useI18n()

/** 任一动作在进行中：切模型 / 切思考档 / 新建 / 压缩。 */
const busy = computed(() => Boolean(chat.switching))
</script>

<template>
  <div class="composer-bar">
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

    <el-dropdown trigger="click" max-height="360px" :disabled="!chat.models.length">
      <button
        class="omp-btn sm ghost"
        :class="{ 'is-loading': chat.switching === 'model' }"
        :disabled="!chat.model || busy"
      >
        <AppIcon name="cpu" :size="13" />
        <span class="model-label">{{ chat.model?.id || t('common.unknown') }}</span>
      </button>
      <template #dropdown>
        <el-dropdown-menu>
          <el-dropdown-item
            v-for="m in chat.models"
            :key="`${m.provider}/${m.id}`"
            @click="emit('pickModel', m.provider, m.id)"
          >
            <span class="drop-row">
              <span class="drop-id">{{ m.id }}</span>
              <span class="drop-prov">{{ m.provider }}</span>
              <span
                v-if="m.provider === chat.model?.provider && m.id === chat.model?.id"
                class="tag brand"
              >
                {{ t('chat.current') }}
              </span>
            </span>
          </el-dropdown-item>
          <el-dropdown-item v-if="!chat.models.length" disabled>
            {{ t('chat.noModels') }}
          </el-dropdown-item>
        </el-dropdown-menu>
      </template>
    </el-dropdown>

    <el-dropdown trigger="click" max-height="320px" :disabled="!chat.levels.length">
      <button
        class="omp-btn sm ghost"
        :class="{ 'is-loading': chat.switching === 'thinking' }"
        :disabled="!chat.levels.length || busy"
      >
        <AppIcon name="spark" :size="13" />
        <span class="model-label">{{ chat.thinkingLevel || t('chat.thinkingDefault') }}</span>
      </button>
      <template #dropdown>
        <el-dropdown-menu>
          <el-dropdown-item v-for="level in chat.levels" :key="level" @click="emit('pickThinking', level)">
            <span class="drop-row">
              <span>{{ level }}</span>
              <span v-if="level === chat.thinkingLevel" class="tag brand">{{ t('chat.current') }}</span>
            </span>
          </el-dropdown-item>
        </el-dropdown-menu>
      </template>
    </el-dropdown>

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
      class="omp-btn sm ghost"
      :class="{ on: chat.autoCompaction, 'is-loading': chat.switching === 'compaction' }"
      :disabled="chat.busy || busy || !app.hasEngine"
      :title="t('chat.autoCompact')"
      @click="emit('toggleCompact', !chat.autoCompaction)"
    >
      <AppIcon name="refresh" :size="13" />
      {{ t('chat.autoCompact') }}
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

    <span class="composer-hint">{{ props.count ? String(props.count) : '' }}</span>
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
</template>

<style scoped>
.composer-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--omp-sp-2);
}

.composer-hint {
  flex: 1;
  text-align: right;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  font-family: var(--omp-font-mono);
}

.model-label {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--omp-font-mono);
}

.drop-id {
  font-family: var(--omp-font-mono);
}

.drop-prov {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  font-family: var(--omp-font-mono);
}

.drop-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--omp-sp-4);
  min-width: 120px;
}

/* 窄窗口（800×600）下工具条挤不下：模型名与思考档只留图标。 */
@container app (max-width: 820px) {
  .model-label {
    display: none;
  }
}
</style>
