<script setup lang="ts">
// 技能页：按来源分组的清单 + 启停 + 正文抽屉 + 新建。
// 两个引擎的发现规则不同（omp 有 managed 目录与 enabled 开关，pi 用
// disable-model-invocation），界面同构、来源标签各自填充。
// 切换先动界面再发请求：慢一个来回的开关手感是坏的，失败再回滚。
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import AppIcon from '@/components/common/AppIcon.vue'
import CodeEditor from '@/components/common/CodeEditor.vue'
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
const caps = computed(() => app.capsOf(kind.value))

const rows = ref<SkillRow[]>([])
const loading = ref(false)
const filter = ref('')
const toggling = ref('')
const deleting = ref('')

const contentOpen = ref(false)
const contentRow = ref<SkillRow | null>(null)
const content = ref('')
// 编辑态只对用户目录（可写）的技能开放：工作区与插件来的那一份属于别人，
// 改了就变成「本地漂移」，下次拉代码或装包时静默被覆盖。
const editing = ref(false)
const draftContent = ref('')
const contentLoading = ref(false)
const saving = ref(false)

const createOpen = ref(false)
const creating = ref(false)
const draft = ref({ name: '', description: '', body: '' })

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

// 后端拒绝的这些字符会落到路径上，先在这里挡住，避免一次必然失败的请求
const nameInvalid = computed(() => !draft.value.name.trim() || /[\\/:*?"<>|]/.test(draft.value.name.trim()))

async function load() {
  loading.value = true
  try {
    const data = await api.skills(kind.value)
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
    await api.toggleSkill(row.path, enabled, kind.value)
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
  draftContent.value = ''
  editing.value = false
  contentOpen.value = true
  contentLoading.value = true
  try {
    const data = await api.skillContent(row.path)
    content.value = data.content ?? ''
    draftContent.value = content.value
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
    contentOpen.value = false
  } finally {
    contentLoading.value = false
  }
}

// 抽屉关掉时丢弃未保存的修改：留着它们会让「下次打开看到的是旧内容还是新草稿」
// 变成一件需要想的事，而抽屉本来只是「看一眼、改一下」。
watch(contentOpen, (open) => {
  if (!open) {
    editing.value = false
    draftContent.value = content.value
  }
})

const dirty = computed(() => editing.value && draftContent.value !== content.value)

async function saveContent() {
  const row = contentRow.value
  if (!row || saving.value) return
  saving.value = true
  try {
    const data = await api.saveSkillContent(kind.value, row.path, draftContent.value)
    content.value = data.content ?? draftContent.value
    draftContent.value = content.value
    editing.value = false
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
    await api.deleteSkill(row.path, kind.value)
    toasts.push('ok', t('common.saved'))
    await load()
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    deleting.value = ''
  }
}

function openCreate() {
  draft.value = { name: '', description: '', body: '' }
  createOpen.value = true
}

async function create() {
  if (nameInvalid.value || creating.value) return
  creating.value = true
  try {
    await api.createSkill(draft.value.name.trim(), draft.value.description.trim(), draft.value.body, kind.value)
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
        <p class="page-sub">{{ t('skills.subtitle') }}</p>
      </div>
      <div class="head-actions">
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

      <section v-if="!rows.length && !loading" class="empty">
        <div class="empty-mark"><AppIcon name="book" :size="22" /></div>
        <p class="empty-title">{{ t('skills.emptyTitle') }}</p>
        <p class="empty-hint">{{ t('skills.emptyHint') }}</p>
      </section>

      <p v-else-if="!filtered.length" class="empty-hint no-match">{{ t('skills.noMatch') }}</p>

      <template v-else>
        <section v-for="group in groups" :key="group.source" class="source-group">
          <div class="omp-section-title">
            {{ t(`skills.sources.${group.source}`) }}
            <span class="count">{{ group.rows.length }}</span>
          </div>

          <div class="skill-list">
            <article v-for="row in group.rows" :key="row.path" class="skill-row card-pad">
              <div class="skill-main">
                <div class="skill-head">
                  <span class="skill-name">{{ row.name }}</span>
                  <span class="tag">{{ t(`skills.sources.${row.source}`) }}</span>
                  <span v-if="!row.editable" class="tag readonly-tag">{{ t('skills.readonly') }}</span>
                </div>
                <p v-if="row.description" class="skill-desc">{{ row.description }}</p>
                <div class="skill-path selectable">{{ row.dir }}</div>
              </div>

              <div class="skill-actions">
                <!-- 引擎没有启停开关时不渲染控件（不渲染成灰开关） -->
                <el-switch
                  v-if="caps?.skill_toggle || row.kind === 'pi'"
                  :model-value="row.enabled"
                  :loading="toggling === row.path"
                  :disabled="Boolean(toggling) && toggling !== row.path"
                  :title="row.enabled ? t('common.enabled') : t('common.disabled')"
                  @change="(value: string | number | boolean) => toggle(row, Boolean(value))"
                />
                <button class="omp-btn text sm" @click="openContent(row)">{{ t('skills.view') }}</button>
                <button
                  v-if="row.editable"
                  class="omp-btn text sm danger"
                  :class="{ 'is-loading': deleting === row.path }"
                  :disabled="Boolean(deleting)"
                  @click="remove(row)"
                >
                  {{ t('common.delete') }}
                </button>
              </div>
            </article>
          </div>
        </section>
      </template>
    </div>

    <el-drawer v-model="contentOpen" :title="contentRow?.name || t('skills.view')" size="720px">
      <div v-if="contentLoading" class="drawer-loading">{{ t('common.loading') }}</div>
      <template v-else>
        <div class="drawer-bar">
          <span class="drawer-path mono selectable">{{ contentRow?.path }}</span>
          <span class="push" />
          <template v-if="contentRow?.editable">
            <template v-if="editing">
              <button class="omp-btn sm" :disabled="saving" @click="editing = false; draftContent = content">
                {{ t('common.cancel') }}
              </button>
              <button
                class="omp-btn primary sm"
                :class="{ 'is-loading': saving }"
                :disabled="saving || !dirty"
                @click="saveContent"
              >
                {{ t('common.save') }}
              </button>
            </template>
            <button v-else class="omp-btn sm" @click="editing = true">
              <AppIcon name="edit" :size="13" />
              {{ t('common.edit') }}
            </button>
          </template>
          <span v-else class="tag readonly-tag">{{ t('skills.readonly') }}</span>
        </div>
        <CodeEditor v-model="draftContent" lang="markdown" :readonly="!editing" min-height="70vh" />
      </template>
    </el-drawer>

    <el-dialog v-model="createOpen" :title="t('skills.createTitle')" width="560px">
      <div class="form">
        <label class="field">
          <span class="field-label">{{ t('skills.nameLabel') }}</span>
          <input v-model="draft.name" class="omp-input" :placeholder="t('skills.nameHint')" />
          <span v-if="nameInvalid && draft.name" class="field-hint bad">{{ t('skills.nameHint') }}</span>
        </label>
        <label class="field">
          <span class="field-label">{{ t('skills.descLabel') }}</span>
          <input v-model="draft.description" class="omp-input" :placeholder="t('skills.descHint')" />
        </label>
        <label class="field">
          <span class="field-label">{{ t('skills.bodyLabel') }}</span>
          <textarea v-model="draft.body" class="omp-input" rows="10" :placeholder="t('skills.bodyHint')" />
        </label>
      </div>

      <template #footer>
        <div class="dialog-foot">
          <button class="omp-btn" :disabled="creating" @click="createOpen = false">{{ t('common.cancel') }}</button>
          <button
            class="omp-btn primary"
            :class="{ 'is-loading': creating }"
            :disabled="creating || nameInvalid"
            @click="create"
          >
            {{ t('common.create') }}
          </button>
        </div>
      </template>
    </el-dialog>
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

.skill-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--omp-sp-4);
  padding: var(--omp-sp-3) var(--omp-sp-4);
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
  min-width: 0;
}

.skill-name {
  font-size: var(--omp-fs-sm);
  font-weight: 650;
  color: var(--omp-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.skill-desc {
  margin: 0;
  font-size: var(--omp-fs-xs);
  line-height: 1.55;
  color: var(--omp-mute);
}

.skill-path {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 只读标签压暗：它是状态说明，不是可点的分类 */
.readonly-tag {
  opacity: 0.65;
}

.skill-actions {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex: none;
}

.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-10);
  text-align: center;
}

.empty-mark {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  border-radius: var(--omp-r-lg);
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
}

.empty-title {
  margin: 0;
  font-size: var(--omp-fs-sm);
  font-weight: 600;
  color: var(--omp-ink);
}

.empty-hint {
  margin: 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
}

.no-match {
  padding: var(--omp-sp-6) 0;
}

.drawer-loading {
  font-size: var(--omp-fs-sm);
  color: var(--omp-mute);
}

/* 抽屉顶部一条工具行：路径 + 右侧动作。路径占剩余宽度，动作不换行。 */
.drawer-bar {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  margin-bottom: var(--omp-sp-3);
  min-height: var(--omp-ctl-sm);
}

.drawer-path {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.push {
  flex: 1;
}

.form {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-4);
}

.field {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-1);
}

.field-label {
  font-size: var(--omp-fs-2xs);
  font-weight: 600;
  color: var(--omp-ink-5);
}

.field-hint {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.field-hint.bad {
  color: var(--omp-bad);
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--omp-sp-2);
}
</style>
