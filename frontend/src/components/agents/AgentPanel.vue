<script setup lang="ts">
// 子代理观测台（仅 omp）。不是编排器：omp 没有 spawn 的 RPC，
// 只做三件事——看见名单、看见进度、在跑偏时插话或掐掉。
// pi 下整块不渲染（不做会说谎的界面）。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import AgentDetail from '@/components/agents/AgentDetail.vue'
import { useAgentsStore } from '@/stores/agents'
import { useAppStore } from '@/stores/app'
import { useToasts } from '@/composables/useToasts'

const { t } = useI18n()
const agents = useAgentsStore()
const app = useAppStore()
const toasts = useToasts()

// 能力门：pi 没有子代理 RPC，整块不渲染（也不留空栏）。
const supported = computed(() => Boolean(app.capsOf(app.active)?.subagents))

const steerDraft = ref('')
const steering = ref(false)
const cancelling = ref('')
let poll: number | undefined

const visible = computed(() => supported.value && (agents.roster.length > 0 || agents.open))
const detail = computed(() => agents.active)

onMounted(() => {
  // 不支持子代理的引擎不去订阅：那会每次都探一次「不支持」错误。
  if (supported.value) void agents.ensureSubscribed()
  void agents.refresh()
  // 帧不是可靠流（终态只出现一次），所以按低频对账兜住漏掉的。
  poll = window.setInterval(() => {
    if (agents.open || agents.roster.length) void agents.refresh()
  }, 8000)
})

onUnmounted(() => {
  if (poll) window.clearInterval(poll)
})

async function pick(id: string) {
  await agents.select(id)
}

async function sendSteer() {
  const id = detail.value?.id
  const message = steerDraft.value.trim()
  if (!id || !message) return
  steering.value = true
  try {
    await agents.steer(id, message)
    steerDraft.value = ''
    toasts.push('ok', t('agents.steerSent'))
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    steering.value = false
  }
}

async function cancelAgent(id: string) {
  cancelling.value = id
  try {
    const res = await agents.cancel(id)
    // cancelled=false 是幂等答案（已结束 / 已取消），不是错误。
    toasts.push('ok', res.cancelled ? t('agents.cancelled') : t('agents.alreadyGone'))
    await agents.refresh()
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    cancelling.value = ''
  }
}

function statusClass(status: string): string {
  if (status === 'running') return 'run'
  if (status === 'completed') return 'ok'
  if (status === 'failed' || status === 'aborted') return 'bad'
  return 'idle'
}

/** 抽一条消息的纯文本并截尾：子代理可能产出几万字。 */
function tailText(raw: unknown, limit = 1600): string {
  const text = extractText(raw)
  if (text.length <= limit) return text
  return `…${text.slice(-limit)}`
}

function extractText(raw: unknown): string {
  const msg = raw as Record<string, unknown> | null
  if (!msg) return ''
  const content = msg.content
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((part) => {
        const p = part as Record<string, unknown>
        return typeof p?.text === 'string' ? p.text : ''
      })
      .filter(Boolean)
      .join('\n')
  }
  return ''
}

const transcript = computed(() => {
  const id = detail.value?.id
  if (!id) return []
  // 只渲尾部：子代理可能产出几万字，全量渲染会把主线程压住。
  // 抽文本放在这一层，AgentDetail 只负责渲染字符串。
  return (agents.messages[id] ?? []).slice(-30).map((msg) => tailText(msg))
})
</script>

<template>
  <aside v-if="visible" class="agents" :class="{ wide: agents.open }">
    <header class="agents-head">
      <span class="agents-title">{{ t('agents.title') }}</span>
      <span class="agents-count">{{ agents.roster.length }}</span>
      <button class="omp-btn icon ghost sm" :title="t('agents.collapse')" @click="agents.open = !agents.open">
        <AppIcon :name="agents.open ? 'chevron' : 'chart'" :size="13" />
      </button>
    </header>

    <div v-if="!agents.roster.length" class="agents-empty">
      <p>{{ t('agents.empty') }}</p>
      <p class="agents-empty-hint">{{ t('agents.emptyHint') }}</p>
    </div>

    <div v-else class="agents-body">
      <ul class="roster">
        <li
          v-for="item in agents.roster"
          :key="item.id"
          class="roster-row"
          :class="{ on: item.id === agents.selected }"
          @click="pick(item.id)"
        >
          <span class="dot" :class="statusClass(item.status)" />
          <span class="roster-name">{{ item.agent }}</span>
          <span class="roster-idx">#{{ item.index }}</span>
        </li>
      </ul>

      <AgentDetail v-if="detail" :detail="detail" :wide="agents.open" :transcript="transcript" />

      <div v-if="detail" class="actions">
        <input
          v-model="steerDraft"
          class="omp-input sm"
          :placeholder="t('agents.steerHint')"
          @keydown.enter.prevent="sendSteer"
        />
        <button
          class="omp-btn sm"
          :class="{ 'is-loading': steering }"
          :disabled="steering || !steerDraft.trim()"
          @click="sendSteer"
        >
          {{ t('agents.steer') }}
        </button>
        <button
          class="omp-btn sm danger"
          :class="{ 'is-loading': cancelling === detail.id }"
          :disabled="cancelling === detail.id"
          @click="cancelAgent(detail.id)"
        >
          {{ t('agents.cancel') }}
        </button>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.agents {
  width: 260px;
  flex: none;
  display: flex;
  flex-direction: column;
  border-left: 1px solid var(--omp-line);
  background: var(--omp-window);
  min-height: 0;
  transition: width var(--omp-dur) var(--omp-ease);
}

.agents.wide {
  width: 360px;
}

.agents-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  height: var(--omp-pagehead, 52px);
  padding: 0 var(--omp-sp-3);
  border-bottom: 1px solid var(--omp-line);
  flex: none;
}

.agents-title {
  font-size: var(--omp-fs-2xs);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--omp-mute);
}

.agents-count {
  font-size: var(--omp-fs-3xs);
  font-family: var(--omp-font-mono);
  color: var(--omp-faint);
  margin-right: auto;
}

.agents-empty {
  padding: var(--omp-sp-4);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
}

.agents-empty-hint {
  margin-top: var(--omp-sp-2);
  color: var(--omp-faint);
  font-size: var(--omp-fs-3xs);
}

.agents-body {
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1;
}

.roster {
  list-style: none;
  margin: 0;
  padding: var(--omp-sp-2);
  display: flex;
  flex-direction: column;
  gap: 1px;
  max-height: 40%;
  overflow: auto;
  flex: none;
}

.roster-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: 6px var(--omp-sp-2);
  border-radius: var(--omp-r-sm);
  cursor: pointer;
  font-size: var(--omp-fs-2xs);
  transition: background-color var(--omp-dur-fast) var(--omp-ease);
}

.roster-row:hover {
  background: var(--omp-hover);
}

.roster-row.on {
  background: var(--omp-selected);
}

.roster-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.roster-idx {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

/* 状态灯：pending 灰 / running 呼吸主色 / completed 绿 / failed 红 */
.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex: none;
}

.dot.idle {
  background: var(--omp-faint);
}

.dot.run {
  background: var(--omp-brand-2);
  animation: breathe 2s ease-in-out infinite;
}

.dot.ok {
  background: var(--omp-ok);
}

.dot.bad {
  background: var(--omp-bad);
}

@keyframes breathe {
  0%,
  100% {
    opacity: 0.45;
  }
  50% {
    opacity: 1;
  }
}

.detail {
  padding: var(--omp-sp-3);
  border-top: 1px solid var(--omp-line);
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  min-height: 0;
  flex: 1;
  overflow: auto;
}

.detail-stage {
  font-size: var(--omp-fs-2xs);
  color: var(--omp-ink-5);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.bar {
  height: 4px;
  border-radius: var(--omp-r-full);
  background: var(--omp-hover);
  overflow: hidden;
}

.bar-fill {
  height: 100%;
  background: var(--omp-brand-2);
  transition: width var(--omp-dur-slow) var(--omp-ease);
}

/* 读数是只读的：不套描边胶囊、不做悬停缩放 */
.readouts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--omp-sp-3);
  font-size: var(--omp-fs-3xs);
  font-family: var(--omp-font-mono);
  color: var(--omp-mute);
}

.mono {
  color: var(--omp-brand);
}

.transcript {
  max-height: 240px;
  overflow: auto;
  padding: var(--omp-sp-2);
  border-radius: var(--omp-r-sm);
  background: var(--omp-code-bg);
  font-size: var(--omp-fs-3xs);
  line-height: 1.55;
  color: var(--omp-ink-5);
  white-space: pre-wrap;
}

.turn {
  margin: 0 0 var(--omp-sp-2);
}

.actions {
  display: flex;
  gap: var(--omp-sp-2);
  margin-top: auto;
}

.actions .omp-input {
  flex: 1;
  min-width: 0;
  height: var(--omp-ctl-sm);
}
</style>
