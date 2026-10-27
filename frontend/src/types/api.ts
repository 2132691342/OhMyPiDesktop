// 前端与后端的契约类型。改字段形态必须同时把 backend/server.ContractVersion +1。
// 一个域一个段落，字段名与 Go 侧的 json tag 一一对应。

export interface Envelope<T> {
  code: string
  message: string
  detail?: string
  data: T
}

// ---------------- 引擎 ----------------

export type EngineKind = 'omp' | 'pi'

/** 能力表：界面按它隐藏入口，不渲染灰按钮。 */
export interface EngineCaps {
  subagents: boolean
  goals: boolean
  todos: boolean
  login: boolean
  fragments: boolean
  queue_edit: boolean
  live_steer_count: boolean
  skill_toggle: boolean
  extensions: boolean
  export_html: boolean
  slash_commands: boolean
}

export interface EngineIdentity {
  kind: EngineKind
  exe: string
  version: string
  source: string
  caps: EngineCaps
}

export interface EngineLayout {
  kind: EngineKind
  root: string
  agent: string
  sessions: string
  skills: string
  plugins: string
  models: string
  settings: string
  exists: boolean
}

export interface RunRow {
  kind: EngineKind
  work_dir: string
  pid: number
  session_id: string
  running: boolean
}

export interface Bootstrap {
  version: string
  engines: EngineIdentity[]
  caps: Record<string, EngineCaps>
  active: EngineKind
  workspace: string
  profile: string
  settings: Record<string, string>
  layouts: Record<string, EngineLayout>
  workspaces: WorkspaceRow[]
  skills: Record<string, number>
  sessions: Record<string, number>
  install: Record<string, string>
}

export interface EngineInfo extends EngineIdentity {
  installed: boolean
  layout: {
    root: string
    agent: string
    sessions: string
    skills: string
    plugins: string
    models: string
    settings: string
    exists: boolean
  }
}

export interface EngineStatus {
  engines: EngineInfo[]
  running: RunRow[]
  install: Record<string, string>
}

export interface WorkspaceRow {
  path: string
  exists: boolean
  updated_at?: string
}

// ---------------- 会话 ----------------

export interface SessionRow {
  id: string
  path: string
  title: string
  cwd: string
  work_dir: string
  model: string
  updated_at: string
  size: number
  messages: number
  tokens: number
  cost: number
}

export interface SessionItem {
  kind: 'user' | 'assistant' | 'tool' | 'system'
  role: string
  text?: string
  thinking?: string
  blocks?: RawBlock[]
  model?: string
  tool_name?: string
  tool_call_id?: string
  is_error?: boolean
  timestamp?: number
  cost?: number
  tokens?: number
}

export interface SessionDetail extends SessionRow {
  thinking: string
  items: SessionItem[]
}

// ---------------- 对话 ----------------

export type BlockKind = 'text' | 'thinking' | 'tool'

export interface RawBlock {
  type?: string
  text?: string
  thinking?: string
  id?: string
  name?: string
  arguments?: unknown
  intent?: string
}

export interface ChatBlock {
  kind: BlockKind
  text?: string
  tool_call_id?: string
  tool_name?: string
  args?: unknown
  intent?: string
  output?: string
  is_error?: boolean
  status?: 'pending' | 'running' | 'done' | 'error'
  streaming?: boolean
  duration_ms?: number
}

export interface Turn {
  run_id: string
  user_message: string
  blocks: ChatBlock[]
  model?: string
  error?: string
  stop_reason?: string
}

export interface ModelInfo {
  id: string
  name: string
  provider: string
  api: string
  reasoning?: boolean
  contextWindow?: number
  maxTokens?: number
  input?: string[]
  cost?: { input: number; output: number; cacheRead: number; cacheWrite: number }
}

export interface ContextUsage {
  tokens: number | null
  contextWindow: number
  percent: number | null
}

export interface TokenCounts {
  input: number
  output: number
  reasoning: number
  cacheRead: number
  cacheWrite: number
  total: number
}

export interface SessionStats {
  sessionId: string
  userMessages: number
  assistantMessages: number
  toolCalls: number
  totalMessages: number
  tokens: TokenCounts
  cost: number
  contextUsage?: ContextUsage
}

export interface QueuedMessages {
  steering: string[]
  followUp: string[]
  liveSteered: number
}

export interface SlashCommand {
  name: string
  source: string
  description?: string
  aliases?: string[]
  input?: { hint?: string }
  subcommands?: { name: string; description?: string; usage?: string }[]
}

export interface HistoryTurn {
  role: 'user' | 'assistant' | 'tool'
  blocks: ChatBlock[]
  model?: string
}

export interface ChatState {
  kind: EngineKind
  session: string
  session_file: string
  work_dir: string
  engine_running: boolean
  streaming: boolean
  model?: ModelInfo
  thinking_level: string
  levels: string[]
  context?: ContextUsage
  tokens_per_second?: number
  auto_compaction: boolean
  queued?: QueuedMessages
  stats?: SessionStats
  messages: unknown[]
  history: HistoryTurn[]
  commands: SlashCommand[]
  profile: string
  caps: EngineCaps
}

// 引擎的提问卡片（四个基元 + ask 工具）
export interface AskRequest {
  id: string
  method: 'confirm' | 'select' | 'input' | 'editor' | 'ask' | 'cancel'
  title?: string
  message?: string
  placeholder?: string
  prefill?: string
  options?: string[]
  optionDetails?: { description?: string }[]
  timeout?: number
  targetId?: string
  questions?: {
    id: string
    question: string
    header?: string
    multi?: boolean
    recommended?: number
    options: { label: string; description?: string; preview?: string }[]
  }[]
}

export interface UIResponse {
  type?: string
  id: string
  value?: string
  confirmed?: boolean
  cancelled?: boolean
  timedOut?: boolean
  answers?: { id: string; selectedOptions: string[]; customInput?: string }[]
}

// ---------------- 模型与服务商 ----------------

export interface ModelRow {
  kind: EngineKind
  provider: string
  id: string
  name: string
  api: string
  context_window: number
  max_tokens: number
  reasoning: boolean
  input?: string[]
}

export interface ModelEntry {
  id: string
  name?: string
  api?: string
  reasoning?: boolean
  input?: string[]
  context_window?: number
  max_tokens?: number
}

export interface ProviderView {
  name: string
  base_url: string
  api: string
  auth: string
  api_key: string
  has_key: boolean
  models: ModelEntry[]
}

export interface ProvidersView {
  providers: ProviderView[]
  path: string
}

export interface LoginProvider {
  id: string
  name: string
  available: boolean
  authenticated: boolean
}

// ---------------- 技能与插件 ----------------

export interface SkillRow {
  name: string
  description: string
  path: string
  dir: string
  source: 'project' | 'user' | 'managed' | 'plugin'
  globs?: string[]
  always_apply?: boolean
  hidden?: boolean
  enabled: boolean
  editable: boolean
  kind: EngineKind
}

export interface PluginRow {
  kind: EngineKind
  name: string
  version: string
  source: string
  path: string
}

// ---------------- 工作区 ----------------

export interface FileEntry {
  name: string
  dir: boolean
  size?: number
}

export interface FileList {
  path: string
  entries: FileEntry[]
  truncated: boolean
}

export interface FileRead {
  path: string
  content: string
  size: number
  binary: boolean
  truncated: boolean
}

export type GitChange = 'M' | 'A' | 'D' | 'R' | 'C' | 'T' | '?' | 'U'

export interface GitFile {
  path: string
  status: string
  staged: boolean
  conflict: boolean
  additions: number
  deletions: number
}

export interface GitRemote {
  name: string
  url: string
}

export interface GitStatus {
  available: boolean
  reason: string
  is_repo: boolean
  workspace: string
  repo_root: string
  branch: string
  upstream: string
  ahead: number
  behind: number
  state: string
  state_detail: string
  remotes: GitRemote[]
  files: GitFile[]
  conflict: boolean
  git: string
}

export interface GitDiffLine {
  kind: 'file' | 'hunk' | 'add' | 'del' | 'ctx'
  old_no: number
  new_no: number
  text: string
}

export interface GitDiff {
  path: string
  truncated: boolean
  dropped: number
  lines: GitDiffLine[]
}

export interface GitCommit {
  hash: string
  short: string
  subject: string
  author: string
  when: number
  files: number
  additions: number
  deletions: number
}

/** 单个提交的完整内容：元信息 + 改动文件 + 逐行差异。 */
export interface GitCommitDetail {
  hash: string
  short: string
  subject: string
  body: string
  author: string
  when: number
  parents: string[]
  files: GitFile[]
  diff?: GitDiff
}

export interface GitBranch {
  name: string
  current: boolean
  remote: string
  ahead: number
  behind: number
}

export interface GitCommitResult {
  status: GitStatus
  hash: string
}

// ---------------- SSE ----------------

export interface StreamEvent {
  seq: number
  event: string
  data: Record<string, unknown>
  sent_at: number
}

export interface ChatPatch {
  run_id: string
  mode: 'delta' | 'block' | 'blocks'
  index: number
  kind: BlockKind
  delta?: string
  block?: ChatBlock
  blocks?: ChatBlock[]
}

// ---------------- 子代理（仅 omp） ----------------

export interface AgentProgress {
  completion: number
  stage: string
  current_tool: string
  recent_tools: string[]
  tool_count: number
  tokens: number
  context_used: number
  context_max: number
  cost: number
  duration_ms: number
  model: string
}

export interface AgentRow {
  id: string
  index: number
  agent: string
  agent_source: string
  description: string
  status: string
  task: string
  session_file: string
  parent_tool_call_id: string
  last_update: number
  progress: AgentProgress
}

export interface TranscriptPage {
  entries: Record<string, unknown>[]
  messages: Record<string, unknown>[]
  next: number
  reset: boolean
}
