// 应用状态：引导、设置、主题、引擎、工作目录、SSE 连接。
// SSE 的连接与事件分流放在这里，因为它是全局的（引擎状态、安装进度都在同一通道）。
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api, streamURL } from '@/api/http'
import { getEndpoint, onStartupError, pickFolder } from '@/api/wails'
import type {
  AskRequest,
  Bootstrap,
  ChatBlock,
  ChatPatch,
  EngineCaps,
  EngineKind,
  EngineStatus,
  SessionRow,
  SlashCommand,
  WorkspaceRow,
} from '@/types/api'
import { useChatStore } from './chat'
import { useAgentsStore } from './agents'
import { useToasts } from '@/composables/useToasts'

export const useAppStore = defineStore('app', () => {
  const ready = ref(false)
  const fatal = ref('')
  const version = ref('')
  const workspace = ref('')
  const profile = ref('')
  const settings = ref<Record<string, string>>({})
  const engines = ref<EngineStatus | null>(null)
  const active = ref<EngineKind>('omp')
  const workspaces = ref<WorkspaceRow[]>([])
  const sessions = ref<SessionRow[]>([])
  const detecting = ref(false)

  // 引擎请求浏览器完成的一次授权。它不弹窗、不自动打开，只在对话页顶部
  // 挂一条可复制的地址——默认浏览器可能打不开，用户需要能看到原文。
  const pendingLink = ref<{ url: string; label: string; instructions: string } | null>(null)

  const boot = ref<Bootstrap | null>(null)
  let source: EventSource | null = null
  let lastSeq = 0
  let reconnectTimer: number | undefined

  const toasts = useToasts()
  const chat = useChatStore()
  const agents = useAgentsStore()

  const theme = computed(() => settings.value['ui.theme'] || 'light')
  /** 探测到的引擎清单（按固定顺序 omp → pi）。 */
  const installed = computed(() => engines.value?.engines.filter((e) => e.installed) ?? [])
  /** 全都没有引擎时，输入区要禁用并给安装命令。 */
  const hasEngine = computed(() => installed.value.length > 0)
  /** 只有一个引擎时不显示选择器（不占位置）。 */
  const showEnginePicker = computed(() => installed.value.length > 1)
  /** 某引擎的能力表；没探测到就按「什么都不能」处理。 */
  function capsOf(kind: EngineKind): EngineCaps | null {
    const found = engines.value?.engines.find((e) => e.kind === kind)
    return found?.caps ?? boot.value?.caps?.[kind] ?? null
  }
  /** 安装命令原文（缺引擎时给用户复制）。 */
  function installHint(kind: EngineKind): string {
    return engines.value?.install?.[kind] ?? boot.value?.install?.[kind] ?? ''
  }

  function applyTheme(next: string) {
    document.documentElement.dataset.theme = next === 'dark' ? 'dark' : 'light'
  }

  function applyMotion(reduced: boolean) {
    document.documentElement.dataset.motion = reduced ? 'reduced' : 'full'
  }

  async function load() {
    try {
      await getEndpoint()
      const data = await api.bootstrap()
      boot.value = data
      version.value = data.version
      workspace.value = data.workspace
      profile.value = data.profile
      settings.value = data.settings ?? {}
      workspaces.value = data.workspaces ?? []
      active.value = data.active ?? 'omp'
      chat.kind = data.active ?? 'omp'
      chat.caps = data.caps?.[data.active] ?? null
      applyTheme(theme.value)
      applyMotion(settings.value['ui.reduce_motion'] === '1')
      applyFontScale(Number(settings.value['ui.font_scale'] || 1))
      ready.value = true
      connect()
      await Promise.all([refreshSessions(), refreshEngines()])
    } catch (err) {
      fatal.value = err instanceof Error ? err.message : String(err)
    }
  }

  function applyFontScale(scale: number) {
    const safe = Number.isFinite(scale) ? Math.min(1.35, Math.max(0.8, scale)) : 1
    document.documentElement.style.setProperty('--omp-fs-scale', String(safe))
  }

  // 会话列表跟当前引擎走：两个引擎的会话文件不是能互换接着聊的东西，
  // 混在一张表里会让人以为点开就能继续。
  async function refreshSessions() {
    const data = await api.sessions(workspace.value || undefined, chat.kind)
    sessions.value = data.sessions ?? []
  }

  async function refreshEngines() {
    engines.value = await api.engineStatus()
  }

  // 重新探测：设置页的「重新检测」。pi 的探测要起一次 Node，所以要给 loading。
  async function detectEngines() {
    detecting.value = true
    try {
      engines.value = await api.detectEngines()
      return engines.value
    } finally {
      detecting.value = false
    }
  }

  // 切当前引擎：写设置 + 换对话通道。切引擎不切目录、不切会话。
  async function setEngine(kind: EngineKind) {
    if (kind === active.value) return
    await api.setEngine(kind)
    active.value = kind
    await chat.switchEngine(kind)
  }

  async function saveSettings(kv: Record<string, string>) {
    settings.value = { ...settings.value, ...kv }
    if (kv['ui.theme']) applyTheme(kv['ui.theme'])
    if (kv['ui.reduce_motion'] !== undefined) applyMotion(kv['ui.reduce_motion'] === '1')
    if (kv['ui.font_scale'] !== undefined) applyFontScale(Number(kv['ui.font_scale']))
    settings.value = await api.saveSettings(kv)
  }

  async function chooseWorkspace() {
    const picked = await pickFolder(workspace.value)
    if (!picked) return
    await setWorkspace(picked)
  }

  async function setWorkspace(dir: string) {
    const res = await api.setWorkspace(dir)
    workspace.value = res.workspace
    const next = await api.bootstrap()
    workspaces.value = next.workspaces ?? []
    boot.value = next
    await refreshSessions()
    chat.reset()
    await chat.refresh('')
  }

  function connect() {
    if (source) source.close()
    const url = streamURL(lastSeq)
    source = new EventSource(url)
    source.onmessage = (evt) => {
      const frame = readFrame(evt.data)
      if (!frame) return
      if (frame.seq) {
        if (lastSeq && frame.seq > lastSeq + 1) {
          // 断流窗口：seq 跳号说明中间丢了帧，拉一次快照对账
          void reconcile()
        }
        lastSeq = frame.seq
      }
      route(frame.event, frame.data)
    }
    source.onerror = () => {
      source?.close()
      source = null
      window.clearTimeout(reconnectTimer)
      reconnectTimer = window.setTimeout(connect, 1200)
    }
  }

  async function reconcile() {
    try {
      if (chat.sessionFile || chat.workDir) await chat.refresh(chat.sessionFile)
    } catch {
      /* 引擎没起来时对账会失败，正常 */
    }
  }

  function route(event: string, data: Record<string, unknown>) {
    switch (event) {
      case 'stream:open':
        return
      case 'engine:ready':
      case 'engine:exit':
        void refreshEngines()
        return
      case 'engine:commands': {
        const list = readCommands(data.commands)
        if (list.length) chat.commands = list
        return
      }
      case 'engine:extension_error':
        return // 扩展自己的报错不该弹给用户
      case 'chat:start':
        chat.streaming = true
        chat.promptError = ''
        return
      case 'chat:turn': {
        const runID = asString(data.run_id)
        const blocks = readBlocks(data.blocks)
        if (!chat.turn || chat.turn.run_id !== runID) {
          chat.turn = { run_id: runID, user_message: asString(data.user_message), blocks }
        } else if (!chat.turn.user_message) {
          chat.turn.user_message = asString(data.user_message)
        }
        return
      }
      case 'chat:patch': {
        const patch = readPatch(data)
        if (patch) chat.applyPatch(patch)
        return
      }
      case 'chat:blocks': {
        if (chat.turn && asString(data.run_id) === chat.turn.run_id) {
          chat.turn.blocks = readBlocks(data.blocks)
        }
        return
      }
      case 'chat:queue':
        chat.queue = { steering: readStrings(data.steering), followUp: readStrings(data.followUp) }
        return
      case 'chat:ask': {
        const ask = readAsk(data)
        if (ask) chat.asks = [...chat.asks.filter((a) => a.id !== ask.id), ask]
        return
      }
      case 'chat:notify': {
        const level = asString(data.level, 'info')
        chat.pushNotice(level, asString(data.message))
        return
      }
      case 'chat:status':
        chat.setStatus(asString(data.key), asString(data.text))
        return
      case 'chat:widget':
        chat.setWidget(asString(data.key), readStrings(data.lines))
        return
      case 'chat:retry':
        chat.retry = { attempt: asNumber(data.attempt), max: asNumber(data.max), delay_ms: asNumber(data.delay_ms) }
        return
      case 'chat:retry_end':
        chat.retry = null
        return
      case 'chat:compaction':
        chat.compacting = data.state === 'start'
        return
      case 'chat:turn_end':
        if (data.usage) void chat.refresh(chat.sessionFile)
        return
      case 'chat:done':
        chat.finishTurn()
        void chat.refresh(chat.sessionFile)
        void refreshSessions()
        return
      case 'chat:settled':
        chat.streaming = false
        return
      case 'chat:prompt_result': {
        const status = asString(data.status)
        if (status && status !== 'completed') {
          chat.promptError = asString(data.message, '这一轮没有完成')
        }
        return
      }
      case 'chat:error':
        chat.promptError = asString(data.message, '出错了')
        chat.streaming = false
        toasts.push('error', chat.promptError)
        return
      case 'chat:open_url': {
        // 优先用 launchUrl（短回环重定向），它比原始长地址更可能在默认浏览器里打开。
        const url = asString(data.launch_url) || asString(data.url)
        if (!url) return
        pendingLink.value = {
          url,
          label: asString(data.url),
          instructions: asString(data.instructions),
        }
        return
      }
      case 'chat:editor_text': {
        const text = asString(data.text)
        if (text) chat.draftOverride = text
        return
      }
      case 'chat:notice': {
        const level = asString(data.level, 'info')
        toasts.push(level === 'error' ? 'error' : level === 'warning' ? 'warn' : 'info', asString(data.message))
        return
      }
      case 'chat:command_output':
        chat.pushNotice('info', asString(data.text))
        return
      case 'chat:gap':
        void reconcile()
        return
      case 'subagent:lifecycle':
        agents.applyLifecycle(data)
        return
      case 'subagent:progress':
        agents.applyProgress(data)
        return
      case 'subagent:event':
        return // 只有 events 档才有；面板不订阅这一档
      case 'agents:cleared':
        // 会话切换后引擎已丢弃旧 id 的帧，本地也必须清干净，
        // 否则界面会一直显示已经不存在的子代理。
        agents.reset()
        return
      case 'session:info':
      case 'session:title':
        void refreshSessions()
        return
      default:
        return
    }
  }

  // 跳到设置页（缺引擎时「去设置」这类动作的目标）。
  function openSettings() {
    window.location.hash = '#/settings'
  }

  function disconnect() {
    source?.close()
    source = null
  }

  function watchStartupFatal() {
    onStartupError((message) => {
      fatal.value = message
    })
  }

  return {
    boot,
    ready,
    fatal,
    version,
    workspace,
    profile,
    settings,
    engines,
    active,
    detecting,
    workspaces,
    sessions,
    pendingLink,
    theme,
    installed,
    hasEngine,
    showEnginePicker,
    capsOf,
    installHint,
    load,
    refreshSessions,
    refreshEngines,
    detectEngines,
    setEngine,
    saveSettings,
    chooseWorkspace,
    setWorkspace,
    openSettings,
    disconnect,
    watchStartupFatal,
  }
})

// ---------------- SSE 载荷的类型边界 ----------------
// 事件体是 JSON 解析出来的 unknown 树：先校验再取字段，绝不 as 一个形状上去直接读。

const asString = (v: unknown, fallback = '') => (typeof v === 'string' ? v : fallback)
const asNumber = (v: unknown, fallback = 0) => (typeof v === 'number' && Number.isFinite(v) ? v : fallback)
const readStrings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [])

/** 命令清单只关心界面要显示的三段：名字 / 说明 / 输入提示。 */
function readCommands(v: unknown): SlashCommand[] {
  if (!Array.isArray(v)) return []
  const out: SlashCommand[] = []
  for (const item of v) {
    if (!item || typeof item !== 'object') continue
    const record = item as Record<string, unknown>
    const name = asString(record.name)
    if (!name) continue
    const input = record.input && typeof record.input === 'object' ? (record.input as Record<string, unknown>) : null
    out.push({
      name,
      source: asString(record.source),
      description: asString(record.description) || undefined,
      aliases: readStrings(record.aliases),
      input: input ? { hint: asString(input.hint) || undefined } : undefined,
    })
  }
  return out
}

function readBlock(v: unknown): ChatBlock | null {
  if (!v || typeof v !== 'object') return null
  const record = v as Record<string, unknown>
  const kind = asString(record.kind)
  if (kind !== 'text' && kind !== 'thinking' && kind !== 'tool') return null
  const status = asString(record.status)
  return {
    kind,
    text: asString(record.text),
    tool_call_id: asString(record.tool_call_id) || undefined,
    tool_name: asString(record.tool_name) || undefined,
    args: record.args,
    intent: asString(record.intent) || undefined,
    output: asString(record.output) || undefined,
    is_error: Boolean(record.is_error),
    status: status === 'pending' || status === 'running' || status === 'done' || status === 'error' ? status : undefined,
    streaming: Boolean(record.streaming),
    duration_ms: asNumber(record.duration_ms) || undefined,
  }
}

function readBlocks(v: unknown): ChatBlock[] {
  if (!Array.isArray(v)) return []
  const out: ChatBlock[] = []
  for (const item of v) {
    const block = readBlock(item)
    if (block) out.push(block)
  }
  return out
}

function readPatch(data: Record<string, unknown>): ChatPatch | null {
  const mode = asString(data.mode)
  if (mode !== 'delta' && mode !== 'block' && mode !== 'blocks') return null
  const kind = asString(data.kind)
  return {
    run_id: asString(data.run_id),
    mode,
    index: asNumber(data.index),
    kind: kind === 'text' || kind === 'thinking' || kind === 'tool' ? kind : 'text',
    delta: asString(data.delta) || undefined,
    block: readBlock(data.block) ?? undefined,
    blocks: mode === 'blocks' ? readBlocks(data.blocks) : undefined,
  }
}

function readAsk(v: unknown): AskRequest | null {
  if (!v || typeof v !== 'object') return null
  const record = v as Record<string, unknown>
  const id = asString(record.id)
  const method = asString(record.method)
  if (!id || !method) return null
  const questions = Array.isArray(record.questions) ? record.questions : []
  return {
    id,
    method: method as AskRequest['method'],
    title: asString(record.title) || undefined,
    message: asString(record.message) || undefined,
    placeholder: asString(record.placeholder) || undefined,
    prefill: asString(record.prefill) || undefined,
    options: readStrings(record.options),
    timeout: asNumber(record.timeout) || undefined,
    targetId: asString(record.targetId) || undefined,
    questions: questions.flatMap((q) => {
      if (!q || typeof q !== 'object') return []
      const item = q as Record<string, unknown>
      const options = Array.isArray(item.options) ? item.options : []
      return [
        {
          id: asString(item.id),
          question: asString(item.question),
          header: asString(item.header) || undefined,
          multi: Boolean(item.multi),
          recommended: typeof item.recommended === 'number' ? item.recommended : undefined,
          options: options.flatMap((o) => {
            if (!o || typeof o !== 'object') return []
            const opt = o as Record<string, unknown>
            return [
              {
                label: asString(opt.label),
                description: asString(opt.description) || undefined,
                preview: asString(opt.preview) || undefined,
              },
            ]
          }),
        },
      ]
    }),
  }
}

/** 读一帧 SSE：信封本身也可能是坏的，坏帧直接丢，不断流。 */
function readFrame(raw: string): { seq: number; event: string; data: Record<string, unknown> } | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== 'object') return null
  const record = parsed as Record<string, unknown>
  const data = record.data && typeof record.data === 'object' ? (record.data as Record<string, unknown>) : {}
  return { seq: asNumber(record.seq), event: asString(record.event), data }
}
