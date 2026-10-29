// 对话状态：消息流、流式增量、提问卡片、队列、用量。
//
// 性能纪律：流式正文只追加到「当前轮的 blocks 数组」上，不重建整个消息列表。
// 一轮长回答如果每次都整表替换，重渲染成本是 O(字数²)。
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '@/api/http'
import { dropAllCaches, dropCache } from '@/utils/md'
import type {
  AskRequest,
  ChatAttachment,
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
  const modelsLoading = ref(false)
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

  // 每个引擎一份的模型清单与思考档。
  //
  // 为什么要分桶：两个引擎的模型清单完全不同，而拉清单可能要起进程
  // （omp 冷启动实测 11 秒）。不分桶的话，切一次引擎就要重拉一次——
  // 于是「切引擎」这个高频动作每次都付一次冷启动。
  //
  // 分桶之后，第一次切到某个引擎要付一次，之后是瞬时的。
  const modelCache = ref<Record<EngineKind, ModelRow[]>>({ omp: [], pi: [] })
  const levelCache = ref<Record<EngineKind, string[]>>({ omp: [], pi: [] })

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
  // 切不到请求会话的原因（引擎正忙 / 扩展拦下）。它不会自动消失：
  // 用户需要知道“历史可以看，但下一句话会写到另一条会话里”。
  const bindErr = ref('')
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
    // 切不到请求的会话时，引擎的当前会话仍是另一条。这时把“实际会写到哪里”
    // 原话交给界面提示——静默把历史当当前会话看，下一句话就会落到错的文件里。
    bindErr.value = state.bind_err ?? ''
    streaming.value = state.streaming
    model.value = state.model ?? null
    thinkingLevel.value = state.thinking_level ?? ''
    if (state.kind) kind.value = state.kind
    // 思考档按引擎记账：切回上一个引擎时，它的档位立刻就位，
    // 不用等一次 RPC 回来。
    if (state.levels?.length) {
      levelCache.value = { ...levelCache.value, [kind.value]: state.levels }
    }
    levels.value = levelCache.value[kind.value] ?? []
    context.value = state.context ?? null
    tokensPerSecond.value = state.tokens_per_second ?? null
    autoCompaction.value = state.auto_compaction
    stats.value = state.stats ?? null
    commands.value = state.commands ?? []
    profile.value = state.profile ?? ''
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
    // 模型清单跟着「当前引擎 + 当前模型」走：引擎可能被换过（装完插件重启、
    // 改了 models 配置），而 get_available_models 的返回不会主动推过来。
    // 不在这里跟一次，下拉里就会一直停着上一次的清单。
    if (!models.value.length || !state.model) void loadModels()
    return state
  }

// 拉可选模型清单。失败不抛：模型清单拉不到时输入区仍应可用，
  // 只是下拉里只剩「当前模型」一项。
  //
  // 按引擎分桶缓存：切引擎时立刻换成那个引擎的清单，不用等。
  // 后端在引擎没跑时退到读配置文件，所以首次拉取也不再冷启动一个进程。
  async function loadModels(force = false) {
    const target = kind.value
    if (!force && modelCache.value[target].length) {
      models.value = modelCache.value[target]
      return models.value
    }
    if (modelsLoading.value) return models.value
    modelsLoading.value = true
    try {
      const data = await api.models(target)
      const rows = data.models ?? []
      modelCache.value = { ...modelCache.value, [target]: rows }
      // 请求返回时用户可能已经切走了：这时不能覆盖当前引擎的清单。
      if (kind.value === target) models.value = rows
    } catch {
      if (kind.value === target) models.value = []
    } finally {
      modelsLoading.value = false
    }
    return models.value
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

  // 切引擎：不切目录。当前会话属于另一个引擎，界面保留已看到的消息（可回看），
  // 并在新引擎上开一条新会话接着聊——直接清空会让人以为上下文丢了。
  //
  // 顺序很重要：先立刻把界面换成目标引擎的缓存值（模型 / 思考档），
  // 再去问后端要新引擎的状态。用户看到的切换是瞬时的，
  // 而不是「等一次 chat/state 往返」之后才变。
  async function switchEngine(next: EngineKind) {
    if (next === kind.value) return
    switching.value = 'engine'
    try {
      // 先把已看的消息并进历史，否则它们随 turn 一起被丢掉。
      if (turn.value) finishTurn()
      const kept = history.value
      kind.value = next
      // 立刻换到目标引擎的缓存：下拉里该出现哪个引擎的模型，一眼就该是它。
      models.value = modelCache.value[next] ?? []
      levels.value = levelCache.value[next] ?? []
      const state = await api.chatState(next, undefined)
      applyState(state)
      // 流里插一条分隔：告诉用户“以下由 xxx 接续，前面的内容不在新会话里”。
      // 它不是系统消息，只是界面层的标记，不会发进引擎。
      // applyState 会用新引擎的历史覆盖，所以拼在它之后。
      history.value = [
        ...kept,
        { role: 'engine-mark', blocks: [{ kind: 'text', text: next }] },
        ...history.value,
      ]
      // 后台补这个引擎的模型清单：切过去时可能是第一次用这个引擎。
      void loadModels()
    } finally {
      switching.value = ''
    }
  }

  async function clearQueue() {
    await api.clearQueue(kind.value, sessionFile.value)
    queue.value = { steering: [], followUp: [] }
  }

  // 移除单条排队消息（仅 omp）：清空是 pi 的能力，逐条移除是 omp 的能力。
  async function removeQueued(message: string, queueName: 'steering' | 'followUp') {
    const res = await api.removeQueued(kind.value, sessionFile.value, message, queueName)
    if (res.removed) {
      if (queueName === 'steering') queue.value.steering = queue.value.steering.filter((m) => m !== message)
      else queue.value.followUp = queue.value.followUp.filter((m) => m !== message)
    }
    return res.removed
  }

  async function loadTurnFromEngine() {
    // 快照里可能已经有一轮正在跑（切回来时），此时用引擎消息还原，交给下次 prompt 接手
    if (!streaming.value) turn.value = null
  }

  async function send(message: string, attachments: ChatAttachment[] = []) {
    promptError.value = ''
    const runID = await api
      .send(kind.value, sessionFile.value, message, attachments)
      .then((r) => r.run_id)
    turn.value = { run_id: runID, user_message: message, blocks: [] }
    streaming.value = true
    return runID
  }

  async function stop() {
    await api.stop(kind.value, sessionFile.value)
    streaming.value = false
  }

  async function steer(message: string, attachments: ChatAttachment[] = []) {
    await api.steer(kind.value, sessionFile.value, message, attachments)
  }

  async function followUp(message: string, attachments: ChatAttachment[] = []) {
    await api.followUp(kind.value, sessionFile.value, message, attachments)
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
      // 这一轮的流式渲染缓存可以丢了：正文已经进历史，
      // 之后渲染走的是静态路径。留着它只是白占一份全文。
      dropCache(finished.run_id)
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
    // 换会话 / 新会话时把渲染缓存全清掉：旧会话的正文不该一直占着内存。
    dropAllCaches()
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
    modelsLoading,
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
    bindErr,
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
    removeQueued,
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
