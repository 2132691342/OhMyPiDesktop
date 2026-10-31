<script setup lang="ts">
// 技能页：按来源分组的清单 + 启停 + 正文抽屉 + 新建。
// 两引擎发现规则不同（omp 认 managed 目录与 enabled，pi 认 disable-model-invocation），
// 界面同构；切换先动界面再发请求，失败回滚。
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import AppIcon from '@/components/common/AppIcon.vue'
import EngineScopeBar from '@/components/common/EngineScopeBar.vue'
import SkillRowItem from '@/components/skills/SkillRow.vue'
import SkillDrawer from '@/components/skills/SkillDrawer.vue'
import SkillCreateDialog from '@/components/skills/SkillCreateDialog.vue'
import { useEngineScope } from '@/composables/useEngineScope'
import { api } from '@/api/http'
import { useToasts } from '@/composables/useToasts'
import { useAppStore } from '@/stores/app'
import type { EngineKind, SkillRow } from '@/types/api'

type SkillSource = SkillRow['source']

// 分组顺序与引擎的加载优先级一致：工作区 → 用户 → 托管 → 插件
const SOURCES: SkillSource[] = ['project', 'user', 'managed', 'plugin']

const { t } = useI18n()
const toasts = useToasts()
const app = useAppStore()

const kind = computed<EngineKind>(() => app.active)
// 本页作用域（omp / pi / 全部），不跟对话引擎联动。
const { scope, set: setScope } = useEngineScope('skills')
const installedKinds = computed(() => app.installed.map((e) => e.kind))
const writeKind = computed<EngineKind>(() => (scope.value === 'all' ? 'omp' : scope.value))
const caps = computed(() => app.capsOf(writeKind.value))
const engineParam = computed<EngineKind | undefined>(() => (scope.value === 'all' ? undefined : scope.value))

/** 一行属于哪个引擎：「全部」下靠行自身的 kind 定向写操作。 */
function rowEngine(row: SkillRow): EngineKind {
  return (row.kind || writeKind.value) as EngineKind
}

const rows = ref<SkillRow[]>([])
const loading = ref(false)
const filter = ref('')
const toggling = ref('')
const deleting = ref('')

const contentOpen = ref(false)
const contentRow = ref<SkillRow | null>(null)
const content = ref('')
// 编辑态与草稿都在 SkillDrawer 里：它们只在那一个抽屉存在期间有意义。
const contentLoading = ref(false)
const saving = ref(false)

const createOpen = ref(false)
const creating = ref(false)

const filtered = computed(() => {
  const query = filter.value.trim().toLowerCase()
  if (!query) return rows.value
  return rows.value.filter((row) =>
    [row.name, row.description].some((field) => (field ?? '').toLowerCase().includes(query)),
  )
})

// 空分组不渲染：四个来源里通常只有两个有内容，空标题只是噪音
const groups = computed(() =>
  SOURCES.map((source) => ({ source, rows: filtered.value.filter((row) => row.source === source) })).filter(
    (group) => group.rows.length > 0,
  ),
)

async function load() {
  loading.value = true
  try {
    const data = await api.skills(engineParam.value)
    rows.value = data.skills ?? []
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

async function toggle(row: SkillRow, enabled: boolean) {
  const before = row.enabled
  row.enabled = enabled
  toggling.value = row.path
  try {
    await api.toggleSkill(row.path, enabled, rowEngine(row))
  } catch (err) {
    row.enabled = before
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    toggling.value = ''
  }
}

async function openContent(row: SkillRow) {
  contentRow.value = row
  content.value = ''
  contentOpen.value = true
  contentLoading.value = true
  try {
    const data = await api.skillContent(row.path)
    content.value = data.content ?? ''
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
    contentOpen.value = false
  } finally {
    contentLoading.value = false
  }
}

// 抽屉交上来的新正文：它已经在自己的草稿里改完了，这里只负责写盘与重载清单。
async function saveContent(text: string) {
  const row = contentRow.value
  if (!row || saving.value) return
  saving.value = true
  try {
    const data = await api.saveSkillContent(rowEngine(row), row.path, text)
    content.value = data.content ?? text
    toasts.push('ok', t('skills.saved'))
    // 正文里可能改了 frontmatter（名字 / 描述 / 启用），清单得跟着重读。
    await load()
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    saving.value = false
  }
}

async function remove(row: SkillRow) {
  try {
    await ElMessageBox.confirm(t('skills.deleteConfirm', { name: row.name }), t('skills.deleteTitle'), {
      confirmButtonText: t('common.delete'),
      cancelButtonText: t('common.cancel'),
      type: 'warning',
    })
  } catch {
    return
  }
  deleting.value = row.path
  try {
    await api.deleteSkill(row.path, rowEngine(row))
    toasts.push('ok', t('common.saved'))
    await load()
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    deleting.value = ''
  }
}

function openCreate() {
  createOpen.value = true
}

async function create(input: { name: string; description: string; body: string }) {
  if (creating.value) return
  creating.value = true
  try {
    await api.createSkill(input.name, input.description, input.body, writeKind.value)
    toasts.push('ok', t('common.saved'))
    createOpen.value = false
    await load()
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    creating.value = false
  }
}

watch(kind, () => {
  rows.value = []
  void load()
})

onMounted(load)
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">{{ t('skills.title') }}</h1>
        <p class="page-sub">{{ t('skills.scopedSubtitle') }}</p>
      </div>
      <div class="head-actions">
        <EngineScopeBar :scope="scope" :installed="installedKinds" @pick="setScope" />
        <span class="count">{{ t('skills.count', { n: rows.length }) }}</span>
        <button
          class="omp-btn icon ghost"
          :title="t('common.refresh')"
          :class="{ 'is-loading': loading }"
          :disabled="loading"
          @click="load"
        >
          <AppIcon name="refresh" :size="15" />
        </button>
        <button class="omp-btn primary" @click="openCreate">
          <AppIcon name="plus" :size="14" />
          {{ t('skills.create') }}
        </button>
      </div>
    </header>

    <div class="page-body">
      <div class="filter-row">
        <input v-model="filter" class="omp-input" :placeholder="t('common.searchPlaceholder')" />
      </div>

      <section v-if="!rows.length && !loading" class="omp-empty">
        <span class="omp-tile omp-tile--lg omp-tile--neutral"><AppIcon name="book" :size="22" /></span>
        <p class="omp-empty-title">{{ t('skills.emptyTitle') }}</p>
        <p class="omp-empty-hint">{{ t('skills.emptyHint') }}</p>
        <button class="omp-btn sm primary" @click="openCreate">{{ t('skills.create') }}</button>
      </section>

      <p v-else-if="!filtered.length" class="no-match">{{ t('skills.noMatch') }}</p>

      <template v-else>
        <section v-for="group in groups" :key="group.source" class="source-group">
          <div class="omp-sec-head">
            <span class="omp-sec-title">{{ t(`skills.sources.${group.source}`) }} <span class="count">{{ group.rows.length }}</span></span>
          </div>

          <div class="skill-list">
            <SkillRowItem
              v-for="row in group.rows"
              :key="row.path"
              :row="row"
              :can-toggle="Boolean(caps?.skill_toggle) || row.kind === 'pi'"
              :toggling="toggling"
              :deleting="deleting"
              @toggle="toggle"
              @view="openContent"
              @remove="remove"
            />
          </div>
        </section>
      </template>
    </div>

    <SkillDrawer
      v-model:open="contentOpen"
      :row="contentRow"
      :content="content"
      :loading="contentLoading"
      :saving="saving"
      @save="saveContent"
    />

    <SkillCreateDialog v-model:open="createOpen" :creating="creating" @create="create" />
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}

.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--omp-sp-4);
  padding: var(--omp-sp-5) var(--omp-sp-6) var(--omp-sp-4);
  border-bottom: 1px solid var(--omp-line);
  flex: none;
}

.page-title {
  margin: 0;
  font-family: var(--omp-font-display);
  font-size: var(--omp-fs-xl);
  font-weight: 650;
  color: var(--omp-ink);
}

.page-sub {
  margin: 4px 0 0;
  max-width: var(--omp-w-desc, 620px);
  font-size: var(--omp-fs-sm);
  line-height: 1.65;
  color: var(--omp-mute);
}

.head-actions {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
}

.page-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: var(--omp-sp-5) var(--omp-sp-6) var(--omp-sp-10);
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-5);
}

.filter-row {
  flex: none;
  max-width: 360px;
}

.count {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  letter-spacing: 0.02em;
}

.source-group {
  display: flex;
  flex-direction: column;
}

.skill-list {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
}

/* 技能条目的样式随 SkillRow 搬走了 */
.no-match {
  margin: 0;
  padding: var(--omp-sp-6) 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  text-align: center;
}

/* 抽屉与新建弹窗的样式随 SkillDrawer / SkillCreateDialog 搬走了 */
</style>
