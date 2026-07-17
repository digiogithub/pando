// Session & Message types (matching backend Go structs)
export interface Session {
  id: string
  parent_session_id?: string
  title: string
  message_count: number
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens?: number
  cache_creation_tokens?: number
  reasoning_tokens?: number
  cost: number
  created_at: string
  updated_at: string
  /** true when the session has an active background agent run */
  is_running?: boolean
  /** effective context window for the active model (from live token updates) */
  context_window?: number
  /** true while prompt/completion tokens are a provisional live estimate */
  tokens_estimated?: boolean
}

export interface ContentPart {
  type: 'text' | 'tool_call' | 'tool_result' | 'image' | 'reasoning'
  text?: string
  tool_name?: string
  tool_call_id?: string
  tool_input?: Record<string, unknown>
  tool_result?: string
  is_error?: boolean
  image_url?: string
}

export interface Message {
  id: string
  session_id: string
  role: 'user' | 'assistant' | 'system'
  content: ContentPart[]
  model?: string
  created_at: string
}

export interface FileNode {
  name: string
  path: string
  is_dir: boolean
  size?: number
  modified?: string
  children?: FileNode[]
}

export interface Tool {
  name: string
  description: string
  input_schema: Record<string, unknown>
}

export interface ServerStatus {
  connected: boolean
  version?: string
  model?: string
}

export interface ChatRequest {
  session_id?: string
  message: string
  model?: string
}

// Tool call kinds (matches backend toolmeta.ToolKind)
export type ToolKind = 'execute' | 'edit' | 'read' | 'search' | 'fetch' | 'think' | 'switch_mode' | 'other'

// Tool call lifecycle status (matches backend toolmeta.ToolCallStatus)
export type ToolCallStatus = 'pending' | 'in_progress' | 'completed' | 'failed'

export interface ToolCallLocation {
  path: string
}

export interface SSEToolCall {
  id: string
  name: string
  input: string
  kind?: ToolKind
  title?: string
  status?: ToolCallStatus
  locations?: ToolCallLocation[]
}

export interface SSEToolCallUpdate {
  id: string
  status?: ToolCallStatus
  kind?: ToolKind
  title?: string
  input?: string
  locations?: ToolCallLocation[]
}

export interface SSEToolResult {
  tool_call_id: string
  name: string
  content: string
  is_error: boolean
  kind?: ToolKind
  title?: string
  status?: ToolCallStatus
  locations?: ToolCallLocation[]
  raw_output?: Record<string, unknown>
  terminal?: {
    terminal_id: string
    exit_code: number
  }
  diff?: {
    file_path: string
    old_string?: string
    new_string?: string
    new_content?: string
  }
}

export interface SSEPlanEntry {
  title: string
  status: string
  active_form?: string
}

export interface GoalStatus {
  objective: string
  status: string
  iteration: number
  maxIterations: number
  progress?: string
  nextStep?: string
  startedAt: number
  completedAt?: number
}

export interface SSEEvent {
  type: 'session' | 'content' | 'content_delta' | 'thinking_delta'
      | 'tool_call' | 'tool_call_update' | 'tool_result'
      | 'plan_update' | 'todos_update' | 'goal_status'
      | 'token_usage' | 'permission_request' | 'question_request'
      | 'steering_queued' | 'steering_injected'
      | 'error' | 'done'
  session_id?: string
  content?: string
  message?: string
  error?: string
  tool_call?: SSEToolCall
  tool_call_update?: SSEToolCallUpdate
  tool_result?: SSEToolResult
  plan_entries?: SSEPlanEntry[]
  goal?: GoalStatus
  token_usage?: SSETokenUsage
  permission_request?: PermissionRequest
  question_request?: QuestionRequest
}

// PermissionRequest is a pending tool permission prompt surfaced by the agent
// when a session is not in auto-approve ("auto mode") state.
export interface PermissionRequest {
  id: string
  session_id: string
  tool_name: string
  description?: string
  action?: string
  path?: string
  params?: unknown
}

export type PermissionAction = 'allow' | 'allow_session' | 'deny'

// QuestionRequest is a set of selectable questions the agent asks the user via
// the AskUserQuestion tool. The agent blocks until the user responds.
export interface QuestionRequest {
  id: string
  session_id: string
  questions: QuestionItem[]
}

export interface QuestionItem {
  id: string
  header: string
  question: string
  multi_select: boolean
  options: QuestionOption[]
}

export interface QuestionOption {
  label: string
  description: string
}

// QuestionAnswer is the user's answer to a single question.
export interface QuestionAnswer {
  questionId: string
  selected: string[]
  otherText: string
}

// SSETokenUsage carries a live context-window token update. When estimated is true
// the value is provisional (estimated while tools run) and rendered with a "~".
export interface SSETokenUsage {
  prompt_tokens: number
  completion_tokens: number
  context_window: number
  estimated: boolean
  cache_read_tokens?: number
  cache_creation_tokens?: number
  reasoning_tokens?: number
  cost?: number
}

// Log types
export interface LogEntry {
  id: string
  timestamp: string
  level: 'debug' | 'info' | 'warn' | 'error'
  source: string
  message: string
  details?: string
}

// Snapshot types
export interface Snapshot {
  id: string
  name: string
  session_id: string
  status: 'active' | 'archived' | 'pending'
  created_at: string
  size: number
  files_count: number
}

// Evaluator / self-improvement types
export interface PromptTemplate {
  id: string
  name: string
  content: string
  ucb_score: number
  win_rate: number
  uses: number
}

export interface Skill {
  id: string
  name: string
  description: string
  confidence: number
  uses: number
}

export interface EvaluatorMetrics {
  total_sessions: number
  total_templates: number
  avg_reward: number
}

// Orchestrator types
export interface OrchestratorToolCall {
  id: string
  name: string
  title: string
  kind: string
  arguments?: Record<string, unknown>
  result?: string
  status: string
  locations?: string[]
  diffs?: Record<string, string>
  started_at: string
  ended_at?: string
}

export interface OrchestratorTask {
  id: string
  name: string
  agent: string
  model: string
  /** persona in effect for this delegated/persona-scoped run; empty when unset */
  persona?: string
  status: 'running' | 'completed' | 'error' | 'pending'
  progress: number
  tokens: number
  created_at: string
  output?: string
  current_tool?: string
  tool_calls?: OrchestratorToolCall[]
}

// DelegationMetrics mirrors orchestrator.DelegationMetricsSnapshot (item E1):
// process-lifetime counters describing warm-vs-cold routing and parent-loop
// re-entry for delegated tasks.
export interface DelegationMetrics {
  warm_attempts: number
  warm_hits: number
  external_hits: number  // subset of warm_hits served by external (editor-launched) peers (B3)
  warm_failures: number
  cold_fallbacks: number
  cap_rejections: number
  resurrections: number
  live_injections: number
  external_reattach_recovered: number // interrupted external delegations recovered on restart (A2)
  external_reattach_failed: number    // interrupted external delegations that could not be recovered (A2)
  warm_hit_rate: number // 0..1
}

// Provider config types (matching backend ProviderConfigItem)
export interface ProviderConfigItem {
  name: string
  apiKey: string // masked in GET responses (e.g. "••••last4")
  baseUrl: string
  disabled: boolean
  useOAuth: boolean
}

export interface ProvidersConfigResponse {
  providers: ProviderConfigItem[]
}

// Agent config types (matching backend AgentConfigItem)
export interface AgentConfigItem {
  name: string
  model: string
  maxTokens: number
  resolvedMaxTokens?: number // effective value after auto-budget resolution (read-only)
  reasoningEffort: string
  thinkingMode: string
  autoCompact: boolean
  autoCompactThreshold: number
}

export interface AgentsConfigResponse {
  agents: AgentConfigItem[]
}

// MCP / LSP config types
export type MCPType = 'stdio' | 'sse' | 'streamable-http'

export interface MCPToolInfo {
  name: string
  description: string
}

export interface MCPServerConfig {
  name: string
  command: string
  args: string[]
  env: string[]
  type: MCPType
  url: string
  headers: Record<string, string>
  running?: boolean
  tools?: MCPToolInfo[]
}

export interface MCPGatewayConfig {
  enabled: boolean
  favorite_threshold: number
  max_favorites: number
  favorite_window_days: number
  decay_days: number
}

export interface LSPConfig {
  language: string
  disabled: boolean
  command: string
  args: string[]
  languages: string[]
}

// Extensions config types
export interface SkillsConfig {
  enabled: boolean
  paths: string[]
}

export interface SkillsCatalogConfig {
  enabled: boolean
  baseUrl: string
  autoUpdate: boolean
  defaultScope: string // "session" | "global" | "project"
}

export interface LuaConfig {
  enabled: boolean
  script_path: string
  timeout: string  // e.g. "30s"
  strict_mode: boolean
  hot_reload: boolean
  log_filtered_data: boolean
}

export interface ExtensionsConfig {
  skills: SkillsConfig
  skillsCatalog: SkillsCatalogConfig
  lua: LuaConfig
}

// Evaluator config types (matches backend EvaluatorConfig)
export interface EvaluatorSettingsConfig {
  enabled: boolean
  model: string
  provider: string
  alphaWeight: number
  betaWeight: number
  explorationC: number
  minSessionsForUCB: number
  correctionsPatterns: string[]
  maxTokensBaseline: number
  maxSkills: number
  judgePromptTemplate: string
  async: boolean
}

// Skills catalog item (from GET /api/v1/skills/catalog?q=... → skills.sh CatalogSkill)
export interface SkillCatalogItem {
  id: string
  skillId: string
  name: string
  installs: number
  source: string // "owner/repo" GitHub source
}

// Installed skill (from GET /api/v1/skills/installed)
export interface InstalledSkill {
  name: string
  description: string
  version: string
  source: string   // "owner/repo" or "(local)"
  scope: string    // "global" | "project" | "(local)"
  active: boolean  // loaded in running SkillManager
  skillId: string
}

// Project types
export interface Project {
  id: string
  name: string
  path: string
  status: 'running' | 'stopped' | 'error' | 'initializing' | 'missing'
  initialized: boolean
  external?: boolean // running but launched by another application (e.g. an editor in ACP mode)
  delegations?: number // count of delegated agent loops running inside the warm instance
  delegation_spawned?: boolean // instance auto-started by the delegation router (vs user-activated)
  acp_pid?: number
  last_opened?: number
  created_at: number
  updated_at: number
}

// Settings / config types (matching SettingsResponse in handlers_settings.go)
export interface SettingsConfig {
  home_directory: string       // read-only: OS home dir
  working_directory: string    // editable working dir
  default_model: string
  default_provider: string
  theme: string                // combined theme ID: 'pando-light' | 'pando-dark' | etc.
  debug: boolean
  log_file: string
  auto_compact: boolean
  skills_enabled: boolean
  data_directory: string
  show_hidden_files: boolean
  nerd_fonts: boolean
  llm_cache_enabled: boolean
  output_filter_enabled: boolean        // RTK-style command-output compression (inverse of Bash.OutputFilterDisabled)
  caveman_default_mode: string          // output brevity default: '' (off) | 'lite' | 'full' | 'ultra' | 'wenyan'
  evaluator_enabled: boolean
  judge_model: string
  tool_discovery_enabled: boolean
  tool_discovery_mode: string            // 'auto' | 'always' | 'off'
  tool_discovery_max_direct_tools: number
  tool_discovery_search_limit: number
  // Delegation (mesnada delegated-task conclusions + agent-loop resurrection)
  delegation_enabled: boolean
  delegation_inject_into_live_loop: boolean
  delegation_resurrect_idle_loop: boolean
  delegation_synthesize_fallback: boolean
  delegation_max_resurrections: number
  delegation_max_depth: number
  delegation_max_concurrent: number
  delegation_resurrection_timeout: string  // e.g. "10m", "1h"
  delegation_reuse_warm_instances: boolean            // route delegated tasks to a warm per-project instance
  delegation_auto_start_warm: boolean                 // auto-start a child instance when none is running
  delegation_warm_idle_timeout: string                // stop idle warm instances after this long ("0" = never)
  delegation_warm_queue_depth: number                 // queue this many delegations for a free warm slot (0 = cold-spawn at cap)
  delegation_allow_external_warm_targets: boolean     // caller opt-in: route delegated tasks to external (editor-launched) peers over IPC
  delegation_accept_delegations: boolean              // target opt-in: accept incoming delegation requests from peers over IPC
  // UI-only fields (not persisted via /api/v1/settings)
  language: string
}

// Tools config (matching ToolsConfigResponse in handlers_config.go)
export interface ToolsConfig {
  fetchEnabled: boolean
  fetchMaxSizeMB: number

  googleSearchEnabled: boolean
  googleApiKey: string
  googleSearchEngineId: string

  braveSearchEnabled: boolean
  braveApiKey: string

  perplexitySearchEnabled: boolean
  perplexityApiKey: string

  exaSearchEnabled: boolean
  exaApiKey: string

  sourcegraphEnabled: boolean
  sourcegraphToken: string

  context7Enabled: boolean

  browserType: string
  browserExecutable: string
  browserEnabled: boolean
  browserHeadless: boolean
  browserTimeout: number
  browserUserDataDir: string
  browserMaxSessions: number
}

export interface BrowserInstallInfo {
  type: string
  label: string
  executable: string
  userDataDir?: string
}

// Bash config (matching BashConfig in config.go)
export interface BashConfig {
  bannedCommands: string[]
  allowedCommands: string[]
}

// Token Optimization config (matching TokenOptimizationConfig in config.go, plus
// the surfaced RTK shell-output filter knobs from BashConfig).
export interface TokenOptimizationConfig {
  readModeDefault: string            // "full" | "auto" | "signatures" | "map"
  readDedupDisabled: boolean
  readModeLearning: boolean
  buildCodeGraph: boolean
  relatedFilesHint: boolean
  savingsLedgerDisabled: boolean
  // Surfaced from Bash (not moved): RTK shell-output compression toggle + extra paths.
  outputFilterEnabled: boolean       // inverse of Bash.OutputFilterDisabled
  outputFilterPaths?: string[]
}

// Aggregated token-savings ledger (Phase 5), matching savings.Report in Go.
export interface SavingsSourceTotal {
  source: string
  events: number
  baseline_tokens: number
  actual_tokens: number
  saved_tokens: number
}

export interface SavingsReport {
  events: number
  baseline_tokens: number
  actual_tokens: number
  saved_tokens: number
  reduction_pct: number
  by_source: SavingsSourceTotal[] | null
  ledgerEnabled: boolean
}

export interface ContainerConfig {
  runtime: string
  image: string
  pull_policy: string
  socket: string
  work_dir: string
  network: string
  read_only: boolean
  user: string
  cpu_limit: string
  mem_limit: string
  pids_limit: number
  no_new_privileges: boolean
  allow_env: string[]
  allow_mounts: string[]
  extra_env: string[]
  extra_mounts: string[]
  embedded_cache_dir: string
  embedded_gc_keep_n: number
}

export interface RuntimeCapability {
  type: string
  available: boolean
  exec: boolean
  fs: boolean
  version?: string
  socket?: string
}

export interface ContainerCapabilitiesResponse {
  runtimes: RuntimeCapability[]
  current: string
}

export interface ContainerSessionInfo {
  sessionId: string
  runtime: string
  containerId?: string
  workDir: string
  createdAt: string
}

export interface ContainerEvent {
  sessionId: string
  runtimeType: string
  containerId?: string
  event: string
  timestamp: string
  details?: string
}

// Services config types (matching backend Go structs)

export interface MesnadaServerConfig {
  host: string
  port: number
}

export interface MesnadaOrchestratorConfig {
  storePath: string
  logDir: string
  enginesDir: string
  maxParallel: number
  defaultEngine: string
  defaultModel: string
  defaultMcpConfig: string
  personaPath: string
}

export interface MesnadaACPServerConfig {
  enabled: boolean
  transports: string[]
  host: string
  port: number
  maxSessions: number
  sessionTimeout: string
  requireAuth: boolean
}

export interface MesnadaACPConfig {
  enabled: boolean
  defaultAgent: string
  autoPermission: boolean
  server: MesnadaACPServerConfig
}

export interface MesnadaTUIConfig {
  enabled: boolean
  webui: boolean
}

export interface MesnadaConfig {
  enabled: boolean
  server: MesnadaServerConfig
  orchestrator: MesnadaOrchestratorConfig
  acp: MesnadaACPConfig
  tui: MesnadaTUIConfig
}

export interface RemembrancesConfig {
  enabled: boolean
  kb_path: string
  kb_watch: boolean
  kb_auto_import: boolean
  kb_convert_documents: boolean
  kb_wiki_links: boolean
  document_embedding_provider: string
  document_embedding_model: string
  document_embedding_base_url: string
  document_embedding_api_key: string
  code_embedding_provider: string
  code_embedding_model: string
  code_embedding_base_url: string
  code_embedding_api_key: string
  use_same_model: boolean
  chunk_size: number
  chunk_overlap: number
  index_workers: number
  // Context Enrichment
  context_enrichment_enabled: boolean
  context_enrichment_kb_results: number
  context_enrichment_code_results: number
  context_enrichment_code_project: string
  context_enrichment_events_results: number
  context_enrichment_events_subject: string
  context_enrichment_events_last_days: number
  context_enrichment_use_agent_planner: boolean
  context_enrichment_planner_fallback_to_coder: boolean
  // Memory System
  memory_enabled: boolean
  memory_context_enrichment_enabled: boolean
  memory_context_max_items: number
  memory_context_max_chars: number
  memory_default_ttl_days: number
  memory_gc_interval: string
  memory_auto_capture: boolean
}

export interface CodeProjectInfo {
  project_id: string
  name: string
  root_path: string
  status: string
}

export interface SnapshotsConfig {
  enabled: boolean
  maxSnapshots: number
  maxFileSize: string
  excludePatterns: string[]
  autoCleanupDays: number
}

export interface APIServerConfig {
  enabled: boolean
  host: string
  port: number
  requireAuth: boolean
  basicAuth?: BasicAuthConfig
}

export interface BasicAuthConfig {
  enabled: boolean
  users?: { username: string; password: string }[]
}

export interface ServicesConfig {
  mesnada: MesnadaConfig
  remembrances: RemembrancesConfig
  snapshots: SnapshotsConfig
  server: APIServerConfig
}

export interface CronJob {
  name: string
  schedule: string
  enabled: boolean
  prompt?: string
  engine?: string
  model?: string
  workDir?: string
  tags?: string[]
  timeout?: string
  nextRun?: string
}

export interface CronJobCreate {
  name: string
  schedule: string
  prompt: string
  enabled: boolean
  engine?: string
  model?: string
  workDir?: string
  tags?: string[]
  timeout?: string
}

// Provider Account types
export interface ProviderAccount {
  id: string
  displayName: string
  type: string
  apiKey?: string
  baseUrl?: string
  extraHeaders?: Record<string, string>
  disabled?: boolean
  useOAuth?: boolean
}

export interface ProviderTypeInfo {
  type: string
  displayName: string
  requiresAPIKey: boolean
  requiresBaseUrl: boolean
  supportsOAuth: boolean
  supportsExtraHeaders: boolean
}

export interface ProviderAccountTestResult {
  ok: boolean
  modelCount: number
  error?: string
}
