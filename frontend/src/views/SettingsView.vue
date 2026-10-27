<script setup lang="ts">
// 设置页：外观 / 环境 / 工作区 / 关于。
// 「环境」这一节只做两件事：说清引擎在哪、没找到时给可复制的安装命令。
// 本应用不下载、不安装、不改 PATH——装上装不上是用户自己的事。
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import { api } from '@/api/http'
import { useAppStore } from '@/stores/app'
import { useToasts } from '@/composables/useToasts'

const { t } = useI18n()
const app = useAppStore()
const toasts = useToasts()

const saving = ref(false)
const logs = ref<string[]>([])
const detecting = ref(false)
const restarting = ref('')
const copiedCmd = ref('')
const profileDraft = ref(app.profile)
const pathDrafts = ref<Record<string, string>>({})
const pathSaving = ref('')

// 最近工作目录：后端合并了「记住的」与「会话里推出来的」，这里只负责点选与移除。
const recentRows = computed(() => app.workspaces.filter((r) => r.exists))
const forgetting = ref('')

// 每个引擎一张卡：探测结果、版本、显式路径、落盘位置、重启。
// 安装命令只展示、不执行——装上装不上是用户自己的事。
const installed = computed(() => app.engines?.engines ?? [])

const pathFields = computed(() => [
  { key: 'root', label: t('settings.layoutRoot') },
  { key: 'agent', label: t('settings.layoutAgent') },
  { key: 'sessions', label: t('settings.layoutSessions') },
  { key: 'skills', label: t('settings.layoutSkills') },
  { key: 'plugins', label: t('settings.layoutPlugins') },
  { key: 'models', label: t('settings.layoutModels') },
  { key: 'settings', label: t('settings.layoutSettings') },
])

function exeDraft(kind: string) {
  return pathDrafts.value[`exe:${kind}`] ?? app.settings[`engine.path.${kind}`] ?? ''
}

function setExeDraft(kind: string, value: string) {
  pathDrafts.value = { ...pathDrafts.value, [`exe:${kind}`]: value }
}

async function copyCommand(cmd: string) {
  try {
    await navigator.clipboard.writeText(cmd)
    copiedCmd.value = cmd
    toasts.push('ok', t('settings.copied'))
    window.setTimeout(() => {
      if (copiedCmd.value === cmd) copiedCmd.value = ''
    }, 2400)
  } catch {
    copiedCmd.value = ''
  }
}

// 重新探测：两个引擎各起一次子进程（pi 要起 Node），所以要给 loading。
async function detectEngines() {
  detecting.value = true
  try {
    await app.detectEngines()
    toasts.push('ok', t('settings.detected'))
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    detecting.value = false
  }
}

async function restartEngines(kind: string) {
  restarting.value = kind
  try {
    await api.restartEngines(kind as 'omp' | 'pi')
    await app.refreshEngines()
    toasts.push('ok', t('settings.restartEngine'))
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    restarting.value = ''
  }
}

// save 只负责保存与报错；成功与否用返回值告知调用处。
// 以前这里吞掉错误，调用处无条件弹「已保存」——失败也显示成功，是最差的一类提示。
async function save(kv: Record<string, string>): Promise<boolean> {
  saving.value = true
  try {
    await app.saveSettings(kv)
    return true
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
    return false
  } finally {
    saving.value = false
  }
}

async function saveEnginePath(kind: string) {
  pathSaving.value = `exe:${kind}`
  const ok = await save({ [`engine.path.${kind}`]: exeDraft(kind).trim() })
  pathSaving.value = ''
  if (!ok) return
  await app.detectEngines()
  toasts.push('ok', t('common.saved'))
}

async function saveProfile() {
  if (!(await save({ 'app.profile': profileDraft.value.trim() }))) return
  toasts.push('ok', t('common.saved'))
}

async function loadLogs() {
  try {
    const data = await api.logs()
    logs.value = data.lines.slice(-200)
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  }
}

async function revealPath(path: string) {
  try {
    await api.reveal(path)
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  }
}

async function useWorkspace(path: string) {
  if (path === app.workspace) return
  await app.setWorkspace(path)
  toasts.push('ok', t('settings.workspaceSwitched'))
}

// 移除只影响「最近」这份记忆，不动目录本身。
async function forgetWorkspace(path: string) {
  forgetting.value = path
  try {
    const res = await api.forgetWorkspace(path)
    app.workspaces = res.workspaces ?? []
    toasts.push('ok', t('settings.workspaceForgotten'))
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    forgetting.value = ''
  }
}

onMounted(() => {
  void loadLogs()
  void app.refreshEngines()
})

/** 某引擎的落盘布局（Bootstrap 里给全，缺省时用本机解析结果）。 */
function layoutOf(kind: string) {
  return app.boot?.layouts?.[kind] ?? null
}
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">{{ t('settings.title') }}</h1>
        <p class="page-sub">{{ t('settings.subtitle') }}</p>
      </div>
    </header>

    <div class="page-body">
      <!-- 外观 -->
      <section class="card-pad">
        <div class="omp-section-title">{{ t('settings.groups.appearance') }}</div>
        <div class="set-row">
          <div class="set-label">{{ t('settings.theme') }}</div>
          <div class="seg">
            <button class="seg-btn" :class="{ active: app.theme === 'light' }" @click="save({ 'ui.theme': 'light' })">
              {{ t('settings.themeLight') }}
            </button>
            <button class="seg-btn" :class="{ active: app.theme === 'dark' }" @click="save({ 'ui.theme': 'dark' })">
              {{ t('settings.themeDark') }}
            </button>
          </div>
        </div>
        <div class="set-row">
          <div class="set-label">{{ t('settings.fontScale') }}</div>
          <el-slider
            :model-value="Number(app.settings['ui.font_scale'] ?? 1)"
            :min="0.85"
            :max="1.3"
            :step="0.05"
            :show-tooltip="false"
            style="max-width: 260px"
            @change="(v: number | number[]) => save({ 'ui.font_scale': String(Array.isArray(v) ? v[0] : v) })"
          />
        </div>
        <div class="set-row">
          <div>
            <div class="set-label">{{ t('settings.reduceMotion') }}</div>
            <div class="set-hint">{{ t('settings.reduceMotionHint') }}</div>
          </div>
          <el-switch
            :model-value="app.settings['ui.reduce_motion'] === '1'"
            @change="(v: string | number | boolean) => save({ 'ui.reduce_motion': v ? '1' : '0' })"
          />
        </div>
      </section>

      <!-- 环境：每个引擎一张卡。不安装，只探测 + 给命令原文 -->
      <section class="card-pad">
        <div class="omp-section-title">
          {{ t('settings.groups.engine') }}
          <span class="tag" :class="installed.some((e) => e.installed) ? 'ok' : 'warn'">
            {{ installed.filter((e) => e.installed).length }}/{{ installed.length }}
          </span>
          <button
            class="omp-btn text sm push"
            :class="{ 'is-loading': detecting }"
            :disabled="detecting"
            @click="detectEngines"
          >
            {{ t('settings.redetect') }}
          </button>
        </div>

        <article v-for="item in installed" :key="item.kind" class="engine-card">
          <div class="engine-head">
            <span class="dot" :class="item.installed ? 'ok' : 'warn'" />
            <span class="engine-name">{{ item.kind }}</span>
            <span class="tag" :class="item.installed ? 'ok' : 'warn'">
              {{ item.installed ? item.version : t('settings.engineSource.none') }}
            </span>
            <span v-if="item.installed" class="tag">{{ item.source }}</span>
            <span class="push" />
            <button
              class="omp-btn sm"
              :class="{ 'is-loading': restarting === item.kind }"
              :disabled="!item.installed || Boolean(restarting)"
              @click="restartEngines(item.kind)"
            >
              {{ t('settings.restartEngine') }}
            </button>
          </div>

          <div v-if="!item.installed" class="cmd-row">
            <code class="cmd-text selectable">{{ app.installHint(item.kind) }}</code>
            <button class="omp-btn sm ghost" @click="copyCommand(app.installHint(item.kind))">
              {{ copiedCmd === app.installHint(item.kind) ? t('settings.copied') : t('settings.copyCommand') }}
            </button>
          </div>

          <div v-else class="engine-facts">
            <div class="fact wide">
              <span class="fact-label">{{ t('settings.engineExe') }}</span>
              <span class="fact-value mono selectable">{{ item.exe }}</span>
            </div>
          </div>

          <div class="set-row top compact">
            <div>
              <div class="set-label">{{ t('settings.engineExe') }}</div>
              <div class="set-hint">{{ t('settings.engineExeHint') }}</div>
            </div>
            <div class="inline-input">
              <input
                class="omp-input"
                :value="exeDraft(item.kind)"
                :placeholder="item.exe || t('settings.engineExePlaceholder')"
                @input="setExeDraft(item.kind, ($event.target as HTMLInputElement).value)"
              />
              <button
                class="omp-btn"
                :class="{ 'is-loading': pathSaving === `exe:${item.kind}` }"
                :disabled="pathSaving === `exe:${item.kind}`"
                @click="saveEnginePath(item.kind)"
              >
                {{ t('settings.apply') }}
              </button>
            </div>
          </div>

          <div v-if="layoutOf(item.kind)" class="layout-grid">
            <div v-for="field in pathFields" :key="field.key" class="layout-row">
              <span class="layout-label">{{ field.label }}</span>
              <button
                class="layout-path mono selectable"
                :title="t('settings.reveal')"
                @click="revealPath((layoutOf(item.kind) as any)[field.key])"
              >
                {{ (layoutOf(item.kind) as any)[field.key] || '—' }}
              </button>
            </div>
          </div>
        </article>

        <!-- profile 只有 omp 认 -->
        <div class="set-row top">
          <div>
            <div class="set-label">{{ t('settings.profile') }}</div>
            <div class="set-hint">{{ t('settings.profileHint') }}</div>
          </div>
          <div class="inline-input">
            <input v-model="profileDraft" class="omp-input" placeholder="default" />
            <button class="omp-btn" :class="{ 'is-loading': saving }" @click="saveProfile">{{ t('common.save') }}</button>
          </div>
        </div>
      </section>

      <!-- 工作区 -->
      <section class="card-pad">
        <div class="omp-section-title">{{ t('settings.groups.workspace') }}</div>
        <div class="set-row">
          <div>
            <div class="set-label">{{ t('settings.workspace') }}</div>
            <div class="set-hint mono selectable">{{ app.workspace }}</div>
          </div>
          <button class="omp-btn" @click="app.chooseWorkspace()">
            <AppIcon name="folder" :size="14" />
            {{ t('chat.pickWorkspace') }}
          </button>
        </div>

        <!-- 最近用过的目录：一行一个，点行即切，× 只从记忆里去掉 -->
        <div v-if="recentRows.length" class="recent-block">
          <div class="set-label">{{ t('settings.recentWorkspaces') }}</div>
          <ul class="recent-list">
            <li v-for="row in recentRows" :key="row.workspace_path" class="recent-row">
              <button
                class="recent-open omp-row-click"
                :class="{ 'is-current': row.workspace_path === app.workspace }"
                :disabled="row.workspace_path === app.workspace"
                @click="useWorkspace(row.workspace_path)"
              >
                <AppIcon name="folder" :size="13" />
                <span class="mono recent-path">{{ row.workspace_path }}</span>
              </button>
              <button
                class="omp-btn text sm recent-forget"
                :class="{ 'is-loading': forgetting === row.workspace_path }"
                :title="t('settings.forgetWorkspace')"
                @click="forgetWorkspace(row.workspace_path)"
              >
                <AppIcon name="close" :size="13" />
              </button>
            </li>
          </ul>
        </div>
        <div v-else class="set-hint">{{ t('settings.recentWorkspacesEmpty') }}</div>
      </section>

      <!-- 关于 -->
      <section class="card-pad">
        <div class="omp-section-title">{{ t('settings.groups.about') }}</div>
        <div class="set-row">
          <span class="set-label">{{ t('settings.aboutVersion') }}</span>
          <span class="fact-value">{{ app.version }}</span>
        </div>
        <div class="set-row">
          <span class="set-label">{{ t('settings.aboutEngine') }}</span>
          <span class="fact-value">
            {{ installed.filter((e) => e.installed).map((e) => `${e.kind} ${e.version}`).join(' · ') || t('common.unknown') }}
          </span>
        </div>

        <div class="set-row top">
          <div>
            <div class="set-label">{{ t('settings.log') }}</div>
            <div class="set-hint">{{ t('settings.logHint') }}</div>
          </div>
          <button class="omp-btn sm" @click="loadLogs">{{ t('common.refresh') }}</button>
        </div>
        <pre class="log selectable">{{ logs.join('\n') || t('common.empty') }}</pre>
      </section>
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
  max-width: 720px;
  font-size: var(--omp-fs-sm);
  color: var(--omp-mute);
  line-height: 1.65;
}

.page-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: var(--omp-sp-5) var(--omp-sp-6) var(--omp-sp-10);
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-4);
}

.set-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--omp-sp-4);
  padding: var(--omp-sp-2) 0;
  border-bottom: 1px solid var(--omp-line);
}

.set-row:last-child {
  border-bottom: none;
}

.set-row.top {
  align-items: flex-start;
}

.set-label {
  font-size: var(--omp-fs-sm);
  font-weight: 550;
  color: var(--omp-ink);
}

.set-hint {
  margin-top: 2px;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  line-height: 1.55;
}

.mono {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
}

.seg {
  display: flex;
  gap: 2px;
  padding: 2px;
  border: 1px solid var(--omp-line);
  border-radius: var(--omp-r);
  background: var(--omp-window);
}




.engine-summary {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--omp-sp-4);
  padding: var(--omp-sp-3) 0;
  flex-wrap: wrap;
}

.engine-facts {
  display: flex;
  gap: var(--omp-sp-6);
}

.fact {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.fact-label {
  font-size: var(--omp-fs-3xs);
  font-weight: 650;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--omp-faint);
}

.fact-value {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-sm);
  color: var(--omp-ink);
}

.fact-value.brand {
  color: var(--omp-brand);
}

.engine-actions {
  display: flex;
  gap: var(--omp-sp-2);
  flex-wrap: wrap;
}

.setup {
  padding: var(--omp-sp-3) 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.setup-msg {
  margin: 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  font-family: var(--omp-font-mono);
}

.inline-input {
  display: flex;
  gap: var(--omp-sp-2);
  width: min(520px, 100%);
}

.path-list {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
  flex: 1;
  max-width: 620px;
}

.path-row {
  display: flex;
  gap: var(--omp-sp-2);
  font-size: var(--omp-fs-3xs);
}

.path-key {
  flex: none;
  width: 96px;
  color: var(--omp-faint);
  font-family: var(--omp-font-mono);
}

.path-val {
  color: var(--omp-ink-5);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log {
  margin: var(--omp-sp-2) 0 0;
  max-height: 220px;
  overflow: auto;
  padding: var(--omp-sp-3);
  border-radius: var(--omp-r);
  background: var(--omp-code-bg);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  line-height: 1.6;
  color: var(--omp-ink-5);
  white-space: pre-wrap;
}

/* 缺引擎：说明 + 可复制的安装命令原文。不用弹窗，
   因为这不是一次需要用户立刻决定的错误，是一个环境事实。 */
.missing {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  padding: var(--omp-sp-3) var(--omp-sp-4);
  margin-bottom: var(--omp-sp-4);
  border: 1px solid var(--omp-line);
  border-left: 2px solid var(--omp-warn);
  border-radius: var(--omp-r);
  background: var(--omp-warn-soft);
}

.missing-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  font-size: var(--omp-fs-sm);
  color: var(--omp-ink);
}

.missing-hint {
  margin: 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
}

.cmd-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  flex-wrap: wrap;
}

.cmd-label {
  flex: none;
  width: 108px;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.cmd-text {
  flex: 1;
  min-width: 0;
  padding: 4px 8px;
  border-radius: var(--omp-r-xs);
  background: var(--omp-code-bg);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-ink);
  overflow-wrap: anywhere;
}

/* 最近工作目录：整行元素按规范不放大（行宽 100%，放大会溢出被裁），
   按下缩到 0.985；五态的缩放节奏不在这里重定义，只接 .omp-pressable。 */
.recent-block {
  margin-top: var(--omp-sp-4);
}

.recent-list {
  list-style: none;
  margin: var(--omp-sp-2) 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.recent-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
}

.recent-open {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  height: var(--omp-ctl);
  padding: 0 var(--omp-sp-3);
  border: 1px solid transparent;
  border-radius: var(--omp-r-sm);
  background: transparent;
  color: var(--omp-ink);
  font-size: var(--omp-fs-sm);
  text-align: left;
  cursor: pointer;
  transition: background var(--omp-dur-fast) ease, color var(--omp-dur-fast) ease,
    transform var(--omp-dur-fast) ease;
}

.recent-open:hover:not(:disabled) {
  background: var(--omp-hover);
}

.recent-open:active:not(:disabled) {
  transform: scale(0.985);
}

.recent-open:disabled {
  opacity: 1;
  cursor: default;
  color: var(--omp-brand);
  background: var(--omp-selected);
}

.recent-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recent-forget {
  flex: none;
  opacity: 0;
  transition: opacity var(--omp-dur-fast) ease;
}

/* 只在本行悬停时显形：十二行常驻的 × 会把「切目录」读成「删记录」。 */
.recent-row:hover .recent-forget,
.recent-forget:focus-visible {
  opacity: 1;
}
</style>
