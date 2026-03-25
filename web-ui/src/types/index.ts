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
