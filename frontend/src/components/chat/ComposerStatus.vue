<script setup lang="ts">
// 输入区上方的状态条：引擎选择、缺引擎提示、引擎上报的状态、排队条目、
// 重试进度、错误原话。
//
// 它们都在回答「现在能不能发、刚才为什么不顺」，与输入框本身是两件事。
// 拆出来还有一个好处：这一堆都是条件渲染，混在输入区里会让那段模板反复
// 折叠，改一个提示得先在一屏 if 里找到它。
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'
import { useAppStore } from '@/stores/app'
import type { EngineKind } from '@/types/api'

const emit = defineEmits<{
  pickEngine: [kind: EngineKind]
  clearAttachError: []
}>()

const chat = useChatStore()
const app = useAppStore()
const { t } = useI18n()

const props = defineProps<{ pickingEngine: boolean; attachError: string }>()


async function clearQueued(): Promise<void> {
  try {
    await chat.clearQueue()
    chat.pushNotice('info', t('chat.queueCleared'))
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  }
}

async function dropQueued(message: string, queueName: 'steering' | 'followUp'): Promise<void> {
  try {
    if (!(await chat.removeQueued(message, queueName))) {
      chat.pushNotice('info', t('chat.queueGone'))
    }
  } catch (err) {
    chat.promptError = err instanceof Error ? err.message : String(err)
  }
}
</script>

<template>
  <!-- 引擎选择：只列探测到的引擎，只有一个时不显示（不占位置） -->
  <div v-if="app.showEnginePicker" class="engine-strip">
    <div class="seg" role="tablist">
      <button
        v-for="item in app.installed"
        :key="item.kind"
        class="seg-btn"
        :class="{ active: chat.kind === item.kind }"
        :disabled="props.pickingEngine || Boolean(chat.switching)"
        :title="item.exe"
        @click="emit('pickEngine', item.kind)"
      >
        <span class="dot" :class="item.kind === chat.kind ? 'ok' : ''" />
        {{ item.kind }}
      </button>
    </div>
    <span v-if="props.pickingEngine" class="engine-switching">
      <span class="dot busy" />
      {{ t('chat.switchingEngine') }}
    </span>
  </div>

  <div v-if="!app.hasEngine" class="no-engine">
    <span class="dot warn" />
    <span>{{ t('chat.noEngine') }}</span>
    <code class="install-cmd selectable">{{ app.installHint(chat.kind) }}</code>
    <button
      class="omp-btn text sm"
      :class="{ 'is-loading': app.reDetecting }"
      :disabled="app.reDetecting"
      @click="app.detectEngines()"
    >
      {{ t('chat.redetect') }}
    </button>
    <button class="omp-btn text sm" @click="app.openApps()">{{ t('chat.openApps') }}</button>
  </div>

  <div v-if="chat.widgets.length || chat.statusText" class="status-strip">
    <span v-if="chat.statusText" class="status-chip">{{ chat.statusText }}</span>
    <span v-for="widget in chat.widgets" :key="widget.key" class="status-chip">
      {{ widget.lines.join(' · ') }}
    </span>
  </div>

  <div v-if="chat.queue.steering.length || chat.queue.followUp.length" class="queue-strip">
    <span class="dot busy" />
    {{ t('chat.queuedCount', { n: chat.queue.steering.length + chat.queue.followUp.length }) }}
    <template v-if="chat.caps?.queue_edit">
      <button
        v-for="(m, i) in chat.queue.steering"
        :key="'s' + i"
        class="queue-chip omp-pressable"
        :title="t('chat.removeQueued')"
        @click="dropQueued(m, 'steering')"
      >
        {{ m }}<span class="x">×</span>
      </button>
      <button
        v-for="(m, i) in chat.queue.followUp"
        :key="'f' + i"
        class="queue-chip omp-pressable"
        :title="t('chat.removeQueued')"
        @click="dropQueued(m, 'followUp')"
      >
        {{ m }}<span class="x">×</span>
      </button>
    </template>
    <button
      v-if="chat.caps?.queue_clear"
      class="omp-btn text sm"
      :title="t('chat.clearQueue')"
      @click="clearQueued"
    >
      {{ t('chat.clearQueue') }}
    </button>
  </div>

  <div v-if="chat.retry" class="retry-strip">
    <span class="dot warn" />
    {{ t('chat.retrying') }} {{ chat.retry.attempt }}/{{ chat.retry.max }}
  </div>

  <!-- 切不到请求的会话：历史看得到，但下一句话会写到另一条里去。
       不能只说“失败了”：要说清“看一下没问题、发消息会到哪里”。 -->
  <div v-if="chat.bindErr" class="bind-strip">
    <span class="dot warn" />
    <span class="bind-text selectable">{{ t('chat.bindFailed', { reason: chat.bindErr }) }}</span>
    <button class="omp-btn text sm" @click="chat.bindErr = ''">{{ t('common.close') }}</button>
  </div>

  <!-- 错误必须把原话说出来：只把面板染红等于没告诉用户发生了什么 -->
  <div v-if="chat.promptError" class="error-strip">
    <span class="dot warn" />
    <span class="error-text selectable">{{ chat.promptError }}</span>
    <button class="omp-btn text sm" @click="chat.promptError = ''">{{ t('common.close') }}</button>
  </div>

  <div v-if="props.attachError" class="error-strip">
    <span class="dot warn" />
    <span class="error-text selectable">{{ props.attachError }}</span>
    <button class="omp-btn text sm" @click="emit('clearAttachError')">
      {{ t('common.close') }}
    </button>
  </div>
</template>

<style scoped>
.engine-strip {
  max-width: var(--omp-col);
  margin: 0 auto var(--omp-sp-2);
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
}

.engine-switching {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
}

.no-engine {
  max-width: var(--omp-col);
  margin: 0 auto var(--omp-sp-2);
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
  flex-wrap: wrap;
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border: 1px solid var(--omp-warn);
  border-radius: var(--omp-r);
  background: var(--omp-warn-soft);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-ink);
}

.install-cmd {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  padding: 2px 6px;
  border-radius: var(--omp-r-xs);
  background: var(--omp-surface);
  border: 1px solid var(--omp-line);
  user-select: all;
}

.queue-strip {
  max-width: var(--omp-col);
  margin: 0 auto var(--omp-sp-2);
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
}

/* 排队条目：点一下移除这一条。截断到 18em，长句子不把整行撑开。 */
.queue-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 18em;
  padding: 2px 7px;
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r-full);
  background: var(--omp-surface);
  color: var(--omp-ink-2);
  font: inherit;
  line-height: 1.5;
  cursor: pointer;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.queue-chip:hover {
  background: var(--omp-hover);
  border-color: var(--omp-line-strong);
}

.queue-chip .x {
  color: var(--omp-mute);
  font-size: 1.05em;
  line-height: 1;
}

.queue-chip:hover .x {
  color: var(--omp-brand);
}

.retry-strip {
  max-width: var(--omp-col);
  margin: 0 auto var(--omp-sp-2);
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-warn);
}

/* 会话绑定失败：用警告色而不是危险色——
   什么都没坏，只是“下一句话会写到另一条会话里”。 */
.bind-strip {
  max-width: var(--omp-col);
  margin: 0 auto var(--omp-sp-2);
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border: 1px solid var(--omp-warn);
  border-radius: var(--omp-r);
  background: var(--omp-warn-soft);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-ink);
}

.bind-text {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}

.error-strip {
  max-width: var(--omp-col);
  margin: 0 auto var(--omp-sp-2);
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border: 1px solid var(--omp-bad);
  border-radius: var(--omp-r);
  background: var(--omp-bad-soft);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-bad);
}

.error-text {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}

.status-strip {
  max-width: var(--omp-col);
  margin: 0 auto var(--omp-sp-2);
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

.status-chip {
  font-size: var(--omp-fs-3xs);
  padding: 1px 7px;
  border-radius: var(--omp-r-full);
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
  font-family: var(--omp-font-mono);
}
</style>
