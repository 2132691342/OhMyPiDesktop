<script setup lang="ts">
// 概览页：一屏回答「两个引擎各自什么状态、最近花了多少、有什么可继续的」。
//
// 它是首屏而不是对话页：进应用先看一眼全局，比直接掉进上次的会话更符合
// 「我要开始干活」的心理——而继续干活的那条路（最近会话）就摆在同一屏上。
//
// 数字全部来自引擎自己的落盘数据（会话文件、配置清单、进程事实），
// 不另存一份账本。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import * as echarts from 'echarts/core'
import { LineChart, BarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import AppIcon from '@/components/common/AppIcon.vue'
import { api } from '@/api/http'
import { useAppStore } from '@/stores/app'
import { useToasts } from '@/composables/useToasts'
import type { EngineHealth, HealthData, SessionRow } from '@/types/api'

echarts.use([LineChart, BarChart, GridComponent, TooltipComponent, CanvasRenderer])

const { t } = useI18n()
const app = useAppStore()
const router = useRouter()
const toasts = useToasts()

const health = ref<HealthData | null>(null)
const loading = ref(false)
const span = ref<'today' | 'week' | 'month'>('week')
const rows = ref<SessionRow[]>([])
const trendEl = ref<HTMLElement | null>(null)
const modelEl = ref<HTMLElement | null>(null)
let trendChart: echarts.ECharts | null = null
let modelChart: echarts.ECharts | null = null

const engines = computed<EngineHealth[]>(() => health.value?.engines ?? [])
const totals = computed(() => health.value ?? null)

const windowDays = computed(() =>
  span.value === 'today' ? 1 : span.value === 'week' ? 7 : 30,
)

const scoped = computed(() => {
  const since = Date.now() - windowDays.value * 86_400_000
  return rows.value.filter((row) => {
    const time = new Date(row.updated_at).getTime()
    return Number.isNaN(time) ? true : time >= since
  })
})

const usage = computed(() => {
  let tokens = 0
  let cost = 0
  for (const row of scoped.value) {
    tokens += row.tokens ?? 0
    cost += row.cost ?? 0
  }
  return { tokens, cost, sessions: scoped.value.length }
})

const byDay = computed(() => {
  const days = span.value === 'today' ? 24 : windowDays.value
  const buckets: { label: string; cost: number }[] = []
  const now = new Date()
  if (span.value === 'today') {
    for (let i = 23; i >= 0; i--) {
      const hour = new Date(now)
      hour.setHours(now.getHours() - i, 0, 0, 0)
      buckets.push({ label: `${hour.getHours()}`, cost: 0 })
    }
  } else {
    for (let i = days - 1; i >= 0; i--) {
      const day = new Date(now)
      day.setDate(now.getDate() - i)
      buckets.push({ label: `${day.getMonth() + 1}/${day.getDate()}`, cost: 0 })
    }
  }
  for (const row of scoped.value) {
    const time = new Date(row.updated_at)
    if (Number.isNaN(time.getTime())) continue
    const index =
      span.value === 'today'
        ? buckets.findIndex((_, i) => {
            const hour = new Date(now)
            hour.setHours(now.getHours() - (buckets.length - 1 - i), 0, 0, 0)
            return hour.getHours() === time.getHours()
          })
        : buckets.length - 1 - Math.floor((now.getTime() - time.getTime()) / 86_400_000)
    if (index < 0 || index >= buckets.length) continue
    buckets[index].cost += row.cost ?? 0
  }
  return buckets
})

const byModel = computed(() => {
  const map = new Map<string, number>()
  for (const row of scoped.value) {
    const key = row.model || t('common.unknown')
    map.set(key, (map.get(key) ?? 0) + (row.tokens ?? 0))
  }
  return [...map.entries()]
    .map(([model, tokens]) => ({ model, tokens }))
    .sort((a, b) => b.tokens - a.tokens)
})

const recent = computed(() => rows.value.slice(0, 6))

async function load() {
  loading.value = true
  try {
    const [data, sessions] = await Promise.all([
      api.health(),
      api.sessions(app.workspace || undefined, app.active),
    ])
    health.value = data
    rows.value = sessions.sessions ?? []
    renderCharts()
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function renderCharts() {
  const axis = cssVar('--omp-faint')
  const line = cssVar('--omp-line')
  if (trendEl.value) {
    trendChart ??= echarts.init(trendEl.value)
    trendChart.setOption({
      animationDuration: 260,
      grid: { left: 46, right: 14, top: 16, bottom: 24 },
      tooltip: { trigger: 'axis' },
      xAxis: {
        type: 'category',
        data: byDay.value.map((d) => d.label),
        axisLine: { lineStyle: { color: line } },
        axisLabel: { color: axis, fontSize: 10 },
        axisTick: { show: false },
      },
      yAxis: {
        type: 'value',
        splitLine: { lineStyle: { color: line } },
        axisLabel: { color: axis, fontSize: 10 },
      },
      series: [
        {
          type: 'line',
          smooth: true,
          showSymbol: false,
          data: byDay.value.map((d) => Number(d.cost.toFixed(5))),
          lineStyle: { width: 2, color: cssVar('--omp-ch-1') },
          areaStyle: {
            color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
              { offset: 0, color: cssVar('--omp-brand-soft') },
              { offset: 1, color: 'transparent' },
            ]),
          },
        },
      ],
    })
  }
  if (modelEl.value) {
    modelChart ??= echarts.init(modelEl.value)
    const top = byModel.value.slice(0, 5)
    modelChart.setOption({
      animationDuration: 260,
      grid: { left: 8, right: 20, top: 8, bottom: 8, containLabel: true },
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
      xAxis: { type: 'value', splitLine: { lineStyle: { color: line } }, axisLabel: { color: axis, fontSize: 10 } },
      yAxis: {
        type: 'category',
        inverse: true,
        data: top.map((m) => m.model),
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: axis, fontSize: 10, width: 130, overflow: 'truncate' },
      },
      series: [
        {
          type: 'bar',
          barWidth: 11,
          data: top.map((m) => m.tokens),
          itemStyle: { color: cssVar('--omp-ch-2'), borderRadius: [0, 4, 4, 0] },
        },
      ],
    })
  }
}

function formatCost(v: number): string {
  if (!v) return '$0'
  return v < 0.01 ? `$${v.toFixed(5)}` : `$${v.toFixed(3)}`
}

function formatTokens(n: number): string {
  if (!n) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(n)
}

function uptimeOf(engine: EngineHealth): string {
  if (!engine.started_at) return ''
  const ms = Date.now() - engine.started_at
  const min = Math.floor(ms / 60000)
  if (min < 1) return t('overview.justNow')
  if (min < 60) return `${min}${t('overview.minutes')}`
  const hours = Math.floor(min / 60)
  if (hours < 24) return `${hours}${t('overview.hours')}`
  return `${Math.floor(hours / 24)}${t('overview.days')}`
}

/**
 * 探测还没完成时，不能显示「未安装」——那是一个尚未成立的事实。
 * 显示「检测中」，用户就知道这一格稍后会变。
 */
const detecting = computed(() => health.value?.detecting ?? false)

/** 状态标签：运行中 / 空闲 / 检测中 / 未安装。 */
function statusLabel(engine: EngineHealth): string {
  if (engine.running) return t('overview.running')
  if (engine.installed) return t('overview.idle')
  if (detecting.value) return t('overview.detecting')
  return t('overview.notInstalled')
}

function statusTagClass(engine: EngineHealth): string {
  if (engine.running) return 'ok'
  if (engine.installed) return ''
  if (detecting.value) return ''
  return 'warn'
}

function statusLedClass(engine: EngineHealth): string {
  if (engine.running) return 'omp-led--ok'
  if (engine.installed) return ''
  if (detecting.value) return ''
  return 'omp-led--warn'
}

/** 版本号那一行：装了就显示版本，没装且还在探就显示「检测中」。 */
function versionLabel(engine: EngineHealth): string {
  if (engine.version) return engine.version
  if (detecting.value) return t('overview.detecting')
  return t('overview.notInstalled')
}

async function openSession(path: string) {
  try {
    await router.push({ name: 'chat', query: { session: path } })
  } catch (err) {
    toasts.push('error', err instanceof Error ? err.message : String(err))
  }
}

function onResize() {
  trendChart?.resize()
  modelChart?.resize()
}

watch([span, byDay], () => renderCharts())

// 探测是后台跑的：首屏拿到的可能还是「不知道装没装」。
// 探测完成时后端会推 engine:detected，全局 store 据此刷新引擎清单，
// 于是 app.detecting 从 true 变 false——这一刻把这一屏的 health 重新拉一次。
//
// 盯的是 store 那个标志而不是本页的 health.detecting：
// 后者只在本页 load() 时才变，而 load() 只由这个 watch 触发——
// 盯自己就成了等自己。
watch(
  () => app.detecting,
  (value, prev) => {
    if (prev && !value) void load()
  },
)

onMounted(() => {
  void load()
  window.addEventListener('resize', onResize)
})

onUnmounted(() => {
  window.removeEventListener('resize', onResize)
  trendChart?.dispose()
  modelChart?.dispose()
  trendChart = null
  modelChart = null
})
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">{{ t('overview.title') }}</h1>
        <p class="page-sub">{{ t('overview.subtitle') }}</p>
      </div>
      <div class="head-actions">
        <div class="seg">
          <button
            v-for="key in (['today', 'week', 'month'] as const)"
            :key="key"
            class="seg-btn"
            :class="{ active: span === key }"
            @click="span = key"
          >
            {{ t(`overview.${key}`) }}
          </button>
        </div>
        <button class="omp-btn icon ghost" :class="{ 'is-loading': loading }" :title="t('common.refresh')" @click="load">
          <AppIcon name="refresh" :size="15" />
        </button>
      </div>
    </header>

    <div class="page-body">
      <!-- 引擎健康卡：一屏看两个引擎 -->
      <section class="engine-grid">
        <article
          v-for="engine in engines"
          :key="engine.engine_kind"
          class="omp-card card-pad engine-card"
          :class="{ 'is-missing': !engine.installed && !detecting }"
        >
          <div class="engine-head">
            <span class="omp-tile omp-tile--sm" :class="engine.installed ? '' : 'omp-tile--neutral'">
              <AppIcon name="box" :size="16" />
            </span>
            <div class="engine-id">
              <span class="engine-name">{{ engine.engine_label }}</span>
              <span class="engine-ver">{{ versionLabel(engine) }}</span>
            </div>
            <span class="tag" :class="statusTagClass(engine)">
              <span class="omp-led" :class="[statusLedClass(engine)]" />
              {{ statusLabel(engine) }}
            </span>
          </div>

          <!-- 还在探测：不知道装没装，就不能显示「未安装」并给出安装命令。
               那会让用户去装一个其实已经装好的引擎。 -->
          <div v-if="detecting && !engine.installed" class="engine-missing">
            <p class="engine-hint">
              <span class="dot busy" />
              {{ t('overview.detecting') }}
            </p>
          </div>

          <div v-else-if="!engine.installed" class="engine-missing">
            <p class="engine-hint">{{ t('overview.installHint') }}</p>
            <code v-if="engine.install_hint" class="engine-cmd selectable">{{ engine.install_hint }}</code>
            <button class="omp-btn sm primary" @click="app.openApps()">{{ t('overview.goInstall') }}</button>
          </div>

          <template v-else>
            <div class="engine-stats">
              <div class="stat">
                <span class="stat-num">{{ engine.session_count }}</span>
                <span class="stat-label">{{ t('nav.sessions') }}</span>
              </div>
              <div class="stat">
                <span class="stat-num">{{ engine.skill_count }}</span>
                <span class="stat-label">{{ t('nav.skills') }}</span>
              </div>
              <div class="stat">
                <span class="stat-num">{{ engine.plugin_count }}</span>
                <span class="stat-label">{{ t('nav.plugins') }}</span>
              </div>
              <div class="stat">
                <span class="stat-num">{{ engine.mcp_count }}</span>
                <span class="stat-label">{{ t('nav.mcp') }}</span>
              </div>
            </div>

            <dl class="engine-meta">
              <div class="omp-kv">
                <dt>{{ t('overview.channel') }}</dt>
                <dd>{{ engine.channel_label }}</dd>
              </div>
              <div class="omp-kv">
                <dt>{{ t('overview.process') }}</dt>
                <dd>
                  <template v-if="engine.running">
                    pid {{ engine.pid }}<template v-if="engine.protocol"> · v{{ engine.protocol }}</template>
                    <template v-if="uptimeOf(engine)"> · {{ uptimeOf(engine) }}</template>
                  </template>
                  <template v-else>{{ t('overview.idle') }}</template>
                </dd>
              </div>
              <div class="omp-kv">
                <dt>{{ t('overview.agentDir') }}</dt>
                <dd class="omp-mono dir">{{ engine.agent_dir }}</dd>
              </div>
            </dl>

            <p v-if="engine.last_error" class="engine-error">{{ engine.last_error }}</p>
          </template>
        </article>
      </section>

      <!-- 六个读数：三个是「花了多少」，三个是「装了什么」。
           最后三个分开算：合成一个「技能+插件+MCP」的合计在界面上无法加——
           三者不是同一种东西，加起来的那个数字也没有任何行动含义。 -->
      <section class="stat-row">
        <div class="omp-card card-pad">
          <span class="omp-metric-label">{{ t('overview.spend') }}</span>
          <span class="omp-metric">{{ formatCost(usage.cost) }}</span>
        </div>
        <div class="omp-card card-pad">
          <span class="omp-metric-label">{{ t('overview.tokens') }}</span>
          <span class="omp-metric">{{ formatTokens(usage.tokens) }}</span>
        </div>
        <div class="omp-card card-pad">
          <span class="omp-metric-label">{{ t('overview.turns') }}</span>
          <span class="omp-metric">{{ usage.sessions }}</span>
        </div>
        <div class="omp-card card-pad">
          <span class="omp-metric-label">{{ t('overview.capSkills') }}</span>
          <span class="omp-metric">{{ totals?.total_skills ?? 0 }}</span>
        </div>
        <div class="omp-card card-pad">
          <span class="omp-metric-label">{{ t('overview.capPlugins') }}</span>
          <span class="omp-metric">{{ totals?.total_plugins ?? 0 }}</span>
        </div>
        <div class="omp-card card-pad">
          <span class="omp-metric-label">{{ t('overview.capMcp') }}</span>
          <span class="omp-metric">{{ totals?.total_mcp ?? 0 }}</span>
        </div>
      </section>

      <div class="chart-row">
        <section class="omp-card card-pad chart-card">
          <div class="omp-sec-head">
            <span class="omp-sec-title">{{ t('overview.byDay') }}</span>
          </div>
          <div ref="trendEl" class="chart" />
        </section>
        <section class="omp-card card-pad chart-card">
          <div class="omp-sec-head">
            <span class="omp-sec-title">{{ t('overview.byModel') }}</span>
          </div>
          <div v-if="byModel.length" ref="modelEl" class="chart" />
          <div v-else class="omp-empty">
            <span class="omp-tile omp-tile--sm omp-tile--neutral"><AppIcon name="chart" :size="16" /></span>
            <p class="omp-empty-title">{{ t('overview.noUsage') }}</p>
          </div>
        </section>
      </div>

      <!-- 最近会话 -->
      <section class="omp-card card-pad">
        <div class="omp-sec-head">
          <span class="omp-sec-title">{{ t('overview.recent') }}</span>
          <button class="omp-btn text sm" @click="router.push({ name: 'sessions' })">
            {{ t('overview.all') }}
            <AppIcon name="chevron" :size="12" />
          </button>
        </div>
        <div v-if="recent.length" class="recent-list">
          <button
            v-for="row in recent"
            :key="row.id"
            class="recent-row omp-row-click"
            @click="openSession(row.path)"
          >
            <span class="omp-tile omp-tile--sm omp-tile--neutral"><AppIcon name="chat" :size="14" /></span>
            <span class="recent-title">{{ row.title || t('common.untitled') }}</span>
            <span class="recent-meta omp-mono">{{ row.model || '—' }}</span>
            <span class="recent-time omp-mono">{{ row.updated_at?.slice(5, 16) }}</span>
          </button>
        </div>
        <div v-else class="omp-empty">
          <span class="omp-tile omp-tile--sm omp-tile--neutral"><AppIcon name="chat" :size="16" /></span>
          <p class="omp-empty-title">{{ t('overview.noSessions') }}</p>
          <p class="omp-empty-hint">{{ t('overview.noSessionsHint') }}</p>
          <button class="omp-btn sm primary" @click="router.push({ name: 'chat' })">
            {{ t('overview.startChat') }}
          </button>
        </div>
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
  max-width: var(--omp-w-desc);
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
  padding: var(--omp-sp-5) var(--omp-sp-6) var(--omp-sp-12);
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-4);
}

.engine-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: var(--omp-sp-3);
}

.engine-card {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-3);
}

.engine-card.is-missing {
  border-style: dashed;
}

.engine-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
}

.engine-id {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.engine-name {
  font-family: var(--omp-font-display);
  font-size: var(--omp-fs-base);
  font-weight: 650;
  color: var(--omp-ink);
}

.engine-ver {
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-mute);
}

.engine-head .tag {
  margin-left: auto;
}

.engine-stats {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--omp-sp-2);
}

.stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: var(--omp-sp-2);
  border-radius: var(--omp-r-sm);
  background: var(--omp-hover);
}

.stat-num {
  font-family: var(--omp-font-display);
  font-size: var(--omp-fs-lg);
  font-weight: 650;
  color: var(--omp-ink);
  font-variant-numeric: tabular-nums;
}

.stat-label {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-mute);
}

.engine-meta {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-1);
  margin: 0;
}

.dir {
  max-width: 240px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.engine-missing {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--omp-sp-2);
}

.engine-hint {
  margin: 0;
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
}

.engine-cmd {
  width: 100%;
  padding: var(--omp-sp-2);
  border-radius: var(--omp-r-sm);
  background: var(--omp-code-bg);
  border: 1px solid var(--omp-line);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  color: var(--omp-ink-5);
  overflow-wrap: anywhere;
}

.engine-error {
  margin: 0;
  padding: var(--omp-sp-2);
  border-radius: var(--omp-r-sm);
  background: var(--omp-bad-soft);
  color: var(--omp-bad);
  font-size: var(--omp-fs-2xs);
  overflow-wrap: anywhere;
}

.stat-row {
  display: grid;
  /* 六个读数排成 3 × 2：用固定三列而不是 auto-fit，
     否则宽屏会排成一行六个（每个只剩 130px，数字换了行），
     窄屏会变成 5 + 1 的余数行。 */
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--omp-sp-3);
}

/* 窄窗口（800×600 的默认尺寸）下三列会把数字挤成两行：降成两列 */
@container app (max-width: 900px) {
  .stat-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.stat-row > .omp-card {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.chart-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: var(--omp-sp-4);
}

.chart-card {
  min-width: 0;
}

.chart {
  width: 100%;
  height: 200px;
}

.recent-list {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.recent-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: var(--omp-sp-2);
  border: none;
  background: transparent;
  text-align: left;
  border-radius: var(--omp-r-sm);
}

.recent-title {
  flex: 1;
  min-width: 0;
  font-size: var(--omp-fs-sm);
  color: var(--omp-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recent-meta,
.recent-time {
  flex: none;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

@media (max-width: 1100px) {
  .chart-row {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
