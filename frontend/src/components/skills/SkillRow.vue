<script setup lang="ts">
// 一条技能：名字、来源、说明、目录，加启停 / 查看 / 删除。
// 启停开关只在引擎真有这个能力时渲染（不渲染成灰开关）：
// omp 认 enabled、pi 认 disable-model-invocation，语义不同，判据在父组件。
import { useI18n } from 'vue-i18n'
import type { SkillRow } from '@/types/api'

const props = defineProps<{
  row: SkillRow
  /** 引擎有没有「启停」这个能力 */
  canToggle: boolean
  /** 正在切启停的那一条路径（空表示空闲） */
  toggling: string
  /** 正在删的那一条路径（空表示空闲） */
  deleting: string
}>()

const emit = defineEmits<{
  toggle: [row: SkillRow, enabled: boolean]
  view: [row: SkillRow]
  remove: [row: SkillRow]
}>()

const { t } = useI18n()
</script>

<template>
  <article class="skill-row card-pad">
    <div class="skill-main">
      <div class="skill-head">
        <span class="skill-name">{{ props.row.name }}</span>
        <span class="tag">{{ t(`skills.sources.${props.row.source}`) }}</span>
        <span v-if="!props.row.editable" class="tag readonly-tag">{{ t('skills.readonly') }}</span>
      </div>
      <p v-if="props.row.description" class="skill-desc">{{ props.row.description }}</p>
      <div class="skill-path selectable">{{ props.row.dir }}</div>
    </div>

    <div class="skill-actions">
      <!-- 引擎没有启停开关时不渲染控件（不渲染成灰开关） -->
      <el-switch
        v-if="props.canToggle"
        :model-value="props.row.enabled"
        :loading="props.toggling === props.row.path"
        :disabled="Boolean(props.toggling) && props.toggling !== props.row.path"
        :title="props.row.enabled ? t('common.enabled') : t('common.disabled')"
        @change="(value: string | number | boolean) => emit('toggle', props.row, Boolean(value))"
      />
      <button class="omp-btn text sm" @click="emit('view', props.row)">{{ t('skills.view') }}</button>
      <button
        v-if="props.row.editable"
        class="omp-btn text sm danger"
        :class="{ 'is-loading': props.deleting === props.row.path }"
        :disabled="Boolean(props.deleting)"
        @click="emit('remove', props.row)"
      >
        {{ t('common.delete') }}
      </button>
    </div>
  </article>
</template>

<style scoped>
.skill-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
}

.skill-main {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
  flex: 1;
}

.skill-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex-wrap: wrap;
}

.skill-name {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-sm);
  font-weight: 650;
  color: var(--omp-ink);
}

/* 只读标签压暗：它是状态说明，不是可点的分类 */
.readonly-tag {
  opacity: 0.65;
}

.skill-desc {
  margin: 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  line-height: 1.6;
  overflow-wrap: anywhere;
}

.skill-path {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.skill-actions {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex: none;
}
</style>
