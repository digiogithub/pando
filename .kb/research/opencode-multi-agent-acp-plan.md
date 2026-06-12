# Update Plan: OpenCode Multi-Agent with ACP Support

## Project Context

Your fork of **OpenCode** is a terminal AI client that has stopped being maintained. The project evolved to **Crush** (Charmbracelet), which:
- Has changed its license
- Has updated support for multiple AI providers
- Uses modern architecture based on Bubble Tea

**Goal:** Modernize your fork with:
1. Updated AI provider system (based on Crush)
2. Support for **Agent Communication Protocol (ACP)** - IBM/Linux Foundation protocol for inter-agent communication
3. Multi-agent TUI to visualize and manage multiple ACP agents simultaneously

---

## Architecture Analysis

### OpenCode (Your Fork)
```
┌─────────────────────────────────────────┐
│         OpenCode Architecture           │
├─────────────────────────────────────────┤
│  • Go-based CLI                         │
│  • Bubble Tea TUI                       │
│  • SQLite session storage               │
│  • LSP integration                      │
│  • Multiple AI providers:               │
│    - OpenAI, Anthropic, Gemini          │
│    - AWS Bedrock, Groq, Azure           │
│  • Tool integration (file search, exec) │
│  • Vim-like editor                      │
└─────────────────────────────────────────┘
```

### Crush (Reference)
```
┌─────────────────────────────────────────┐
│          Crush Architecture             │
├─────────────────────────────────────────┤
│  • Provider abstraction layer           │
│  • Dynamic model switching              │
│  • MCP server support (http, stdio,sse) │
│  • Enhanced LSP integration             │
│  • Improved session management          │
│  • Cost optimization per model          │
│  • Fallback mechanisms                  │
└─────────────────────────────────────────┘
```

### Agent Communication Protocol (ACP)
```
┌─────────────────────────────────────────┐
│        ACP Protocol Overview            │
├─────────────────────────────────────────┤
│  • REST-based (HTTP/JSON)               │
│  • Agent-to-agent communication         │
│  • Synchronous & asynchronous           │
│  • Streaming support (SSE/WebSockets)   │
│  • Stateful sessions                    │
│  • Discovery mechanism                  │
│  • Task delegation & routing            │
│  • Multimodal message support           │
└─────────────────────────────────────────┘
```

---

## Proposed Architecture

### General Structure
```
opencode-multi-agent/
├── cmd/
│   ├── opencode/          # Main client (TUI)
│   ├── agent-server/      # ACP server for agents
│   └── agent-bridge/      # Bridge OpenCode ↔ ACP
│
├── internal/
│   ├── llm/
│   │   ├── provider/      # Updated AI providers
│   │   │   ├── anthropic.go
│   │   │   ├── openai.go
│   │   │   ├── gemini.go
│   │   │   ├── groq.go
│   │   │   ├── openrouter.go
│   │   │   ├── vercel.go
│   │   │   └── provider.go (common interface)
│   │   │
│   │   └── client/        # Unified LLM client
│   │       └── client.go
│   │
│   ├── acp/
│   │   ├── server/        # ACP server
│   │   │   ├── server.go
│   │   │   ├── handler.go
│   │   │   └── registry.go
│   │   │
│   │   ├── client/        # ACP client
│   │   │   ├── client.go
│   │   │   └── discovery.go
│   │   │
│   │   ├── protocol/      # Protocol definitions
│   │   │   ├── messages.go
│   │   │   ├── types.go
│   │   │   └── schema.go
│   │   │
│   │   └── agent/         # OpenCode agent wrapper
│   │       ├── agent.go
│   │       └── capabilities.go
│   │
│   ├── tui/
│   │   ├── app.go         # Updated main TUI
│   │   ├── multiagent/    # Multi-agent view
│   │   │   ├── view.go
│   │   │   ├── panel.go
│   │   │   └── layout.go
│   │   │
│   │   └── components/    # Reusable components
│   │       ├── agent_card.go
│   │       ├── status_bar.go
│   │       └── chat_view.go
│   │
│   ├── session/           # Session management
│   │   ├── manager.go
│   │   └── storage.go
│   │
│   └── tool/              # Built-in tools
│       ├── executor.go
│       └── lsp.go
│
├── pkg/
│   └── config/
│       └── config.go
│
└── go.mod
```

---

## Key Components

### 1. Updated AI Provider System

**File:** `internal/llm/provider/provider.go`

```go
package provider

import (
    "context"
    "io"
)

// Provider is the common interface for all AI providers
type Provider interface {
    // Name returns the provider name
    Name() string
    
    // Models returns the list of available models
    Models() []Model
    
    // Chat sends a message and receives a response
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    
    // Stream sends a message and receives a streaming response
    Stream(ctx context.Context, req ChatRequest) (StreamReader, error)
    
    // Capabilities returns the provider's capabilities
    Capabilities() Capabilities
    
    // ValidateConfig validates the provider configuration
    ValidateConfig(cfg Config) error
}

// Model represents an AI model
type Model struct {
    ID          string
    Name        string
    Description string
    ContextSize int
    Cost        ModelCost
    Capabilities ModelCapabilities
}

// ModelCost represents the model cost
type ModelCost struct {
    InputTokens  float64 // Cost per million input tokens
    OutputTokens float64 // Cost per million output tokens
}

// ModelCapabilities represents the model capabilities
type ModelCapabilities struct {
    Vision      bool
    FunctionCall bool
    Streaming   bool
    JSON        bool
}

// ChatRequest represents a chat request
type ChatRequest struct {
    Model       string
    Messages    []Message
    Temperature float64
    MaxTokens   int
    Stream      bool
    Tools       []Tool
}

// Message represents a message in the conversation
type Message struct {
    Role    string // system, user, assistant
    Content string
    ToolCalls []ToolCall
}

// ChatResponse represents a chat response
type ChatResponse struct {
    Content   string
    ToolCalls []ToolCall
    Usage     Usage
    Model     string
}

// Usage represents token usage
type Usage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
}

// StreamReader is a reader for streaming responses
type StreamReader interface {
    io.ReadCloser
    Next() (*StreamChunk, error)
}

// StreamChunk represents a streaming response chunk
type StreamChunk struct {
    Content string
    Delta   string
    Done    bool
}

// Capabilities represents the provider capabilities
type Capabilities struct {
    Streaming    bool
    Vision       bool
    FunctionCall bool
    JSON         bool
    ContextCache bool
}

// Config represents a provider configuration
type Config struct {
    APIKey      string
    BaseURL     string
    Model       string
    Temperature float64
    MaxTokens   int
    Extra       map[string]interface{}
}

// Tool represents a tool available to the model
type Tool struct {
    Name        string
    Description string
    Parameters  interface{}
}

// ToolCall represents a tool call
type ToolCall struct {
    ID       string
    Name     string
    Args     map[string]interface{}
}
```

**Implementation example (Anthropic):**

```go
package provider

import (
    "context"
    "fmt"
    "github.com/anthropics/anthropic-sdk-go"
)

type AnthropicProvider struct {
    client *anthropic.Client
    config Config
}

func NewAnthropicProvider(cfg Config) (*AnthropicProvider, error) {
    if cfg.APIKey == "" {
        return nil, fmt.Errorf("API key is required")
    }
    
    client := anthropic.NewClient(
        anthropic.WithAPIKey(cfg.APIKey),
    )
    
    return &AnthropicProvider{
        client: client,
        config: cfg,
    }, nil
}

func (p *AnthropicProvider) Name() string {
    return "anthropic"
}

func (p *AnthropicProvider) Models() []Model {
    return []Model{
        {
            ID:          "claude-3-5-sonnet-20241022",
            Name:        "Claude 3.5 Sonnet",
            Description: "Most intelligent model for complex tasks",
            ContextSize: 200000,
            Cost: ModelCost{
                InputTokens:  3.00,
                OutputTokens: 15.00,
            },
            Capabilities: ModelCapabilities{
                Vision:      true,
                FunctionCall: true,
                Streaming:   true,
                JSON:        true,
            },
        },
        {
            ID:          "claude-3-5-haiku-20241022",
            Name:        "Claude 3.5 Haiku",
            Description: "Fast and efficient for simpler tasks",
            ContextSize: 200000,
            Cost: ModelCost{
                InputTokens:  0.80,
                OutputTokens: 4.00,
            },
            Capabilities: ModelCapabilities{
                Vision:      true,
                FunctionCall: true,
                Streaming:   true,
                JSON:        true,
            },
        },
    }
}

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
    // Convert messages to Anthropic format
    messages := make([]anthropic.Message, len(req.Messages))
    for i, msg := range req.Messages {
        messages[i] = anthropic.Message{
            Role:    msg.Role,
            Content: msg.Content,
        }
    }
    
    // Create request
    resp, err := p.client.Messages.Create(ctx, anthropic.MessageCreateParams{
        Model:       req.Model,
        Messages:    messages,
        MaxTokens:   req.MaxTokens,
        Temperature: anthropic.Float64(req.Temperature),
    })
    
    if err != nil {
        return nil, err
    }
    
    return &ChatResponse{
        Content: resp.Content[0].Text,
        Usage: Usage{
            InputTokens:  resp.Usage.InputTokens,
            OutputTokens: resp.Usage.OutputTokens,
            TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
        },
        Model: resp.Model,
    }, nil
}

func (p *AnthropicProvider) Stream(ctx context.Context, req ChatRequest) (StreamReader, error) {
    // Implement streaming
    // ...
    return nil, nil
}

func (p *AnthropicProvider) Capabilities() Capabilities {
    return Capabilities{
        Streaming:    true,
        Vision:       true,
        FunctionCall: true,
        JSON:         true,
        ContextCache: true,
    }
}

func (p *AnthropicProvider) ValidateConfig(cfg Config) error {
    if cfg.APIKey == "" {
        return fmt.Errorf("API key is required")
    }
    return nil
}
```

---

### 2. ACP Protocol Implementation

**File:** `internal/acp/protocol/types.go`

```go
package protocol

import (
    "time"
)

// ACPVersion is the ACP protocol version
const ACPVersion = "1.0"

// Message is the ACP base message
type Message struct {
    ID          string                 `json:"id"`
    Type        MessageType            `json:"type"`
    From        AgentID                `json:"from"`
    To          AgentID                `json:"to"`
    TaskID      string                 `json:"task_id,omitempty"`
    Content     interface{}            `json:"content"`
    Metadata    map[string]interface{} `json:"metadata,omitempty"`
    Timestamp   time.Time              `json:"timestamp"`
    CorrelationID string               `json:"correlation_id,omitempty"`
}

// MessageType defines ACP message types
type MessageType string

const (
    MessageTypeRequest     MessageType = "request"
    MessageTypeResponse    MessageType = "response"
    MessageTypeNotification MessageType = "notification"
    MessageTypeError       MessageType = "error"
    MessageTypeStream      MessageType = "stream"
)

// AgentID uniquely identifies an agent
type AgentID struct {
    Name      string `json:"name"`
    Instance  string `json:"instance"`
    Framework string `json:"framework"`
}

// AgentManifest describes an agent's capabilities
type AgentManifest struct {
    ID           AgentID             `json:"id"`
    Version      string              `json:"version"`
    Description  string              `json:"description"`
    Capabilities []Capability        `json:"capabilities"`
    Endpoints    []Endpoint          `json:"endpoints"`
    Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Capability describes an agent capability
type Capability struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    InputSchema interface{}            `json:"input_schema"`
    OutputSchema interface{}           `json:"output_schema"`
    Async       bool                   `json:"async"`
    Streaming   bool                   `json:"streaming"`
}

// Endpoint defines an agent REST endpoint
type Endpoint struct {
    Path        string   `json:"path"`
    Method      string   `json:"method"`
    Description string   `json:"description"`
    ContentType []string `json:"content_type"`
}

// TaskRequest represents a task request
type TaskRequest struct {
    Action      string                 `json:"action"`
    Parameters  map[string]interface{} `json:"parameters"`
    Context     map[string]interface{} `json:"context,omitempty"`
    Priority    int                    `json:"priority,omitempty"`
    Timeout     *time.Duration         `json:"timeout,omitempty"`
}

// TaskResponse represents a task response
type TaskResponse struct {
    Status  TaskStatus             `json:"status"`
    Result  interface{}            `json:"result,omitempty"`
    Error   *ErrorDetails          `json:"error,omitempty"`
    Metrics TaskMetrics            `json:"metrics,omitempty"`
}

// TaskStatus defines task status
type TaskStatus string

const (
    TaskStatusPending   TaskStatus = "pending"
    TaskStatusRunning   TaskStatus = "running"
    TaskStatusCompleted TaskStatus = "completed"
    TaskStatusFailed    TaskStatus = "failed"
    TaskStatusCancelled TaskStatus = "cancelled"
)

// TaskMetrics contains execution metrics
type TaskMetrics struct {
    StartTime    time.Time     `json:"start_time"`
    EndTime      time.Time     `json:"end_time"`
    Duration     time.Duration `json:"duration"`
    TokensUsed   int           `json:"tokens_used,omitempty"`
    Cost         float64       `json:"cost,omitempty"`
}

// ErrorDetails provides detailed error information
type ErrorDetails struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Details interface{} `json:"details,omitempty"`
}

// StreamChunk represents a streaming response chunk
type StreamChunk struct {
    TaskID    string      `json:"task_id"`
    Sequence  int         `json:"sequence"`
    Data      interface{} `json:"data"`
    Done      bool        `json:"done"`
    Timestamp time.Time   `json:"timestamp"`
}
```

**File:** `internal/acp/server/server.go`

```go
package server

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "sync"
    
    "github.com/digiogithub/opencode/internal/acp/protocol"
    "github.com/gorilla/mux"
)

// Server is the ACP server
type Server struct {
    addr     string
    router   *mux.Router
    agents   map[string]*AgentHandler
    mu       sync.RWMutex
    server   *http.Server
}

// AgentHandler handles requests for a specific agent
type AgentHandler struct {
    manifest protocol.AgentManifest
    executor TaskExecutor
}

// TaskExecutor executes agent tasks
type TaskExecutor interface {
    Execute(ctx context.Context, req protocol.TaskRequest) (*protocol.TaskResponse, error)
    Stream(ctx context.Context, req protocol.TaskRequest) (<-chan protocol.StreamChunk, error)
}

// NewServer creates a new ACP server
func NewServer(addr string) *Server {
    router := mux.NewRouter()
    
    s := &Server{
        addr:   addr,
        router: router,
        agents: make(map[string]*AgentHandler),
    }
    
    // ACP protocol routes
    router.HandleFunc("/acp/v1/discover", s.handleDiscover).Methods("GET")
    router.HandleFunc("/acp/v1/agents", s.handleListAgents).Methods("GET")
    router.HandleFunc("/acp/v1/agents/{agent}", s.handleAgentManifest).Methods("GET")
    router.HandleFunc("/acp/v1/agents/{agent}/task", s.handleTask).Methods("POST")
    router.HandleFunc("/acp/v1/agents/{agent}/stream", s.handleStream).Methods("GET")
    
    return s
}

// RegisterAgent registers a new agent on the server
func (s *Server) RegisterAgent(manifest protocol.AgentManifest, executor TaskExecutor) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    agentKey := fmt.Sprintf("%s-%s", manifest.ID.Name, manifest.ID.Instance)
    
    if _, exists := s.agents[agentKey]; exists {
        return fmt.Errorf("agent %s already registered", agentKey)
    }
    
    s.agents[agentKey] = &AgentHandler{
        manifest: manifest,
        executor: executor,
    }
    
    return nil
}

// Start starts the server
func (s *Server) Start(ctx context.Context) error {
    s.server = &http.Server{
        Addr:    s.addr,
        Handler: s.router,
    }
    
    go func() {
        <-ctx.Done()
        s.server.Shutdown(context.Background())
    }()
    
    return s.server.ListenAndServe()
}

// handleDiscover handles the discovery request
func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    manifests := make([]protocol.AgentManifest, 0, len(s.agents))
    for _, handler := range s.agents {
        manifests = append(manifests, handler.manifest)
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "version": protocol.ACPVersion,
        "agents":  manifests,
    })
}

// handleListAgents lists all agents
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    agentIDs := make([]protocol.AgentID, 0, len(s.agents))
    for _, handler := range s.agents {
        agentIDs = append(agentIDs, handler.manifest.ID)
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(agentIDs)
}

// handleAgentManifest returns an agent's manifest
func (s *Server) handleAgentManifest(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    agentKey := vars["agent"]
    
    s.mu.RLock()
    handler, exists := s.agents[agentKey]
    s.mu.RUnlock()
    
    if !exists {
        http.Error(w, "Agent not found", http.StatusNotFound)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(handler.manifest)
}

// handleTask handles a task request
func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    agentKey := vars["agent"]
    
    s.mu.RLock()
    handler, exists := s.agents[agentKey]
    s.mu.RUnlock()
    
    if !exists {
        http.Error(w, "Agent not found", http.StatusNotFound)
        return
    }
    
    var req protocol.TaskRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }
    
    resp, err := handler.executor.Execute(r.Context(), req)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

// handleStream handles a streaming request
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    agentKey := vars["agent"]
    
    s.mu.RLock()
    handler, exists := s.agents[agentKey]
    s.mu.RUnlock()
    
    if !exists {
        http.Error(w, "Agent not found", http.StatusNotFound)
        return
    }
    
    var req protocol.TaskRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }
    
    // Configure SSE
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    
    chunks, err := handler.executor.Stream(r.Context(), req)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "Streaming not supported", http.StatusInternalServerError)
        return
    }
    
    for chunk := range chunks {
        data, _ := json.Marshal(chunk)
        fmt.Fprintf(w, "data: %s\n\n", data)
        flusher.Flush()
        
        if chunk.Done {
            break
        }
    }
}
```

**File:** `internal/acp/client/client.go`

```go
package client

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    
    "github.com/digiogithub/opencode/internal/acp/protocol"
)

// Client is the ACP client
type Client struct {
    baseURL    string
    httpClient *http.Client
}

// NewClient creates a new ACP client
func NewClient(baseURL string) *Client {
    return &Client{
        baseURL:    baseURL,
        httpClient: &http.Client{},
    }
}

// Discover discovers available agents
func (c *Client) Discover(ctx context.Context) ([]protocol.AgentManifest, error) {
    url := fmt.Sprintf("%s/acp/v1/discover", c.baseURL)
    
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }
    
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("discovery failed: %d", resp.StatusCode)
    }
    
    var result struct {
        Version string                    `json:"version"`
        Agents  []protocol.AgentManifest  `json:"agents"`
    }
    
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }
    
    return result.Agents, nil
}

// SendTask sends a task to an agent
func (c *Client) SendTask(ctx context.Context, agentKey string, req protocol.TaskRequest) (*protocol.TaskResponse, error) {
    url := fmt.Sprintf("%s/acp/v1/agents/%s/task", c.baseURL, agentKey)
    
    body, err := json.Marshal(req)
    if err != nil {
        return nil, err
    }
    
    httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("Content-Type", "application/json")
    
    resp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("task failed: %d", resp.StatusCode)
    }
    
    var taskResp protocol.TaskResponse
    if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
        return nil, err
    }
    
    return &taskResp, nil
}

// StreamTask sends a task and receives the streaming response
func (c *Client) StreamTask(ctx context.Context, agentKey string, req protocol.TaskRequest) (<-chan protocol.StreamChunk, error) {
    url := fmt.Sprintf("%s/acp/v1/agents/%s/stream", c.baseURL, agentKey)
    
    body, err := json.Marshal(req)
    if err != nil {
        return nil, err
    }
    
    httpReq, err := http.NewRequestWithContext(ctx, "GET", url, bytes.NewReader(body))
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("Accept", "text/event-stream")
    
    resp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return nil, err
    }
    
    if resp.StatusCode != http.StatusOK {
        resp.Body.Close()
        return nil, fmt.Errorf("stream failed: %d", resp.StatusCode)
    }
    
    chunks := make(chan protocol.StreamChunk)
    
    go func() {
        defer resp.Body.Close()
        defer close(chunks)
        
        decoder := json.NewDecoder(resp.Body)
        for {
            var chunk protocol.StreamChunk
            if err := decoder.Decode(&chunk); err != nil {
                return
            }
            
            select {
            case chunks <- chunk:
                if chunk.Done {
                    return
                }
            case <-ctx.Done():
                return
            }
        }
    }()
    
    return chunks, nil
}
```

---

### 3. Multi-Agent TUI

**File:** `internal/tui/multiagent/view.go`

```go
package multiagent

import (
    "fmt"
    
    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
    "github.com/digiogithub/opencode/internal/acp/protocol"
)

// Model is the model for the multi-agent view
type Model struct {
    agents       []AgentPanel
    activeAgent  int
    width        int
    height       int
    layout       Layout
}

// AgentPanel represents an agent panel
type AgentPanel struct {
    ID       protocol.AgentID
    Manifest protocol.AgentManifest
    Messages []Message
    Status   AgentStatus
    Active   bool
}

// AgentStatus represents agent status
type AgentStatus string

const (
    AgentStatusIdle    AgentStatus = "idle"
    AgentStatusBusy    AgentStatus = "busy"
    AgentStatusError   AgentStatus = "error"
    AgentStatusOffline AgentStatus = "offline"
)

// Message represents a message in the agent panel
type Message struct {
    Role      string
    Content   string
    Timestamp string
}

// Layout defines panel layout
type Layout string

const (
    LayoutGrid       Layout = "grid"       // Grid
    LayoutHorizontal Layout = "horizontal" // Horizontal
    LayoutVertical   Layout = "vertical"   // Vertical
    LayoutFocus      Layout = "focus"      // One agent focused large
)

// NewModel creates a new multi-agent model
func NewModel() Model {
    return Model{
        agents:      make([]AgentPanel, 0),
        activeAgent: 0,
        layout:      LayoutGrid,
    }
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
    return nil
}

// Update updates the model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "ctrl+c", "q":
            return m, tea.Quit
            
        case "tab":
            // Switch to next agent
            m.activeAgent = (m.activeAgent + 1) % len(m.agents)
            return m, nil
            
        case "shift+tab":
            // Switch to previous agent
            m.activeAgent = (m.activeAgent - 1 + len(m.agents)) % len(m.agents)
            return m, nil
            
        case "1":
            m.layout = LayoutGrid
            return m, nil
        case "2":
            m.layout = LayoutHorizontal
            return m, nil
        case "3":
            m.layout = LayoutVertical
            return m, nil
        case "4":
            m.layout = LayoutFocus
            return m, nil
        }
        
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        return m, nil
        
    case AgentAddedMsg:
        m.agents = append(m.agents, AgentPanel{
            ID:       msg.ID,
            Manifest: msg.Manifest,
            Messages: make([]Message, 0),
            Status:   AgentStatusIdle,
        })
        return m, nil
        
    case AgentMessageMsg:
        // Add message to agent
        for i := range m.agents {
            if m.agents[i].ID == msg.AgentID {
                m.agents[i].Messages = append(m.agents[i].Messages, Message{
                    Role:      msg.Role,
                    Content:   msg.Content,
                    Timestamp: msg.Timestamp,
                })
                break
            }
        }
        return m, nil
        
    case AgentStatusMsg:
        // Update agent status
        for i := range m.agents {
            if m.agents[i].ID == msg.AgentID {
                m.agents[i].Status = msg.Status
                break
            }
        }
        return m, nil
    }
    
    return m, nil
}

// View renders the view
func (m Model) View() string {
    if len(m.agents) == 0 {
        return noAgentsView(m.width, m.height)
    }
    
    switch m.layout {
    case LayoutGrid:
        return m.gridView()
    case LayoutHorizontal:
        return m.horizontalView()
    case LayoutVertical:
        return m.verticalView()
    case LayoutFocus:
        return m.focusView()
    default:
        return m.gridView()
    }
}

// gridView renders the grid view
func (m Model) gridView() string {
    if len(m.agents) == 0 {
        return ""
    }
    
    // Calculate grid dimensions
    cols := 2
    if len(m.agents) == 1 {
        cols = 1
    }
    rows := (len(m.agents) + cols - 1) / cols
    
    panelWidth := m.width / cols
    panelHeight := (m.height - 3) / rows // -3 for status bar
    
    var grid []string
    for row := 0; row < rows; row++ {
        var rowPanels []string
        for col := 0; col < cols; col++ {
            idx := row*cols + col
            if idx < len(m.agents) {
                active := idx == m.activeAgent
                panel := renderAgentPanel(m.agents[idx], panelWidth, panelHeight, active)
                rowPanels = append(rowPanels, panel)
            }
        }
        grid = append(grid, lipgloss.JoinHorizontal(lipgloss.Top, rowPanels...))
    }
    
    content := lipgloss.JoinVertical(lipgloss.Left, grid...)
    statusBar := renderStatusBar(m)
    
    return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
}

// horizontalView renders the horizontal view
func (m Model) horizontalView() string {
    panelWidth := m.width / len(m.agents)
    panelHeight := m.height - 3
    
    var panels []string
    for i, agent := range m.agents {
        active := i == m.activeAgent
        panel := renderAgentPanel(agent, panelWidth, panelHeight, active)
        panels = append(panels, panel)
    }
    
    content := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
    statusBar := renderStatusBar(m)
    
    return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
}

// verticalView renders the vertical view
func (m Model) verticalView() string {
    panelWidth := m.width
    panelHeight := (m.height - 3) / len(m.agents)
    
    var panels []string
    for i, agent := range m.agents {
        active := i == m.activeAgent
        panel := renderAgentPanel(agent, panelWidth, panelHeight, active)
        panels = append(panels, panel)
    }
    
    content := lipgloss.JoinVertical(lipgloss.Left, panels...)
    statusBar := renderStatusBar(m)
    
    return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
}

// focusView renders the focused view
func (m Model) focusView() string {
    if m.activeAgent >= len(m.agents) {
        return ""
    }
    
    agent := m.agents[m.activeAgent]
    panel := renderAgentPanel(agent, m.width, m.height-3, true)
    statusBar := renderStatusBar(m)
    
    return lipgloss.JoinVertical(lipgloss.Left, panel, statusBar)
}

// renderAgentPanel renders an agent panel
func renderAgentPanel(agent AgentPanel, width, height int, active bool) string {
    // Styles
    borderColor := lipgloss.Color("240")
    if active {
        borderColor = lipgloss.Color("86") // Green
    }
    
    borderStyle := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(borderColor).
        Width(width - 2).
        Height(height - 2).
        Padding(1)
    
    statusColor := lipgloss.Color("240")
    switch agent.Status {
    case AgentStatusIdle:
        statusColor = lipgloss.Color("86") // Green
    case AgentStatusBusy:
        statusColor = lipgloss.Color("226") // Yellow
    case AgentStatusError:
        statusColor = lipgloss.Color("196") // Red
    case AgentStatusOffline:
        statusColor = lipgloss.Color("240") // Gray
    }
    
    statusStyle := lipgloss.NewStyle().
        Foreground(statusColor).
        Bold(true)
    
    // Header
    header := fmt.Sprintf("%s [%s]", 
        agent.ID.Name, 
        statusStyle.Render(string(agent.Status)))
    
    // Messages
    var messages []string
    startIdx := 0
    if len(agent.Messages) > height-6 {
        startIdx = len(agent.Messages) - (height - 6)
    }
    
    for _, msg := range agent.Messages[startIdx:] {
        roleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
        if msg.Role == "assistant" {
            roleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
        }
        
        messages = append(messages, fmt.Sprintf("%s: %s",
            roleStyle.Render(msg.Role),
            msg.Content))
    }
    
    content := lipgloss.JoinVertical(lipgloss.Left,
        header,
        "",
        lipgloss.JoinVertical(lipgloss.Left, messages...),
    )
    
    return borderStyle.Render(content)
}

// renderStatusBar renders the status bar
func renderStatusBar(m Model) string {
    statusStyle := lipgloss.NewStyle().
        Background(lipgloss.Color("235")).
        Foreground(lipgloss.Color("250")).
        Padding(0, 1)
    
    layoutName := ""
    switch m.layout {
    case LayoutGrid:
        layoutName = "Grid"
    case LayoutHorizontal:
        layoutName = "Horizontal"
    case LayoutVertical:
        layoutName = "Vertical"
    case LayoutFocus:
        layoutName = "Focus"
    }
    
    status := fmt.Sprintf("Agents: %d | Active: %s | Layout: %s [1-4] | Tab: Next | Q: Quit",
        len(m.agents),
        m.agents[m.activeAgent].ID.Name,
        layoutName)
    
    return statusStyle.Width(m.width).Render(status)
}

// noAgentsView renders the view when no agents are connected
func noAgentsView(width, height int) string {
    style := lipgloss.NewStyle().
        Width(width).
        Height(height).
        AlignHorizontal(lipgloss.Center).
        AlignVertical(lipgloss.Center)
    
    return style.Render("No agents connected.\n\nPress 'q' to quit.")
}

// Custom messages

type AgentAddedMsg struct {
    ID       protocol.AgentID
    Manifest protocol.AgentManifest
}

type AgentMessageMsg struct {
    AgentID   protocol.AgentID
    Role      string
    Content   string
    Timestamp string
}

type AgentStatusMsg struct {
    AgentID protocol.AgentID
    Status  AgentStatus
}
```

---

## Implementation Plan

### Phase 1: Provider System Update (1-2 weeks)

**Tasks:**
1. ✅ Create unified `Provider` interface in `internal/llm/provider/provider.go`
2. ✅ Implement updated Anthropic provider
3. ✅ Implement updated OpenAI provider
4. ✅ Implement Google Gemini provider
5. ✅ Implement Groq provider
6. ✅ Implement OpenRouter provider
7. ✅ Implement Vercel AI Gateway provider
8. ✅ Create provider registry system
9. ✅ Update configuration to support multiple providers
10. ✅ Unit tests for each provider

**Files to create/modify:**
- `internal/llm/provider/provider.go` (new)
- `internal/llm/provider/anthropic.go` (update)
- `internal/llm/provider/openai.go` (update)
- `internal/llm/provider/gemini.go` (update)
- `internal/llm/provider/groq.go` (new)
- `internal/llm/provider/openrouter.go` (new)
- `internal/llm/provider/vercel.go` (new)
- `internal/llm/provider/registry.go` (new)
- `pkg/config/config.go` (update)

### Phase 2: ACP Protocol Implementation (2-3 weeks)

**Tasks:**
1. ✅ Define ACP protocol types in `internal/acp/protocol/`
2. ✅ Implement ACP server
3. ✅ Implement ACP client
4. ✅ Create OpenCode agent wrapper
5. ✅ Implement agent discovery
6. ✅ Implement synchronous communication
7. ✅ Implement asynchronous communication
8. ✅ Implement SSE streaming
9. ✅ Integration tests

**Files to create:**
- `internal/acp/protocol/types.go`
- `internal/acp/protocol/messages.go`
- `internal/acp/server/server.go`
- `internal/acp/server/handler.go`
- `internal/acp/server/registry.go`
- `internal/acp/client/client.go`
- `internal/acp/client/discovery.go`
- `internal/acp/agent/agent.go`
- `internal/acp/agent/capabilities.go`
- `cmd/agent-server/main.go`

### Phase 3: Multi-Agent TUI (2-3 weeks)

**Tasks:**
1. ✅ Design multi-agent view architecture
2. ✅ Implement Bubble Tea model for multi-agent
3. ✅ Implement grid view
4. ✅ Implement horizontal view
5. ✅ Implement vertical view
6. ✅ Implement focused view
7. ✅ Implement agent panel component
8. ✅ Implement status bar
9. ✅ Integrate with ACP client
10. ✅ Add hotkeys for navigation
11. ✅ UI tests

**Files to create:**
- `internal/tui/multiagent/view.go`
- `internal/tui/multiagent/model.go`
- `internal/tui/multiagent/panel.go`
- `internal/tui/multiagent/layout.go`
- `internal/tui/components/agent_card.go`
- `internal/tui/components/status_bar.go`
- `internal/tui/components/chat_view.go`

### Phase 4: Integration and Testing (1-2 weeks)

**Tasks:**
1. ✅ Integrate provider system with TUI
2. ✅ Integrate ACP server with agents
3. ✅ Create CLI commands to manage agents
4. ✅ Implement multi-agent session persistence
5. ✅ Usage documentation
6. ✅ Configuration examples
7. ✅ End-to-end tests
8. ✅ Performance optimization

---

## Example Configuration

**File:** `~/.opencode.json`

```json
{
  "providers": [
    {
      "name": "anthropic",
      "api_key": "${ANTHROPIC_API_KEY}",
      "model": "claude-3-5-sonnet-20241022",
      "temperature": 0.7,
      "max_tokens": 4096
    },
    {
      "name": "openai",
      "api_key": "${OPENAI_API_KEY}",
      "model": "gpt-4-turbo-preview",
      "temperature": 0.7
    },
    {
      "name": "groq",
      "api_key": "${GROQ_API_KEY}",
      "model": "llama-3.1-70b-versatile",
      "temperature": 0.5
    }
  ],
  "acp": {
    "server": {
      "enabled": true,
      "address": "localhost:8080"
    },
    "agents": [
      {
        "name": "code-assistant",
        "provider": "anthropic",
        "capabilities": ["code_generation", "code_review", "debugging"],
        "description": "AI assistant for coding tasks"
      },
      {
        "name": "research-assistant",
        "provider": "openai",
        "capabilities": ["research", "summarization", "analysis"],
        "description": "AI assistant for research tasks"
      },
      {
        "name": "fast-assistant",
        "provider": "groq",
        "capabilities": ["quick_tasks", "simple_queries"],
        "description": "Fast AI assistant for simple tasks"
      }
    ]
  },
  "tui": {
    "default_layout": "grid",
    "theme": "dark",
    "font_size": 14
  }
}
```

---

## Application Usage

### Start ACP Server

```bash
# Start ACP server in background
opencode agent-server --config ~/.opencode.json --port 8080

# Or as daemon
opencode agent-server --daemon
```

### Start Multi-Agent Client

```bash
# Start TUI with all configured agents
opencode multi

# Start TUI connecting to remote server
opencode multi --server http://remote:8080

# Start with specific layout
opencode multi --layout grid
```

### TUI Commands

- `Tab` / `Shift+Tab`: Navigate between agents
- `1`: Grid view
- `2`: Horizontal view
- `3`: Vertical view
- `4`: Focused view (active agent full screen)
- `Ctrl+N`: New message to active agent
- `Ctrl+S`: Change active agent provider
- `Ctrl+D`: Disconnect agent
- `Ctrl+R`: Reconnect agent
- `Q`: Quit

### Connect External Agents

```bash
# Discover available agents
opencode agent discover --server http://localhost:8080

# Connect external agent
opencode agent connect --name external-agent --url http://external:9000
```

---

## Advantages of Proposed Architecture

### 1. Provider Flexibility
- ✅ Support for multiple simultaneous providers
- ✅ Dynamic provider switching per agent
- ✅ Cost optimization using appropriate models per task

### 2. Interoperability (ACP)
- ✅ Standard inter-agent communication
- ✅ Framework-independent (LangChain, CrewAI, etc.)
- ✅ Automatic agent discovery
- ✅ Support for remote agents

### 3. User Experience
- ✅ Real-time multi-agent visualization
- ✅ Multiple adaptable layouts
- ✅ Familiar interface (Bubble Tea)
- ✅ Intuitive hotkeys

### 4. Scalability
- ✅ Modular architecture
- ✅ Easy to add new providers
- ✅ Easy to add new agents
- ✅ Support for distributed agents

### 5. Open Source & Community
- ✅ Maintains original OpenCode spirit
- ✅ Standard open ACP protocol (Linux Foundation)
- ✅ Compatible with existing ecosystem

---

## Go Dependencies

```go
module github.com/digiogithub/opencode

go 1.22

require (
    // TUI
    github.com/charmbracelet/bubbletea v0.26.0
    github.com/charmbracelet/lipgloss v0.10.0
    github.com/charmbracelet/bubbles v0.18.0
    
    // AI Providers
    github.com/anthropics/anthropic-sdk-go v0.1.0
    github.com/sashabaranov/go-openai v1.24.0
    
    // ACP/HTTP
    github.com/gorilla/mux v1.8.1
    github.com/gorilla/websocket v1.5.1
    
    // Config
    github.com/spf13/viper v1.18.2
    
    // Database
    github.com/mattn/go-sqlite3 v1.14.22
    
    // LSP
    github.com/tliron/glsp v0.2.2
    
    // Utils
    github.com/google/uuid v1.6.0
    go.uber.org/zap v1.27.0
)
```

---

## Next Steps

1. **Fork and Setup**
   - Clone your fork
   - Create `feature/multi-agent-acp` branch
   - Set up folder structure

2. **Incremental Development**
   - Phase 1: Providers (maintain current functionality)
   - Phase 2: ACP (add communication capability)
   - Phase 3: Multi-Agent TUI (new interface)

3. **Continuous Testing**
   - Unit tests per component
   - Integration tests between phases
   - Manual TUI testing

4. **Documentation**
   - Updated README
   - Usage guides
   - Configuration examples
   - ACP API documentation

---

## Additional Resources

### ACP References
- Specification: https://agentcommunicationprotocol.dev
- BeeAI Framework: https://github.com/i-am-bee/bee-agent-framework
- IBM Research: https://research.ibm.com/projects/agent-communication-protocol

### Crush References
- Repo: https://github.com/charmbracelet/crush
- Docs: https://github.com/charmbracelet/crush/tree/main/docs

### Bubble Tea References
- Repo: https://github.com/charmbracelet/bubbletea
- Tutorial: https://github.com/charmbracelet/bubbletea/tree/master/tutorials
- Examples: https://github.com/charmbracelet/bubbletea/tree/master/examples

---

## Contact and Support

For questions about the implementation:
- Issues on the GitHub fork
- Discussions in the repo
- Inline documentation in the code

---

**This plan provides a complete roadmap for modernizing your OpenCode fork with multi-agent capabilities and ACP communication, maintaining the essence of the original project while adding enterprise-level features.**
