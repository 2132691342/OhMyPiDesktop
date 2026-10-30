<script setup lang="ts">
// 历史消息列表：把后端投影好的轮次排成一列，并算出「哪几轮该并成一坨」。
//
// 并坨的判据放在这一层而不是每个轮次自己看 index：
// 列表里有「切引擎分隔」这种不是消息的行，index 相邻不代表它们属于同一段对话；
// 而且当前正在流的那一轮是拼在历史后面的，它要不要接着上一轮的头
// 同样得拿「上一行是什么」来判。
import { computed } from 'vue'
import MessageTurn from '@/components/chat/MessageTurn.vue'
import type { ChatBlock, HistoryTurn } from '@/types/api'

const props = defineProps<{
  turns: HistoryTurn[]
  /**
   * 拼在历史后面的当前轮（流式进行中）。
   * 它的头要不要出，取决于历史最后一行是不是同一个角色。
   */
  live?: {
    userMessage: string
    blocks: ChatBlock[]
    runId: string
    model?: string
    streaming: boolean
    errored: boolean
  } | null
}>()

/** 每一行与其上一行是否同角色（分隔行永远不并坨）。 */
const continuedFlags = computed<boolean[]>(() =>
  props.turns.map((turn, i) => {
    if (i === 0) return false
    const prev = props.turns[i - 1]
    if (turn.role === 'engine-mark' || prev.role === 'engine-mark') return false
    return prev.role === turn.role
  }),
)

/** 当前轮要不要出头：历史最后一行是同一个角色就不出。 */
const liveContinued = computed(() => {
  const last = props.turns.at(-1)
  return last?.role === 'assistant'
})
</script>

<template>
  <div class="msg-col">
    <MessageTurn
      v-for="(item, index) in props.turns"
      :key="`h-${index}`"
      :role="item.role"
      :blocks="item.blocks"
      :run-id="`h-${index}`"
      :model="item.model"
      :continued="continuedFlags[index]"
    />

    <template v-if="props.live">
      <MessageTurn
        role="user"
        :blocks="[{ kind: 'text', text: props.live.userMessage }]"
        :run-id="`${props.live.runId}:user`"
        :continued="false"
      />
      <MessageTurn
        role="assistant"
        :blocks="props.live.blocks"
        :run-id="props.live.runId"
        :model="props.live.model"
        :continued="liveContinued"
        :streaming="props.live.streaming"
        live
        :errored="props.live.errored"
      />
    </template>
  </div>
</template>

<style scoped>
.msg-col {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-3);
  max-width: var(--omp-col);
  margin: 0 auto;
  width: 100%;
}
</style>
