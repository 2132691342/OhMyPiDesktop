<script setup lang="ts">
// 一轮消息：角色头 + 若干块（正文 / 思考 / 工具卡）。
//
// 角色头是**可选**的：同一角色连续好几轮时只出一次头。
//
// 为什么：助手常常连着说好几轮（先自言自语、再改两个文件、最后总结），
// 每轮都顶一行「assistant」会把一次回答切成几块，
// 读的人得自己去想「这几段其实是一件事」。消掉重复头之后，
// 同一次回答在视觉上就是一坨。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import TurnBlocks from '@/components/chat/TurnBlocks.vue'
import MarkdownBlock from '@/components/chat/MarkdownBlock.vue'
import type { ChatBlock, EngineKind } from '@/types/api'

const props = defineProps<{
  /** 这一轮的角色 */
  role: string
  /** 这一轮的若干块 */
  blocks: ChatBlock[]
  /** 稳定前缀，TurnBlocks 用它做增量渲染的缓存键 */
  runId: string
  /** 模型名：只在出头的那一轮显示，接着往下说时不必重复 */
  model?: string
  /** 流式渲染最后一块 */
  streaming?: boolean
  /**
   * 上一轮是不是同一个角色。
   *
   * 为真时这一轮不出头、也不给上外边距——它与上一轮在视觉上是同一坨。
   * 由父组件算而不是自己看 index==0：列表里还有「切引擎分隔」这种
   * 不是消息的行，index 相邻的未必属于同一段对话。
   */
  continued: boolean
  /** 流式进行中的当前轮 */
  live?: boolean
  /** 这一轮出错了 */
  errored?: boolean
}>()

const { t } = useI18n()

const isUser = computed(() => props.role === 'user')

/** 切引擎的分隔行：它不是消息，只是界面层的标记。 */
const handoffEngine = computed<EngineKind | ''>(() =>
  props.role === 'engine-mark' ? ((props.blocks[0]?.text ?? '') as EngineKind) : '',
)
</script>

<template>
  <!-- 切引擎分隔线：说明下面的内容来自另一个引擎、另一条会话 -->
  <div v-if="handoffEngine" class="mark-line">
    <span class="mark-text">{{ t('chat.engineHandoff', { engine: handoffEngine }) }}</span>
  </div>

  <article
    v-else
    class="msg"
    :class="{
      user: isUser,
      'is-continued': props.continued,
      'msg-streaming': props.live,
      'msg-error': props.errored,
    }"
  >
    <div v-if="!props.continued" class="msg-head">
      <span class="msg-badge" :class="{ user: isUser }">{{ isUser ? 'you' : 'assistant' }}</span>
      <span v-if="props.model" class="msg-meta">{{ props.model }}</span>
    </div>
    <div class="msg-body">
      <!-- 用户轮只有一段纯文本，不需要 TurnBlocks 那套工具卡分派 -->
      <MarkdownBlock
        v-if="isUser"
        :text="props.blocks[0]?.text ?? ''"
        :streaming="false"
        :cache-key="`${props.runId}:user`"
      />
      <TurnBlocks v-else :blocks="props.blocks" :run-id="props.runId" :streaming="props.streaming" />
    </div>
  </article>
</template>

<style scoped>
.msg {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-1);
}

/* 连续同一角色：取消与上一条之间的间距，视觉上合成一坨。
   头与正文之间的间距留着——那一坨内部还是看得出结构。 */
.msg.is-continued {
  margin-top: calc(-1 * var(--omp-sp-2));
}

.msg-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
}

.msg-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 1px 7px;
  border-radius: var(--omp-r-full);
  background: var(--omp-hover);
  color: var(--omp-mute);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  font-weight: 700;
  letter-spacing: 0.04em;
}

.msg-badge.user {
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
}

.msg-meta {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  font-family: var(--omp-font-mono);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.msg-body {
  min-width: 0;
}

/* 流式中的那一轮：左边一条细标记，说明「这段还在长」 */
.msg-streaming .msg-body {
  border-left: 2px solid var(--omp-brand-2);
  padding-left: var(--omp-sp-3);
  margin-left: 1px;
}

.msg-error .msg-body {
  border-left-color: var(--omp-bad);
}

.mark-line {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
  margin: var(--omp-sp-4) 0;
  color: var(--omp-faint);
}

.mark-line::before,
.mark-line::after {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--omp-line);
}

.mark-text {
  flex: none;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  letter-spacing: 0.04em;
}
</style>
