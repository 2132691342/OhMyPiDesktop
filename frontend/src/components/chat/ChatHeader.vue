<script setup lang="ts">
// 对话页的页头与授权链接条。
//
// 页头回答「现在在跟谁说话、上下文用了多少」；
// 授权链接条回答「引擎要我去浏览器点一下，地址在这儿」。
//
// 两者都是「读状态 + 渲染」，不持有自己的状态，所以放在一个文件里：
// 它们占的是同一块位置（页头下方），而且都只在对话页出现。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ContextMeter from '@/components/chat/ContextMeter.vue'
import AppIcon from '@/components/common/AppIcon.vue'
import { useAppStore } from '@/stores/app'
import { useChatStore } from '@/stores/chat'
import { openExternal } from '@/api/wails'

const app = useAppStore()
const chat = useChatStore()
const { t } = useI18n()

/** 当前这一行：服务商 · 模型 · 速率。 */
const modelLine = computed(() => {
  if (!chat.model) return ''
  const parts = [chat.model.provider, chat.model.id]
  if (chat.tokensPerSecond) parts.push(`${chat.tokensPerSecond.toFixed(1)} tok/s`)
  return parts.join(' · ')
})

/**
 * 引擎要求浏览器完成授权时，把地址挂在这里给用户复制。
 *
 * 不自动打开：默认浏览器可能打不开，而用户看不到原文就无从补救。
 */
async function copyLink(url: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(url)
    app.pendingLink = null
  } catch {
    // 剪贴板不可用时保留这一条，用户仍能手动选中复制
  }
}

async function openLink(url: string): Promise<void> {
  try {
    await openExternal(url)
  } finally {
    app.pendingLink = null
  }
}
</script>

<template>
  <header class="chat-head">
    <div class="chat-head-left">
      <h1 class="chat-title">{{ t('nav.chat') }}</h1>
      <span v-if="modelLine" class="chat-sub">{{ modelLine }}</span>
    </div>
    <div class="chat-head-right">
      <button class="omp-btn icon ghost" :title="app.workspace" @click="app.chooseWorkspace()">
        <AppIcon name="folder" :size="15" />
      </button>
      <button
        class="omp-btn icon ghost"
        :class="{ 'is-loading': chat.compacting }"
        :title="t('chat.compact')"
        :disabled="chat.busy || !app.hasEngine"
        @click="chat.compact()"
      >
        <AppIcon name="refresh" :size="15" />
      </button>
      <ContextMeter />
    </div>
  </header>

  <div v-if="app.pendingLink" class="link-strip">
    <div class="link-text">
      <strong>{{ t('chat.openLinkTitle') }}</strong>
      <span class="link-hint">{{ app.pendingLink.instructions || t('chat.openLinkHint') }}</span>
      <code class="link-url selectable">{{ app.pendingLink.url }}</code>
    </div>
    <div class="link-actions">
      <button class="omp-btn sm ghost" @click="copyLink(app.pendingLink.url)">
        {{ t('chat.openLinkCopy') }}
      </button>
      <button class="omp-btn primary sm" @click="openLink(app.pendingLink.url)">
        {{ t('chat.openLinkBrowser') }}
      </button>
      <button class="omp-btn sm ghost" @click="app.pendingLink = null">
        {{ t('chat.openLinkDismiss') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.chat-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--omp-sp-3);
  padding: var(--omp-sp-3) var(--omp-sp-5);
  border-bottom: 1px solid var(--omp-line);
  flex: none;
}

.chat-head-left {
  display: flex;
  align-items: baseline;
  gap: var(--omp-sp-3);
  min-width: 0;
}

.chat-title {
  margin: 0;
  font-family: var(--omp-font-display);
  font-size: var(--omp-fs-lg);
  font-weight: 650;
  color: var(--omp-ink);
  flex: none;
}

.chat-sub {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-head-right {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex: none;
}

/* 授权链接条：贴在页头下方，不遮消息。用清单底的淡强调色，
   它是一条「请去别处办一件事」的指引，不是错误。 */
.link-strip {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
  flex-wrap: wrap;
  padding: var(--omp-sp-2) var(--omp-sp-5);
  border-bottom: 1px solid var(--omp-line);
  background: var(--omp-brand-soft);
  flex: none;
}

.link-text {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex-wrap: wrap;
  min-width: 0;
  flex: 1;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-ink);
}

.link-hint {
  color: var(--omp-mute);
}

.link-url {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  padding: 2px 6px;
  border-radius: var(--omp-r-xs);
  background: var(--omp-surface);
  border: 1px solid var(--omp-line);
  overflow-wrap: anywhere;
}

.link-actions {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex: none;
}
</style>
