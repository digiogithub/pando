export interface ToolCall {
  id: string;
  name: string;
  input: unknown;
}

export interface ToolResult {
  tool_call_id: string;
  name: string;
  content: string;
  metadata?: unknown;
  is_error?: boolean;
}

export interface Message {
  id: string;
  role: "user" | "assistant" | "system";
  content: string;
  timestamp: Date;
  isStreaming?: boolean;
  thinking?: string;
  toolCalls?: ToolCall[];
  toolResults?: ToolResult[];
}

export interface Session {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
  message_count: number;
}

export interface ProjectContext {
  cwd: string;
  name: string;
  files: FileNode[];
}

export interface FileNode {
  name: string;
  path: string;
  is_dir: boolean;
  children?: FileNode[];
}

export interface Tool {
  name: string;
  description: string;
  input_schema: Record<string, unknown>;
}

export interface ServerStatus {
  connected: boolean;
  version: string;
  cwd: string;
}

export interface ChatRequest {
  prompt: string;
  session_id?: string;
  model?: string;
}

export interface SSEEvent {
  type?: "session" | "thinking_delta" | "content_delta" | "tool_call" | "tool_result" | "done" | "error";
  sessionId?: string;
  text?: string;
  id?: string;
  name?: string;
  input?: unknown;
  tool_call_id?: string;
  content?: string;
  metadata?: unknown;
  is_error?: boolean;
  error?: string;
}
