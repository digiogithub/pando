// Session & Message types (matching backend Go structs)
export interface Session {
  id: string
  parent_session_id?: string
  title: string
  message_count: number
  prompt_tokens: number
  completion_tokens: number
  cost: number
  created_at: string
  updated_at: string
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

export interface SSEToolCall {
  id: string
  name: string
  input: string
}

export interface SSEToolResult {
  tool_call_id: string
  name: string
  content: string
  is_error: boolean
}

export interface SSEEvent {
  type: 'session' | 'content' | 'content_delta' | 'thinking_delta' | 'tool_call' | 'tool_result' | 'error' | 'done'
  session_id?: string
  content?: string
  error?: string
  tool_call?: SSEToolCall
  tool_result?: SSEToolResult
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
  status: 'running' | 'completed' | 'error' | 'pending'
  progress: number
  tokens: number
  created_at: string
  output?: string
  current_tool?: string
  tool_calls?: OrchestratorToolCall[]
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
  reasoningEffort: string
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
  acp_pid?: number
  last_opened?: number
  created_at: number
  updated_at: number
}

// Settings / config types
export interface SettingsConfig {
  home_directory: string
  default_model: string
  default_provider: string
  language: string
  theme: string  // combined theme ID: 'pando-light' | 'pando-dark' | 'claude-light' | etc.
  auto_save: boolean
  markdown_preview: boolean
  custom_instructions: string
  llm_cache_enabled: boolean
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

  context7Enabled: boolean

  browserEnabled: boolean
  browserHeadless: boolean
  browserTimeout: number
  browserUserDataDir: string
  browserMaxSessions: number
}

// Bash config (matching BashConfig in config.go)
export interface BashConfig {
  bannedCommands: string[]
  allowedCommands: string[]
}

// Services config types (matching backend Go structs)

export interface MesnadaServerConfig {
  host: string
  port: number
}

export interface MesnadaOrchestratorConfig {
  storePath: string
  logDir: string
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
