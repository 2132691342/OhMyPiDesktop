<script setup lang="ts">
// 一轮消息：角色头 + 若干块（正文 / 思考 / 工具卡）。
//
// 角色头是**可选**的：同一角色连续好几轮时只出一次头。
//
// 为什么：助手常常连着说好几轮（先自言自语、再改两个文件、最后总结），
// 每轮都顶一行「assistant」会把一次回答切成几块，
// 读的人得自己去想「这几段其实是一件事」。消掉重复头之后，
// 同一次回答在视觉上就是一坨。
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import TurnBlocks from '@/components/chat/TurnBlocks.vue'
import MarkdownBlock from '@/components/chat/MarkdownBlock.vue'
import AppIcon from '@/components/common/AppIcon.vue'
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

/** 这一轮的纯文本（复制用）。 */
const plainText = computed(() =>
  props.blocks
    .filter((b) => b.kind === 'text' && b.text)
    .map((b) => b.text)
    .join('\n\n'),
)

const copied = ref(false)

/**
 * 复制这一轮的回答。
 *
 * 放在轮尾而不是每一段上：用户要的通常是「这次回答的全文」，
 * 而逐段复制会让他按段拼一遍。按钮只在悬停时出现，平时不占视觉重量。
 */
async function copyTurn(): Promise<void> {
  if (!plainText.value.trim()) return
  try {
    await navigator.clipboard.writeText(plainText.value)
    copied.value = true
    window.setTimeout(() => {
      copied.value = false
    }, 1400)
  } catch {
    /* 剪贴板不可用时不打扰：用户可以手动选中 */
  }
}
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

      <!-- 轮尾动作：悬停才出现，平时不占位置 -->
      <div v-if="!isUser" class="msg-acts">
        <button class="msg-act" type="button" :disabled="copied" @click="copyTurn">
          <AppIcon :name="copied ? 'check' : 'file'" :size="11" />
          {{ copied ? t('common.copied') : t('common.copy') }}
        </button>
      </div>
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
  margin-top: calc(-1 * var(--omp-sp-3));
}

.msg-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
}

/* 角色徽章：一行人名，不画胶囊。
   长会话里每一轮都顶一个描边胶囊时，一屏会多出十几个相同形状的框。 */
.msg-badge {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--omp-faint);
}

.msg-badge.user {
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

/* 轮尾动作：默认透明，悬停整个回合才浮现 */
.msg-acts {
  display: flex;
  justify-content: flex-end;
  height: 18px;
  opacity: 0;
  transition: opacity var(--omp-dur-fast) var(--omp-ease);
}

.msg:hover .msg-acts {
  opacity: 1;
}

.msg-act {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 0 6px;
  border: none;
  background: transparent;
  color: var(--omp-faint);
  font-size: var(--omp-fs-3xs);
  font-weight: 600;
  transition:
    color var(--omp-dur-fast) var(--omp-ease),
    transform var(--omp-dur-fast) var(--omp-ease);
}

.msg-act:hover:not(:disabled) {
  color: var(--omp-brand);
  transform: scale(var(--omp-press-hover));
}

.msg-act:active:not(:disabled) {
  transform: scale(var(--omp-press-active));
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
