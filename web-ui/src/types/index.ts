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
  type: 'text' | 'tool_call' | 'tool_result' | 'image'
  text?: string
  tool_name?: string
  tool_input?: Record<string, unknown>
  tool_result?: string
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

export interface SSEEvent {
  type: 'session' | 'content' | 'error' | 'done'
  session_id?: string
  content?: string
  error?: string
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

export interface MCPServerConfig {
  name: string
  command: string
  args: string[]
  env: string[]
  type: MCPType
  url: string
  headers: Record<string, string>
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

// Skills catalog item (from GET /api/v1/skills/catalog)
export interface SkillCatalogItem {
  name: string
  description: string
  version: string
  author?: string
  tags?: string[]
  installed?: boolean
}

// Settings / config types
export interface SettingsConfig {
  home_directory: string
  default_model: string
  default_provider: string
  language: string
  theme: 'light' | 'dark'
  auto_save: boolean
  markdown_preview: boolean
  custom_instructions: string
}
