// 对话状态：消息流、流式增量、提问卡片、队列、用量。
//
// 性能纪律：流式正文只追加到「当前轮的 blocks 数组」上，不重建整个消息列表。
// 一轮长回答如果每次都整表替换，重渲染成本是 O(字数²)。
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '@/api/http'
import type {
  AskRequest,
  EngineCaps,
  EngineKind,
  HistoryTurn,
  ChatPatch,
  ChatState,
  ContextUsage,
  ModelInfo,
  ModelRow,
  SessionStats,
  SlashCommand,
  Turn,
  UIResponse,
} from '@/types/api'

export const useChatStore = defineStore('chat', () => {
  const sessionFile = ref('')
  const workDir = ref('')
  const engineRunning = ref(false)
  const streaming = ref(false)
  const model = ref<ModelInfo | null>(null)
  const thinkingLevel = ref('')
  const levels = ref<string[]>([])
  // 可选模型清单：引擎的 get_available_models。切换走 RPC，不是本地开关。
  const models = ref<ModelRow[]>([])
  // 切换进行中的动作名（'model' / 'thinking' / 'new' / 'compact'），
  // 按钮靠它进 loading 并锁尺寸，避免连点造成两次切换打架。
  const switching = ref('')
  const context = ref<ContextUsage | null>(null)
  const tokensPerSecond = ref<number | null>(null)
  const autoCompaction = ref(true)
  const stats = ref<SessionStats | null>(null)
  const commands = ref<SlashCommand[]>([])
  const profile = ref('')
  // 这一轮/这个会话属于哪个引擎。切换引擎时它跟着换，输入区据此显示。
  const kind = ref<EngineKind>('omp')
  // 能力表：界面按它决定显示哪些入口（不渲染灰按钮）。
  const caps = ref<EngineCaps | null>(null)

  // 当前轮：一次「提问 → 回答」
  const turn = ref<Turn | null>(null)
  // 历史轮（由后端投影好的轮次，界面直接渲染）
  const history = ref<HistoryTurn[]>([])

  const asks = ref<AskRequest[]>([])
  const queue = ref<{ steering: string[]; followUp: string[] }>({ steering: [], followUp: [] })
  const statusText = ref('')
  const widgets = ref<{ key: string; lines: string[] }[]>([])
  const notices = ref<{ id: number; level: string; message: string }[]>([])
  const retry = ref<{ attempt: number; max: number; delay_ms: number } | null>(null)
  const compacting = ref(false)
  const promptError = ref('')
  // 引擎通过 set_editor_text 要求回填输入框。它是一次性的，消费后置空。
  const draftOverride = ref('')

  let noticeSeq = 0

  const activeAsk = computed(() => asks.value[0] ?? null)
  const busy = computed(() => streaming.value || compacting.value)
  const contextPercent = computed(() => {
    if (!context.value || !context.value.contextWindow) return 0
    // pi 的 tokens 可能为 null（还没估算过），此时按 percent 兜底。
    if (typeof context.value.tokens === 'number') return context.value.tokens / context.value.contextWindow
    if (typeof context.value.percent === 'number') return context.value.percent / 100
    return 0
  })

  function applyState(state: ChatState) {
    sessionFile.value = state.session_file ?? ''
    workDir.value = state.work_dir ?? ''
    engineRunning.value = state.engine_running
    streaming.value = state.streaming
    model.value = state.model ?? null
    thinkingLevel.value = state.thinking_level ?? ''
    levels.value = state.levels?.length ? state.levels : levels.value
    context.value = state.context ?? null
    tokensPerSecond.value = state.tokens_per_second ?? null
    autoCompaction.value = state.auto_compaction
    stats.value = state.stats ?? null
    commands.value = state.commands ?? []
    profile.value = state.profile ?? ''
    if (state.kind) kind.value = state.kind
    if (state.caps) caps.value = state.caps
    if (state.queued) queue.value = { steering: state.queued.steering ?? [], followUp: state.queued.followUp ?? [] }
    history.value = state.history ?? []
  }

  /** 增量应用：delta 只追加尾部，block 只替换那一块，blocks 才整表换。 */
  function applyPatch(patch: ChatPatch) {
    if (!turn.value || patch.run_id !== turn.value.run_id) return
    const blocks = turn.value.blocks
    if (patch.mode === 'blocks' && patch.blocks) {
      turn.value.blocks = patch.blocks
      return
    }
    if (patch.mode === 'delta') {
      const block = blocks[patch.index]
      if (block) {
        block.text = (block.text ?? '') + (patch.delta ?? '')
      }
      return
    }
    if (patch.mode === 'block' && patch.block) {
      blocks[patch.index] = patch.block
    }
  }

  async function refresh(session = sessionFile.value) {
    const state = await api.chatState(kind.value, session || undefined)
    applyState(state)
    return state
  }

  // 拉可选模型清单。失败不抛：模型清单拉不到时输入区仍应可用，
  // 只是下拉里只剩「当前模型」一项。
  async function loadModels() {
    try {
      const data = await api.models(kind.value)
      models.value = data.models ?? []
    } catch {
      models.value = []
    }
  }

  // 换模型。失败要把显示值拨回去——否则界面上写着一个引擎并不认识的模型。
  async function setModel(provider: string, id: string) {
    const prev = model.value
    switching.value = 'model'
    // 先动显示值，让点击立刻有反馈；失败再回滚。
    model.value = { ...(prev ?? ({} as ModelInfo)), provider, id }
    try {
      await api.setModel(kind.value, sessionFile.value, provider, id)
    } catch (err) {
      model.value = prev
      throw err
    } finally {
      switching.value = ''
    }
  }

  // 换思考档。同上：先动显示值，失败回滚。
  async function setThinking(level: string) {
    const prev = thinkingLevel.value
    if (level === prev) return
    switching.value = 'thinking'
    thinkingLevel.value = level
    try {
      await api.setThinking(kind.value, sessionFile.value, level)
    } catch (err) {
      thinkingLevel.value = prev
      throw err
    } finally {
      switching.value = ''
    }
  }

  async function setAutoCompaction(enabled: boolean) {
    const prev = autoCompaction.value
    switching.value = 'compaction'
    autoCompaction.value = enabled
    try {
      await api.setAutoCompaction(kind.value, sessionFile.value, enabled)
    } catch (err) {
      autoCompaction.value = prev
      throw err
    } finally {
      switching.value = ''
    }
  }

  // 开新会话。必须走 /chat/new：只清本地 state 的话，下一句仍会追加进
  // 同一个会话文件，用户会以为「新开了一个」而实际还接着上面写。
  async function newChat() {
    switching.value = 'new'
    try {
      const res = await api.newChat(kind.value)
      reset()
      sessionFile.value = res.session_file ?? ''
      await refresh(sessionFile.value)
      return res.session_file
    } finally {
      switching.value = ''
    }
  }

  // 切引擎：不切目录、不切会话，只换「跟哪个进程说话」。
  // 当前会话属于另一个引擎，所以界面要开一轮新会话，并在消息流里留一句分隔提示。
  async function switchEngine(next: EngineKind) {
    if (next === kind.value) return
    switching.value = 'engine'
    try {
      kind.value = next
      reset()
      sessionFile.value = ''
      const state = await api.chatState(next, undefined)
      applyState(state)
    } finally {
      switching.value = ''
    }
  }

  async function clearQueue() {
    await api.clearQueue(kind.value, sessionFile.value)
    queue.value = { steering: [], followUp: [] }
  }

  async function loadTurnFromEngine() {
    // 快照里可能已经有一轮正在跑（切回来时），此时用引擎消息还原，交给下次 prompt 接手
    if (!streaming.value) turn.value = null
  }

  async function send(message: string) {
    promptError.value = ''
    const runID = await api.send(kind.value, sessionFile.value, message).then((r) => r.run_id)
    turn.value = { run_id: runID, user_message: message, blocks: [] }
    streaming.value = true
    return runID
  }

  async function stop() {
    await api.stop(kind.value, sessionFile.value)
    streaming.value = false
  }

  async function steer(message: string) {
    await api.steer(kind.value, sessionFile.value, message)
  }

  async function followUp(message: string) {
    await api.followUp(kind.value, sessionFile.value, message)
    queue.value.followUp = [...queue.value.followUp, message]
  }

  async function compact() {
    compacting.value = true
    switching.value = 'compact'
    try {
      await api.compact(kind.value, sessionFile.value)
    } finally {
      compacting.value = false
      switching.value = ''
    }
  }

  async function answer(response: UIResponse) {
    await api.answer(kind.value, sessionFile.value, { ...response, type: 'extension_ui_response' })
    asks.value = asks.value.filter((a) => a.id !== response.id)
  }

  function pushNotice(level: string, message: string) {
    notices.value = [...notices.value, { id: ++noticeSeq, level, message }].slice(-4)
  }

  function dismissNotice(id: number) {
    notices.value = notices.value.filter((n) => n.id !== id)
  }

  function setStatus(key: string, text: string) {
    if (key === '' || text === '') {
      statusText.value = ''
      return
    }
    statusText.value = text
  }

  function setWidget(key: string, lines: string[]) {
    const rest = widgets.value.filter((w) => w.key !== key)
    widgets.value = (lines ?? []).length ? [...rest, { key, lines }] : rest
  }

  function finishTurn() {
    streaming.value = false
    retry.value = null
    if (turn.value) {
      // 收尾后把这一轮并入历史，下一轮从干净的 blocks 开始。
      // 用户那句也要进历史，否则刷新对账前，历史里会缺「我刚刚问的」。
      const finished = turn.value
      history.value = [
        ...history.value,
        { role: 'user', blocks: [{ kind: 'text', text: finished.user_message }] },
        { role: 'assistant', blocks: finished.blocks, model: finished.model },
      ]
      turn.value = null
    }
  }

  function reset() {
    turn.value = null
    history.value = []
    asks.value = []
    queue.value = { steering: [], followUp: [] }
    statusText.value = ''
    widgets.value = []
    notices.value = []
    context.value = null
    stats.value = null
    promptError.value = ''
  }

  return {
    sessionFile,
    workDir,
    engineRunning,
    streaming,
    model,
    thinkingLevel,
    levels,
    models,
    switching,
    context,
    contextPercent,
    tokensPerSecond,
    autoCompaction,
    stats,
    commands,
    profile,
    kind,
    caps,
    turn,
    history,
    asks,
    activeAsk,
    queue,
    statusText,
    widgets,
    notices,
    retry,
    compacting,
    busy,
    promptError,
    draftOverride,
    applyState,
    applyPatch,
    refresh,
    loadModels,
    setModel,
    setThinking,
    setAutoCompaction,
    newChat,
    switchEngine,
    clearQueue,
    loadTurnFromEngine,
    send,
    stop,
    steer,
    followUp,
    compact,
    answer,
    pushNotice,
    dismissNotice,
    setStatus,
    setWidget,
    finishTurn,
    reset,
  }
})
