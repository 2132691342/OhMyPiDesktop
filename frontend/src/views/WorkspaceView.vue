<script setup lang="ts">
// 工作区页：文件树 + 改动清单 + 差异 + 提交/分支。
//
// 它回答一个真问题：「助手到底改了什么，我能不能收成一次提交」。
// 读全开放；写只做白名单动作，且每个动作都由用户点下去。
// 没有 git 或不是仓库时，页面显示原因与「初始化」，不显示空表格。
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import AppIcon from '@/components/common/AppIcon.vue'
import DiffView from '@/components/workspace/DiffView.vue'
import FilePanel from '@/components/workspace/FilePanel.vue'
import { api } from '@/api/http'
import { useAppStore } from '@/stores/app'
import { useToasts } from '@/composables/useToasts'
import type { GitBranch, GitCommit, GitDiff, GitFile, GitStatus } from '@/types/api'

const { t } = useI18n()
const app = useAppStore()
const toasts = useToasts()

const status = ref<GitStatus | null>(null)
const diff = ref<GitDiff | null>(null)
const branches = ref<GitBranch[]>([])
const history = ref<GitCommit[]>([])

const loadingStatus = ref(false)
const loadingDiff = ref(false)
const selected = ref('')
const message = ref('')
const committing = ref(false)
const pending = ref('')
const tab = ref<'changes' | 'history'>('changes')

const unstaged = computed(() => (status.value?.files ?? []).filter((f) => !f.staged))
const staged = computed(() => (status.value?.files ?? []).filter((f) => f.staged))
const conflicts = computed(() => (status.value?.files ?? []).filter((f) => f.conflict))
const canCommit = computed(() => Boolean(message.value.trim()) && staged.value.length > 0 && !committing.value)

async function loadStatus() {
  loadingStatus.value = true
  try {
    status.value = await api.gitStatus()
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    loadingStatus.value = false
  }
}

async function loadDiff(path = selected.value) {
  if (!status.value?.is_repo) return
  loadingDiff.value = true
  try {
    diff.value = await api.gitDiff(path)
  } catch {
    diff.value = null
  } finally {
    loadingDiff.value = false
  }
}

async function loadExtras() {
  if (!status.value?.is_repo) {
    branches.value = []
    history.value = []
    return
  }
  try {
    branches.value = await api.gitBranches()
  } catch {
    branches.value = []
  }
  try {
    history.value = await api.gitHistory(40)
  } catch {
    history.value = []
  }
}

async function refresh() {
  await loadStatus()
  await Promise.all([loadDiff(), loadExtras()])
}

function pick(file: GitFile) {
  selected.value = file.path
  void loadDiff(file.path)
}

// 写动作统一走这里：挂 loading、成功刷新状态、失败给原文。
async function act(name: string, fn: () => Promise<GitStatus | void>) {
  pending.value = name
  try {
    const next = await fn()
    if (next) status.value = next
    await loadStatus()
    await Promise.all([loadDiff(), loadExtras()])
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    pending.value = ''
  }
}

async function stageAll() {
  await act('stage', () => api.gitStage(unstaged.value.map((f) => f.path)))
}

async function stage(file: GitFile) {
  await act(`stage:${file.path}`, () => api.gitStage([file.path]))
}

async function unstage(file: GitFile) {
  await act(`unstage:${file.path}`, () => api.gitUnstage([file.path]))
}

async function discard(file: GitFile) {
  try {
    await ElMessageBox.confirm(t('workspace.discardConfirm', { path: file.path }), t('workspace.discardTitle'), {
      confirmButtonText: t('workspace.discard'),
      cancelButtonText: t('common.cancel'),
      type: 'warning',
    })
  } catch {
    return
  }
  await act(`discard:${file.path}`, () => api.gitDiscard([file.path]))
}

async function commit() {
  if (!canCommit.value) return
  committing.value = true
  try {
    const result = await api.gitCommit(message.value.trim())
    status.value = result.status
    message.value = ''
    toasts.push('ok', t('workspace.committed', { hash: result.hash.slice(0, 7) }))
    await Promise.all([loadDiff(), loadExtras()])
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    committing.value = false
  }
}

async function resolveConflicts() {
  await act('resolve', () => api.gitResolve(conflicts.value.map((f) => f.path)))
}

async function stateAction(action: 'continue' | 'abort') {
  if (action === 'abort') {
    try {
      await ElMessageBox.confirm(t('workspace.abortConfirm'), t('workspace.abortTitle'), {
        confirmButtonText: t('workspace.abort'),
        cancelButtonText: t('common.cancel'),
        type: 'warning',
      })
    } catch {
      return
    }
  }
  await act(`state:${action}`, () => api.gitState(action))
}

async function initRepo() {
  await act('init', () => api.gitInit('main'))
}

async function switchBranch(name: string) {
  await act(`switch:${name}`, () => api.gitSwitchBranch(name))
}

async function createBranch() {
  try {
    const result = await ElMessageBox.prompt(t('workspace.branchName'), t('workspace.newBranch'), {
      confirmButtonText: t('common.save'),
      cancelButtonText: t('common.cancel'),
    })
    await act('branch', () => api.gitCreateBranch(result.value))
  } catch {
    /* 用户取消 */
  }
}

function changeTag(file: GitFile): string {
  if (file.conflict) return 'U'
  return file.status || '?'
}

onMounted(refresh)
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">{{ t('workspace.title') }}</h1>
        <p class="page-sub mono selectable">{{ app.workspace }}</p>
      </div>
      <div class="head-actions">
        <span v-if="status?.is_repo" class="tag" :class="status.conflict ? 'warn' : ''">
          <AppIcon name="branch" :size="11" />
          {{ status.branch || t('workspace.noCommit') }}
        </span>
        <span v-if="status?.is_repo && status.upstream" class="readout">
          ↑{{ status.ahead }} ↓{{ status.behind }}
        </span>
        <button class="omp-btn icon ghost" :class="{ 'is-loading': loadingStatus }" :title="t('common.refresh')" @click="refresh">
          <AppIcon name="refresh" :size="15" />
        </button>
      </div>
    </header>

    <!-- 不可用：说清原因与下一步 -->
    <div v-if="status && !status.available" class="notice">
      <span class="dot warn" />
      <span>{{ status.reason }}</span>
    </div>

    <div v-else-if="status && !status.is_repo" class="notice">
      <span class="dot warn" />
      <span>{{ t('workspace.notRepo') }}</span>
      <button class="omp-btn sm" :class="{ 'is-loading': pending === 'init' }" :disabled="Boolean(pending)" @click="initRepo">
        {{ t('workspace.init') }}
      </button>
    </div>

    <template v-else-if="status">
      <!-- 中间态横幅：冲突时的处置入口 -->
      <div v-if="status.state !== 'none' || status.conflict" class="notice warn">
        <span class="dot warn" />
        <span>{{ status.state_detail || t('workspace.inProgress') }}</span>
        <button class="omp-btn sm" :class="{ 'is-loading': pending === 'resolve' }" :disabled="!conflicts.length || Boolean(pending)" @click="resolveConflicts">
          {{ t('workspace.markResolved') }}
        </button>
        <button class="omp-btn sm" :class="{ 'is-loading': pending === 'state:continue' }" :disabled="Boolean(pending)" @click="stateAction('continue')">
          {{ t('workspace.continue') }}
        </button>
        <button class="omp-btn sm danger" :class="{ 'is-loading': pending === 'state:abort' }" :disabled="Boolean(pending)" @click="stateAction('abort')">
          {{ t('workspace.abort') }}
        </button>
      </div>

      <div class="split">
        <!-- 左：文件树 -->
        <FilePanel class="pane-files" />

        <!-- 中：改动 + 提交 -->
        <div class="pane-mid">
          <div class="tabs seg">
            <button class="seg-btn" :class="{ active: tab === 'changes' }" @click="tab = 'changes'">
              {{ t('workspace.changes') }}
              <span v-if="status.files.length" class="count">{{ status.files.length }}</span>
            </button>
            <button class="seg-btn" :class="{ active: tab === 'history' }" @click="tab = 'history'">
              {{ t('workspace.history') }}
              <span v-if="history.length" class="count">{{ history.length }}</span>
            </button>
          </div>

          <div v-if="tab === 'changes'" class="mid-body">
            <div v-if="!status.files.length" class="empty-mini">{{ t('workspace.noChanges') }}</div>

            <template v-else>
              <div v-if="staged.length" class="group">
                <div class="group-head">
                  <span>{{ t('workspace.staged') }}</span>
                  <span class="count">{{ staged.length }}</span>
                </div>
                <div
                  v-for="file in staged"
                  :key="`s:${file.path}`"
                  class="file-line omp-row-click"
                  :class="{ active: selected === file.path }"
                  @click="pick(file)"
                >
                  <span class="chg" :class="`chg-${changeTag(file)}`">{{ changeTag(file) }}</span>
                  <span class="path">{{ file.path }}</span>
                  <span class="num"><b class="add">+{{ file.additions }}</b><b class="del">−{{ file.deletions }}</b></span>
                  <button class="omp-btn text sm" :disabled="Boolean(pending)" @click.stop="unstage(file)">
                    {{ t('workspace.unstage') }}
                  </button>
                </div>
              </div>

              <div v-if="unstaged.length" class="group">
                <div class="group-head">
                  <span>{{ t('workspace.unstaged') }}</span>
                  <span class="count">{{ unstaged.length }}</span>
                  <button class="omp-btn text sm push" :disabled="Boolean(pending)" @click="stageAll">
                    {{ t('workspace.stageAll') }}
                  </button>
                </div>
                <div
                  v-for="file in unstaged"
                  :key="`u:${file.path}`"
                  class="file-line omp-row-click"
                  :class="{ active: selected === file.path }"
                  @click="pick(file)"
                >
                  <span class="chg" :class="`chg-${changeTag(file)}`">{{ changeTag(file) }}</span>
                  <span class="path">{{ file.path }}</span>
                  <span class="num"><b class="add">+{{ file.additions }}</b><b class="del">−{{ file.deletions }}</b></span>
                  <button class="omp-btn text sm" :disabled="Boolean(pending)" @click.stop="stage(file)">
                    {{ t('workspace.stage') }}
                  </button>
                  <button class="omp-btn text sm danger" :disabled="Boolean(pending)" @click.stop="discard(file)">
                    {{ t('workspace.discard') }}
                  </button>
                </div>
              </div>
            </template>

            <div class="commit-box">
              <textarea
                v-model="message"
                class="omp-input"
                rows="3"
                :placeholder="t('workspace.commitPlaceholder')"
              />
              <button
                class="omp-btn primary"
                :class="{ 'is-loading': committing }"
                :disabled="!canCommit"
                @click="commit"
              >
                {{ t('workspace.commit', { n: staged.length }) }}
              </button>
            </div>

            <div class="branch-row">
              <span class="plate">{{ t('workspace.branches') }}</span>
              <button class="omp-btn text sm" :disabled="Boolean(pending)" @click="createBranch">
                {{ t('workspace.newBranch') }}
              </button>
            </div>
            <div class="branch-list">
              <button
                v-for="branch in branches"
                :key="`${branch.remote}:${branch.name}`"
                class="branch-line omp-row-click"
                :class="{ active: branch.current }"
                :disabled="branch.current || Boolean(pending)"
                @click="switchBranch(branch.name)"
              >
                <span>{{ branch.name }}</span>
                <span v-if="branch.remote" class="tag">{{ branch.remote }}</span>
                <span v-if="branch.ahead || branch.behind" class="readout">↑{{ branch.ahead }} ↓{{ branch.behind }}</span>
              </button>
            </div>
          </div>

          <div v-else class="mid-body">
            <div v-if="!history.length" class="empty-mini">{{ t('workspace.noHistory') }}</div>
            <div v-for="item in history" :key="item.hash" class="commit-line">
              <span class="hash mono">{{ item.short }}</span>
              <span class="subject">{{ item.subject }}</span>
              <span class="readout">{{ item.author }}</span>
              <span class="num"><b class="add">+{{ item.additions }}</b><b class="del">−{{ item.deletions }}</b></span>
            </div>
          </div>
        </div>

        <!-- 右：差异 -->
        <DiffView class="pane-diff" :diff="diff" :path="selected" :loading="loadingDiff" />
      </div>
    </template>

    <div v-else class="empty">
      <span class="dot busy" />
      {{ t('common.loading') }}
    </div>
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
  font-size: var(--omp-fs-2xs);
  color: var(--omp-faint);
}
.head-actions {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
}
.notice {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  margin: var(--omp-sp-4) var(--omp-sp-6) 0;
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r);
  background: var(--omp-surface);
  font-size: var(--omp-fs-xs);
  color: var(--omp-ink);
  flex-wrap: wrap;
  flex: none;
}
.notice.warn {
  border-color: var(--omp-warn);
  background: var(--omp-warn-soft);
}
.split {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: 220px minmax(280px, 1fr) minmax(320px, 1.2fr);
  gap: var(--omp-sp-3);
  padding: var(--omp-sp-4) var(--omp-sp-6) var(--omp-sp-5);
  overflow: hidden;
}
.pane-files,
.pane-diff {
  min-height: 0;
}
.pane-mid {
  display: flex;
  flex-direction: column;
  min-height: 0;
  gap: var(--omp-sp-2);
}
.mid-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-3);
}
.group-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  font-size: var(--omp-fs-2xs);
  font-weight: 650;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--omp-faint);
  margin-bottom: var(--omp-sp-1);
}
.push {
  margin-left: auto;
}
.file-line {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: 3px var(--omp-sp-2);
  border: none;
  background: transparent;
  color: var(--omp-ink);
  font-size: var(--omp-fs-xs);
  text-align: left;
  border-radius: var(--omp-r-sm);
  cursor: pointer;
}
.file-line .path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--omp-font-mono);
}
.file-line .num,
.commit-line .num {
  display: flex;
  gap: 6px;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  flex: none;
}
.add {
  color: var(--omp-ok);
}
.del {
  color: var(--omp-bad);
}
.chg {
  width: 16px;
  text-align: center;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  font-weight: 700;
  flex: none;
}
.chg-M { color: var(--omp-warn); }
.chg-A { color: var(--omp-ok); }
.chg-D { color: var(--omp-bad); }
.chg-R,
.chg-C { color: var(--omp-brand); }
.chg-U { color: var(--omp-bad); }
.chg-\? { color: var(--omp-faint); }
.commit-box {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  flex: none;
}
.branch-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex: none;
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
.branch-line.active {
  color: var(--omp-brand);
  font-weight: 600;
}
.commit-line {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  padding: 3px var(--omp-sp-2);
  font-size: var(--omp-fs-xs);
}
.commit-line .hash {
  color: var(--omp-brand);
  font-size: var(--omp-fs-3xs);
  flex: none;
}
.commit-line .subject {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.empty-mini {
  padding: var(--omp-sp-4);
  color: var(--omp-faint);
  font-size: var(--omp-fs-2xs);
  text-align: center;
}
.empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--omp-sp-2);
  color: var(--omp-faint);
}
</style>
