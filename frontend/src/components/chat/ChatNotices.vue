<script setup lang="ts">
// 对话内提示条：排队、已切换、压缩中这类「刚刚发生了什么」。
//
// 为什么不复用右下角的 toast：那些是全局动作的结果（保存设置、装插件）；
// 这里的是这一轮对话的过程，它该出现在消息流旁边而不是屏幕角落。
// 4 秒自动收，悬停暂停——用户可能正要把里面的路径复制出来。
import { onBeforeUnmount, watch } from 'vue'
import { useChatStore } from '@/stores/chat'

const chat = useChatStore()

// 一条提示 4 秒后自己消失。用 setTimeout 而不是 CSS 动画，
// 因为「移除」要真的从数组里删掉，否则下一条会被它顶慢。
let timer: number | undefined
watch(
  () => chat.notices.length,
  (n) => {
    if (!n) return
    window.clearTimeout(timer)
    timer = window.setTimeout(() => {
      const first = chat.notices[0]
      if (first) chat.dismissNotice(first.id)
    }, 4200)
  },
)

onBeforeUnmount(() => window.clearTimeout(timer))
</script>

<template>
  <transition-group name="notice" tag="div" class="notice-stack">
    <button
      v-for="notice in chat.notices"
      :key="notice.id"
      class="notice-chip omp-pressable"
      :class="notice.level"
      @click="chat.dismissNotice(notice.id)"
    >
      <span class="dot" :class="notice.level === 'error' ? 'warn' : notice.level === 'warn' ? 'warn' : 'ok'" />
      <span class="notice-text selectable">{{ notice.message }}</span>
    </button>
  </transition-group>
</template>

<style scoped>
.notice-stack {
  position: absolute;
  left: 50%;
  bottom: 178px;
  transform: translateX(-50%);
  z-index: var(--omp-z-pop);
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-1);
  align-items: center;
  pointer-events: none;
}

.notice-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--omp-sp-2);
  max-width: 520px;
  padding: var(--omp-sp-1) var(--omp-sp-3);
  border: 1px solid var(--omp-line-strong);
  border-radius: var(--omp-r-full);
  background: var(--omp-raised);
  box-shadow: var(--omp-shadow-lift);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-ink-5);
  pointer-events: auto;
}

/* 缩放交给 .omp-pressable；这里只管换色 */
.notice-chip:hover {
  border-color: var(--omp-brand);
}

.notice-chip.error {
  border-color: var(--omp-bad);
  color: var(--omp-bad);
}

.notice-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.notice-enter-active,
.notice-leave-active {
  transition:
    opacity var(--omp-dur) var(--omp-ease),
    transform var(--omp-dur) var(--omp-ease);
}

.notice-enter-from,
.notice-leave-to {
  opacity: 0;
  transform: translateY(6px) scale(0.96);
}
</style>
