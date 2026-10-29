<script setup lang="ts">
// 提交框与分支列表。
//
// 两者占的是同一块位置（改动清单下方），且都只在「改动」标签下出现。
// 拆出来是为了让 WorkspaceView 只管三栏的排布与刷新——
// 这一块自己的状态只有「正在写的提交说明」，不需要回到父组件去。
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import type { GitBranch } from '@/types/api'

const props = defineProps<{
  branches: GitBranch[]
  /** 暂存区里的文件数（提交按钮上显示） */
  stagedCount: number
  committing: boolean
  /** 进行中的动作名（空表示空闲） */
  pending: string
}>()

const emit = defineEmits<{
  commit: [message: string]
  createBranch: []
  switchBranch: [name: string]
}>()

const { t } = useI18n()

const message = ref('')

const canCommit = computed(
  () => Boolean(message.value.trim()) && props.stagedCount > 0 && !props.committing,
)

async function submit(): Promise<void> {
  if (!canCommit.value) return
  emit('commit', message.value.trim())
  message.value = ''
}

/** 父组件提交成功后调用：清空输入框。 */
function reset(): void {
  message.value = ''
}

defineExpose({ reset })
</script>

<template>
  <div class="commit-box">
    <textarea
      v-model="message"
      class="omp-input"
      rows="3"
      :placeholder="t('workspace.commitPlaceholder')"
    />
    <div class="commit-actions">
      <button
        class="omp-btn primary"
        :class="{ 'is-loading': props.committing }"
        :disabled="!canCommit"
        @click="submit"
      >
        {{ t('workspace.commit', { n: props.stagedCount }) }}
      </button>
    </div>
  </div>

  <div class="branch-row">
    <span class="plate">{{ t('workspace.branches') }}</span>
    <button class="omp-btn text sm" :disabled="Boolean(props.pending)" @click="emit('createBranch')">
      {{ t('workspace.newBranch') }}
    </button>
  </div>
  <div class="branch-list">
    <button
      v-for="branch in props.branches"
      :key="`${branch.remote_name}:${branch.branch_name}`"
      class="branch-line omp-row-click"
      :class="{ active: branch.current }"
      :disabled="branch.current || Boolean(props.pending)"
      @click="emit('switchBranch', branch.branch_name)"
    >
      <AppIcon name="branch" :size="12" />
      <span>{{ branch.branch_name }}</span>
      <span v-if="branch.remote_name" class="tag">{{ branch.remote_name }}</span>
      <span v-if="branch.ahead || branch.behind" class="readout">
        ↑{{ branch.ahead }} ↓{{ branch.behind }}
      </span>
    </button>
  </div>
</template>

<style scoped>
.commit-box {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  flex: none;
}

.commit-actions {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
}

.branch-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex: none;
}

.plate {
  font-size: var(--omp-fs-3xs);
  font-weight: 700;
  letter-spacing: var(--omp-ls-plate);
  text-transform: uppercase;
  color: var(--omp-faint);
}

.branch-list {
  display: flex;
  flex-direction: column;
  gap: 1px;
  flex: none;
}

.branch-line {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: 3px var(--omp-sp-2);
  border: none;
  background: transparent;
  color: var(--omp-mute);
  font-size: var(--omp-fs-xs);
  font-family: var(--omp-font-mono);
  text-align: left;
  border-radius: var(--omp-r-sm);
  cursor: pointer;
}

.branch-line span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.branch-line.active {
  color: var(--omp-brand);
  font-weight: 600;
}

.branch-line:disabled {
  cursor: default;
}

.readout {
  margin-left: auto;
  flex: none;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}
</style>
