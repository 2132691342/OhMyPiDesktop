<script setup lang="ts">
// 轻提示：右下角堆叠，可点选复制，悬停暂停计时；可带一个动作（如撤销）。
// 有原因才显示原因，没原因不硬编一句「操作成功」。
import { useToasts } from '@/composables/useToasts'

const { items, dismiss, hold, runAction } = useToasts()
</script>

<template>
  <div class="toast-host">
    <transition-group name="omp-list" tag="div" class="toast-stack">
      <div
        v-for="item in items"
        :key="item.id"
        class="toast selectable"
        :class="item.level"
        @mouseenter="hold(item.id, true)"
        @mouseleave="hold(item.id, false)"
      >
        <span class="toast-dot" />
        <div class="toast-text">
          <p class="toast-msg">{{ item.message }}</p>
          <p v-if="item.detail" class="toast-detail">{{ item.detail }}</p>
        </div>
        <button v-if="item.action" class="toast-action" @click="runAction(item)">
          {{ item.action.label }}
        </button>
        <button class="toast-close" title="关闭" @click="dismiss(item.id)">
          <svg viewBox="0 0 12 12" width="10" height="10" stroke="currentColor" stroke-width="1.4"><path d="M3 3l6 6M9 3 3 9" /></svg>
        </button>
      </div>
    </transition-group>
  </div>
</template>

<style scoped>
.toast-host {
  position: fixed;
  right: var(--omp-sp-4);
  bottom: var(--omp-sp-4);
  z-index: var(--omp-z-toast);
  pointer-events: none;
}

.toast-stack {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  align-items: flex-end;
}

.toast {
  display: flex;
  align-items: flex-start;
  gap: var(--omp-sp-2);
  max-width: 380px;
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r-lg);
  background: var(--omp-raised);
  box-shadow: var(--omp-shadow-lift);
  pointer-events: auto;
  animation: toast-in var(--omp-dur-slow) var(--omp-ease-out);
}

@keyframes toast-in {
  from {
    opacity: 0;
    transform: translateY(8px) scale(0.97);
  }
}

.toast-dot {
  width: 6px;
  height: 6px;
  margin-top: 6px;
  border-radius: 50%;
  background: var(--omp-mute);
  flex: none;
}

.toast.ok .toast-dot {
  background: var(--omp-ok);
}
.toast.warn .toast-dot {
  background: var(--omp-warn);
}
.toast.error .toast-dot {
  background: var(--omp-bad);
}

.toast-text {
  min-width: 0;
}

.toast-msg {
  margin: 0;
  font-size: var(--omp-fs-sm);
  line-height: 1.5;
  color: var(--omp-ink);
  overflow-wrap: anywhere;
}

.toast-detail {
  margin: 3px 0 0;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  line-height: 1.45;
  color: var(--omp-faint);
  overflow-wrap: anywhere;
}

/* 动作按钮（如「撤销」）：比关闭钮显眼，但不能盖过提示正文 */
.toast-action {
  flex: none;
  align-self: center;
  padding: 2px 10px;
  border: 1px solid var(--omp-line-strong);
  border-radius: var(--omp-r-full);
  background: transparent;
  color: var(--omp-brand);
  font-size: var(--omp-fs-2xs);
  font-weight: 600;
  cursor: pointer;
  transition:
    background-color var(--omp-dur-fast) var(--omp-ease),
    border-color var(--omp-dur-fast) var(--omp-ease),
    transform var(--omp-dur-fast) var(--omp-ease);
}

.toast-action:hover {
  background: var(--omp-brand-soft);
  border-color: var(--omp-brand-line);
  transform: scale(var(--omp-press-hover));
}

.toast-action:active {
  transform: scale(var(--omp-press-active));
}

.toast-close {
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
  flex: none;
  border: none;
  border-radius: var(--omp-r-xs);
  background: transparent;
  color: var(--omp-faint);
  transition:
    background-color var(--omp-dur-fast) var(--omp-ease),
    color var(--omp-dur-fast) var(--omp-ease);
}

.toast-close:hover {
  background: var(--omp-hover);
  color: var(--omp-ink);
}
</style>
