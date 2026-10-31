// 历史消息列表：把后端投影好的轮次排成一列，并算出「哪几行该并成一坨」。
// 并坨的判据放这一层：列表里有「切引擎分隔」这类非消息行，index 相邻
// 不代表同一段对话；直播轮以「历史里有没有它」去重，两处都画就是回复画两遍。
<script setup lang="ts">
import { computed } from 'vue'
import MessageTurn from '@/components/chat/MessageTurn.vue'
import { turnPresence, type TurnUsage } from '@/composables/useMessageFlow'
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
    usage?: TurnUsage
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

/**
 * 直播的用户那句要不要画。
 *
 * 服务器历史里已经有这句了就不画——否则用户会看到自己刚发的话出现两次。
 */
const liveUserVisible = computed(() => {
  if (!props.live) return false
  return turnPresence(props.turns, props.live.userMessage, props.live.blocks) !== 1
})

/**
 * 直播的助手回复要不要画。
 *
 * 整轮已经在服务器历史里 → 不画。
 * 只有用户那句在 → 要画（助手回复还在流）。
 */
const liveAssistantVisible = computed(() => {
  if (!props.live) return false
  return turnPresence(props.turns, props.live.userMessage, props.live.blocks) !== 2
})

/**
 * 当前轮要不要出头：历史最后一行是同一个角色就不出。
 */
const liveContinued = computed(() => {
  const last = props.turns.at(-1)
  return last?.role === 'assistant'
})

/**
 * 这一行是不是它那一轮的**最后**一条。
 *
 * 读数只挂在最后一条上：引擎每条助手消息都带一份累计用量，
 * 而一轮（一次提问到一次回答）里往往有好几条助手消息（工具调用前后各一条）。
 * 每条都摆一次读数，看起来就像「这一轮花了八次钱」。
 *
 * 判据是「下一条是不是另一个角色」：同角色的下一条说明这一轮还没结束。
 */
function isRoundEnd(index: number): boolean {
  const next = props.turns[index + 1]
  return !next || next.role !== props.turns[index].role
}
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
      :history-turn="item"
      :hid="item.hid"
      :show-metrics="isRoundEnd(index)"
    />

    <template v-if="props.live">
      <MessageTurn
        v-if="liveUserVisible"
        role="user"
        :blocks="[{ kind: 'text', text: props.live.userMessage }]"
        :run-id="`${props.live.runId}:user`"
        :continued="false"
      />
      <MessageTurn
        v-if="liveAssistantVisible"
        role="assistant"
        :blocks="props.live.blocks"
        :run-id="props.live.runId"
        :model="props.live.model"
        :continued="liveContinued"
        :streaming="props.live.streaming"
        live
        :errored="props.live.errored"
        :usage="props.live.usage"
        show-metrics
      />
    </template>
  </div>
</template>

<style scoped>
/* 列宽、间距、屏外不参与 layout 都归 content.css 的 .msg-col（唯一落点）。 */
</style>
