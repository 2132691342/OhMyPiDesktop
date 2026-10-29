<script setup lang="ts">
// 版本栏：当前分支、上游、领先 / 落后，以及取回 / 拉取 / 推送。
//
// 为什么单独一个文件：这一条横杠上每个控件都有自己的前置条件
// （有没有远端、有没有上游、允不允许强推），塞进工作区页会让那页的模板
// 长到没法一眼看完。这里只负责「按状态决定显示什么」，
// 具体的 git 动作由上层执行。
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import type { GitStatus } from '@/types/api'

const props = defineProps<{ status: GitStatus | null; busy: boolean }>()

const emit = defineEmits<{
  fetch: []
  pull: []
  push: [force: string]
}>()

const { t } = useI18n()

/** 有远端才能同步。没有远端时按钮不渲染（不渲染成灰按钮）。 */
const hasRemote = computed(() => (props.status?.remotes?.length ?? 0) > 0)

/** 有上游才有「推送到上游」这个确定动作；没有就退到「推送并建立跟踪」。 */
const hasUpstream = computed(() => Boolean(props.status?.upstream_ref))

/** 强推要确认：先选级别，再输分支名。 */
const forceOpen = ref(false)
const forceLevel = ref<'lease' | 'force'>('lease')
const confirmText = ref('')

/** 确认文本必须与分支名完全一致，否则强推按钮不给点。 */
const canForce = computed(
  () => forceLevel.value === 'lease' || confirmText.value === (props.status?.branch_name ?? ''),
)

function submitForce() {
  if (!canForce.value) return
  emit('push', forceLevel.value)
  forceOpen.value = false
  confirmText.value = ''
}
</script>

<template>
  <div class="git-bar">
    <span class="git-chip">
      <AppIcon name="branch" :size="13" />
      <b class="git-branch">{{ status?.branch_name || t('workspace.noCommit') }}</b>
    </span>

    <span v-if="status?.upstream_ref" class="git-chip git-upstream">
      <span class="git-upstream-name">{{ status.upstream_ref }}</span>
    </span>

    <span v-if="status && (status.ahead || status.behind)" class="git-chip git-count">
      <b v-if="status.ahead" class="is-ahead">↑{{ status.ahead }}</b>
      <b v-if="status.behind" class="is-behind">↓{{ status.behind }}</b>
    </span>

    <span v-if="!hasRemote" class="git-hint">{{ t('workspace.noRemote') }}</span>

    <span class="git-sp" />

    <button
      v-if="hasRemote"
      class="omp-btn sm ghost"
      :class="{ 'is-loading': busy }"
      :disabled="busy"
      :title="t('workspace.fetch')"
      @click="emit('fetch')"
    >
      <AppIcon name="refresh" :size="13" />
      {{ t('workspace.fetch') }}
    </button>

    <button
      v-if="hasRemote"
      class="omp-btn sm ghost"
      :class="{ 'is-loading': busy }"
      :disabled="busy"
      :title="t('workspace.pull')"
      @click="emit('pull')"
    >
      {{ t('workspace.pull') }}
    </button>

    <button
      v-if="hasRemote"
      class="omp-btn sm primary"
      :class="{ 'is-loading': busy }"
      :disabled="busy"
      :title="hasUpstream ? t('workspace.push') : t('workspace.pushFirst')"
      @click="emit('push', 'none')"
    >
      {{ hasUpstream ? t('workspace.push') : t('workspace.pushFirst') }}
    </button>

    <!-- 强推：单独一个入口，且要二次确认。
         强推会覆盖别人的提交，所以它不能是普通推送的「高级选项」，
         得让人显式点出来。 -->
    <button
      v-if="hasRemote"
      class="omp-btn sm ghost"
      :disabled="busy"
      :title="t('workspace.forcePush')"
      @click="forceOpen = !forceOpen"
    >
      {{ t('workspace.forcePush') }}
    </button>

    <div v-if="forceOpen" class="force-panel">
      <div class="force-head">{{ t('workspace.forcePush') }}</div>
      <label class="force-opt">
        <input v-model="forceLevel" type="radio" value="lease" />
        <span>
          <b>{{ t('workspace.forceLease') }}</b>
          <em>{{ t('workspace.forceLeaseHint') }}</em>
        </span>
      </label>
      <label class="force-opt">
        <input v-model="forceLevel" type="radio" value="force" />
        <span>
          <b class="is-danger">{{ t('workspace.forceTrue') }}</b>
          <em>{{ t('workspace.forceTrueHint') }}</em>
        </span>
      </label>
      <div v-if="forceLevel === 'force'" class="force-confirm">
        <input
          v-model="confirmText"
          class="omp-input"
          type="text"
          :placeholder="t('workspace.forceConfirmPlaceholder', { name: status?.branch_name ?? '' })"
        />
        <span class="force-confirm-hint">{{ t('workspace.forceConfirmHint') }}</span>
      </div>
      <div class="force-actions">
        <button class="omp-btn sm ghost" @click="forceOpen = false">{{ t('common.cancel') }}</button>
        <button class="omp-btn sm danger" :disabled="!canForce" @click="submitForce">
          {{ t('workspace.forcePush') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 版本栏：一条横杠，把「现在在哪个分支、与远端差多少、怎么同步」说清 */
.git-bar {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex-wrap: wrap;
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r);
  background: var(--omp-surface);
  flex: none;
}

.git-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  min-width: 0;
}
.git-branch {
  font-family: var(--omp-font-mono);
  color: var(--omp-brand);
  font-weight: 650;
}
.git-upstream-name {
  font-family: var(--omp-font-mono);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 200px;
}
.git-count {
  font-family: var(--omp-font-mono);
  gap: 6px;
  font-variant-numeric: tabular-nums;
}
.is-ahead {
  color: var(--omp-ok);
}
.is-behind {
  color: var(--omp-warn);
}

.git-hint {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.git-sp {
  flex: 1;
}

/* 强推面板：就在版本栏下方展开，不弹窗——
   弹窗会挡住版本栏，而强推的前提正是「看着版本栏决定要不要推」。 */
.force-panel {
  flex-basis: 100%;
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  margin-top: var(--omp-sp-1);
  padding: var(--omp-sp-3);
  border: 1px solid var(--omp-line);
  border-left: 2px solid var(--omp-warn);
  border-radius: var(--omp-r-sm);
  background: var(--omp-hover);
}

.force-head {
  font-size: var(--omp-fs-2xs);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--omp-faint);
}

.force-opt {
  display: flex;
  align-items: flex-start;
  gap: var(--omp-sp-2);
  cursor: pointer;
}
.force-opt > span {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}
.force-opt b {
  font-size: var(--omp-fs-xs);
  color: var(--omp-ink);
}
.force-opt b.is-danger {
  color: var(--omp-bad);
}
.force-opt em {
  font-style: normal;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  line-height: 1.5;
}

.force-confirm {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.force-confirm-hint {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.force-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--omp-sp-2);
}
</style>
