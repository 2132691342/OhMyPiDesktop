<script setup lang="ts">
// 对话页：左栏会话列表 + 主区消息流 + 输入区。
// 滚动贴底由 JS 管（流式时元素高度在变，浏览器锚定会乱跳）。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import ChatComposer from '@/components/chat/ChatComposer.vue'
import ChatNotices from '@/components/chat/ChatNotices.vue'
import ContextMeter from '@/components/chat/ContextMeter.vue'
import TurnBlocks from '@/components/chat/TurnBlocks.vue'
import AskCard from '@/components/chat/AskCard.vue'
import MarkdownBlock from '@/components/chat/MarkdownBlock.vue'
import AppIcon from '@/components/common/AppIcon.vue'
import SessionList from '@/components/sessions/SessionList.vue'
import AgentPanel from '@/components/agents/AgentPanel.vue'
import { useAppStore } from '@/stores/app'
import { useChatStore } from '@/stores/chat'
import { openExternal } from '@/api/wails'
import type { UIResponse } from '@/types/api'

const app = useAppStore()
const chat = useChatStore()
const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const scroller = ref<HTMLElement | null>(null)
const composer = ref<{ focus: () => void } | null>(null)
const pinned = ref(true)

/**
 * 全局快捷键：只放两个真的每天会用的。
 *
 * Ctrl+K 是「我要说话」——不管光标在哪都回到输入框；
 * Esc 是「先不管这段授权提示」——它贴在消息上方，不关掉就一直在占位。
 * 其他都交给控件自己的 Enter / 方向键，不在全局抢按键。
 */
function onKeydown(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
    event.preventDefault()
    composer.value?.focus()
    return
  }
  if (event.key === 'Escape' && app.pendingLink) {
    app.pendingLink = null
  }
}

const hasTurn = computed(() => Boolean(chat.turn))

function nearBottom(): boolean {
  const el = scroller.value
  if (!el) return true
  return el.scrollHeight - el.scrollTop - el.clientHeight < 120
}

function scrollToBottom(smooth = false) {
  const el = scroller.value
  if (!el) return
  el.scrollTo({ top: el.scrollHeight, behavior: smooth ? 'smooth' : 'auto' })
}

function onScroll() {
  pinned.value = nearBottom()
}

watch(
  () => [chat.turn?.blocks.length, chat.turn?.blocks.at(-1)?.text?.length, chat.history.length],
  async () => {
    if (!pinned.value) return
    await nextTick()
    scrollToBottom()
  },
)

async function answer(response: UIResponse) {
  try {
    await chat.answer(response)
  } catch (err) {
    chat.pushNotice('error', err instanceof Error ? err.message : String(err))
  }
}

async function openSession(path: string) {
  chat.reset()
  await chat.refresh(path)
  // 会话页点「继续」会带 query.session 跳过来：把地址同步成当前会话，
  // 刷新窗口时才不会又回到默认会话。
  await router.replace({ name: 'chat', query: path ? { session: path } : {} })
  await nextTick()
  scrollToBottom()
}

/** 会话页跳过来时带的会话路径。 */
function sessionFromQuery(): string {
  const value = route.query.session
  return typeof value === 'string' ? value : ''
}

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  try {
    const path = sessionFromQuery()
    await chat.refresh(path)
    await nextTick()
    scrollToBottom()
  } catch {
    /* 引擎没起来时对话页仍是可用的空态 */
  }
  // 模型与思考档清单不阻塞首屏：它要问引擎，而引擎冷启动可能要十几秒。
  // 但它**必须发**：之前没有任何地方在进对话页时拉它，于是模型下拉永远空的。
  void chat.loadModels()
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
})

watch(
  () => route.query.session,
  async (value) => {
    const path = typeof value === 'string' ? value : ''
    if (!path || path === chat.sessionFile) return
    await openSession(path)
  },
)

const modelLine = computed(() => {
  if (!chat.model) return ''
  const parts = [chat.model.provider, chat.model.id]
  if (chat.tokensPerSecond) parts.push(`${chat.tokensPerSecond.toFixed(1)} tok/s`)
  return parts.join(' · ')
})

// 引擎要求浏览器完成授权时，把地址挂在这里给用户复制。
// 不自动打开：默认浏览器可能打不开，而用户看不到原文就无从补救。
async function copyLink(url: string) {
  try {
    await navigator.clipboard.writeText(url)
    app.pendingLink = null
  } catch {
    // 剪贴板不可用时保留抽屉，用户仍能手动选中复制
  }
}

async function openLink(url: string) {
  try {
    await openExternal(url)
  } finally {
    app.pendingLink = null
  }
}
</script>

<template>
  <div class="chat-page">
    <aside class="chat-side">
      <SessionList :active="chat.sessionFile" :compact="true" @open="openSession" />
    </aside>

    <section class="chat-main">
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

      <div ref="scroller" class="chat-scroll" @scroll="onScroll">        <div v-if="!app.hasEngine" class="chat-gate">
          <p class="gate-title">{{ t('chat.noEngine') }}</p>
          <router-link class="omp-btn primary" :to="{ name: 'settings' }">{{ t('nav.settings') }}</router-link>
        </div>

        <div v-else-if="!hasTurn && !chat.history.length" class="chat-empty">
          <div class="empty-mark">
            <AppIcon name="spark" :size="26" />
          </div>
          <h2 class="empty-title">{{ t('chat.emptyTitle') }}</h2>
          <p class="empty-hint">{{ t('chat.emptyHint') }}</p>
          <p class="empty-path selectable">{{ app.workspace }}</p>
        </div>

        <div v-else class="msg-col">
          <article
            v-for="(item, index) in chat.history"
            :key="`h-${index}`"
            class="msg"
            :class="{ user: item.role === 'user', 'msg-mark': item.role === 'engine-mark' }"
          >
            <!-- 切引擎的分隔线：说明下面的内容来自另一个引擎、另一条会话 -->
            <div v-if="item.role === 'engine-mark'" class="mark-line">
              <span class="mark-text">{{ t('chat.engineHandoff', { engine: item.blocks[0]?.text ?? '' }) }}</span>
            </div>
            <template v-else>
              <div class="msg-head">
                <span class="msg-badge" :class="{ user: item.role === 'user' }">{{ item.role === 'user' ? 'you' : 'assistant' }}</span>
                <span v-if="item.model" class="msg-meta">{{ item.model }}</span>
              </div>
              <div class="msg-body">
                <TurnBlocks :blocks="item.blocks" :run-id="`h-${index}`" />
              </div>
            </template>
          </article>

          <template v-if="chat.turn">
            <article class="msg user">
              <div class="msg-head">
                <span class="msg-badge user">you</span>
              </div>
              <div class="msg-body">
                <MarkdownBlock :text="chat.turn.user_message" :streaming="false" :cache-key="`${chat.turn.run_id}:user`" />
              </div>
            </article>

            <article class="msg" :class="{ 'msg-streaming': chat.streaming, 'msg-error': chat.promptError }">
              <div class="msg-head">
                <span class="msg-badge">assistant</span>
                <span v-if="chat.model" class="msg-meta">{{ chat.model.id }}</span>
              </div>
              <div class="msg-body">
                <TurnBlocks :blocks="chat.turn.blocks" :run-id="chat.turn.run_id" :streaming="chat.streaming" />
              </div>
            </article>
          </template>

          <AskCard v-if="chat.activeAsk" :ask="chat.activeAsk" @answer="answer" />
        </div>
      </div>

      <button v-if="!pinned" class="jump-bottom" @click="scrollToBottom(true)">
        <AppIcon name="chevron" :size="13" />
      </button>

      <ChatComposer ref="composer" />
      <ChatNotices />
    </section>

    <AgentPanel />
  </div>
</template>

<style scoped>
.chat-page {
  display: flex;
  flex: 1;
  min-height: 0;
}

/* 授权链接条：贴在页头下方，不遮消息。用清单底的淡强调色，
   不和错误提示（危险色）撞。 */
.link-strip {
  display: flex;
  align-items: flex-start;
  gap: var(--omp-sp-3);
  padding: var(--omp-sp-3) var(--omp-sp-4);
  border-bottom: 1px solid var(--omp-line);
  background: var(--omp-brand-soft);
  flex: none;
}

.link-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1;
  font-size: var(--omp-fs-2xs);
}

.link-hint {
  color: var(--omp-mute);
}

.link-url {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  word-break: break-all;
  color: var(--omp-ink);
}

.link-actions {
  display: flex;
  gap: var(--omp-sp-2);
  flex: none;
}

.chat-side {
  width: var(--omp-side);
  flex: none;
  border-right: 1px solid var(--omp-line);
  background: var(--omp-window);
  display: flex;
  flex-direction: column;
  min-height: 0;
}

/* 窄窗口（800×600 的默认尺寸）下侧栏收窄：244px 在 800px 的窗口里会把
   主区压到放不下一行代码。用容器查询而不是媒体查询——窗口是最外层容器，
   两者的断点在这里恰好一致，但以后嵌进别的布局时容器查询仍然正确。 */
@container app (max-width: 960px) {
  .chat-side {
    width: var(--omp-side-narrow);
  }
}

.chat-main {
  position: relative;
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}

.chat-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--omp-sp-3);
  height: 52px;
  flex: none;
  padding: 0 var(--omp-sp-6);
  border-bottom: 1px solid var(--omp-line);
  background: var(--omp-work);
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
}

.chat-sub {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  letter-spacing: 0.02em;
}

.chat-head-right {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
}

.chat-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-anchor: none;
  scrollbar-gutter: stable;
}

.chat-empty,
.chat-gate {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--omp-sp-3);
  min-height: 100%;
  padding: var(--omp-sp-10);
  text-align: center;
}

.empty-mark {
  display: grid;
  place-items: center;
  width: 52px;
  height: 52px;
  border-radius: var(--omp-r-lg);
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
  margin-bottom: var(--omp-sp-1);
}

.empty-title {
  margin: 0;
  font-family: var(--omp-font-display);
  font-size: var(--omp-fs-xl);
  font-weight: 650;
  color: var(--omp-ink);
}

.empty-hint,
.gate-title {
  margin: 0;
  font-size: var(--omp-fs-sm);
  color: var(--omp-mute);
}

.empty-path {
  margin: var(--omp-sp-2) 0 0;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

/* 切引擎分隔：一条带文字的细线。它不是消息，
   所以不进气泡、不带头像——只把「上下文在这里断了」说清楚。 */
.msg-mark {
  padding: 0;
}

.mark-line {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
  margin: var(--omp-sp-4) 0;
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
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  font-family: var(--omp-font-mono);
}

.jump-bottom {
  position: absolute;
  left: 50%;
  bottom: 168px;
  transform: translateX(-50%) rotate(90deg);
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border: 1px solid var(--omp-line-strong);
  border-radius: 50%;
  background: var(--omp-raised);
  color: var(--omp-mute);
  box-shadow: var(--omp-shadow-lift);
  transition:
    transform var(--omp-dur-fast) var(--omp-ease),
    color var(--omp-dur-fast) var(--omp-ease);
}

.jump-bottom:hover {
  color: var(--omp-brand);
}

.jump-bottom:active {
  transform: translateX(-50%) rotate(90deg) scale(0.92);
}
</style>
