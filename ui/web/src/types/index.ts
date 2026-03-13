export interface Message {
  id: string;
  role: "user" | "assistant" | "system";
  content: string;
  timestamp: Date;
  isStreaming?: boolean;
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
  type: "content_delta" | "tool_use" | "complete" | "error";
  content?: string;
  tool_name?: string;
  tool_args?: Record<string, unknown>;
  error?: string;
}
