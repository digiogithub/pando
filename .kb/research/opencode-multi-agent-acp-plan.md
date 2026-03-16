# Plan de Actualización: OpenCode Multi-Agent con Soporte ACP

## Contexto del Proyecto

Tu fork de **OpenCode** es un cliente terminal AI que ha dejado de mantenerse. El proyecto evolucionó a **Crush** (Charmbracelet), que:
- Ha cambiado de licencia
- Tiene soporte actualizado para múltiples proveedores AI
- Usa arquitectura moderna basada en Bubble Tea

**Objetivo:** Modernizar tu fork con:
1. Sistema de proveedores AI actualizado (basado en Crush)
2. Soporte para **Agent Communication Protocol (ACP)** - protocolo IBM/Linux Foundation para comunicación entre agentes
3. TUI multi-agente para visualizar y gestionar múltiples agentes ACP simultáneamente

---

## Análisis de Arquitectura

### OpenCode (Tu Fork)
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

### Crush (Referencia)
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

## Arquitectura Propuesta

### Estructura General
```
opencode-multi-agent/
├── cmd/
│   ├── opencode/          # Cliente principal (TUI)
│   ├── agent-server/      # Servidor ACP para agentes
│   └── agent-bridge/      # Bridge OpenCode ↔ ACP
│
├── internal/
│   ├── llm/
│   │   ├── provider/      # Proveedores AI actualizados
│   │   │   ├── anthropic.go
│   │   │   ├── openai.go
│   │   │   ├── gemini.go
│   │   │   ├── groq.go
│   │   │   ├── openrouter.go
│   │   │   ├── vercel.go
│   │   │   └── provider.go (interface común)
│   │   │
│   │   └── client/        # Cliente LLM unificado
│   │       └── client.go
│   │
│   ├── acp/
│   │   ├── server/        # Servidor ACP
│   │   │   ├── server.go
│   │   │   ├── handler.go
│   │   │   └── registry.go
│   │   │
│   │   ├── client/        # Cliente ACP
│   │   │   ├── client.go
│   │   │   └── discovery.go
│   │   │
│   │   ├── protocol/      # Definiciones del protocolo
│   │   │   ├── messages.go
│   │   │   ├── types.go
│   │   │   └── schema.go
│   │   │
│   │   └── agent/         # Wrapper de agente OpenCode
│   │       ├── agent.go
│   │       └── capabilities.go
│   │
│   ├── tui/
│   │   ├── app.go         # TUI principal actualizado
│   │   ├── multiagent/    # Vista multi-agente
│   │   │   ├── view.go
│   │   │   ├── panel.go
│   │   │   └── layout.go
│   │   │
│   │   └── components/    # Componentes reutilizables
│   │       ├── agent_card.go
│   │       ├── status_bar.go
│   │       └── chat_view.go
│   │
│   ├── session/           # Gestión de sesiones
│   │   ├── manager.go
│   │   └── storage.go
│   │
│   └── tool/              # Herramientas integradas
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

## Componentes Clave

### 1. Sistema de Proveedores AI Actualizado

**Archivo:** `internal/llm/provider/provider.go`

```go
package provider

import (
    "context"
    "io"
)

// Provider es la interfaz común para todos los proveedores de AI
type Provider interface {
    // Name devuelve el nombre del proveedor
    Name() string
    
    // Models devuelve la lista de modelos disponibles
    Models() []Model
    
    // Chat envía un mensaje y recibe una respuesta
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    
    // Stream envía un mensaje y recibe una respuesta en streaming
    Stream(ctx context.Context, req ChatRequest) (StreamReader, error)
    
    // Capabilities devuelve las capacidades del proveedor
    Capabilities() Capabilities
    
    // ValidateConfig valida la configuración del proveedor
    ValidateConfig(cfg Config) error
}

// Model representa un modelo de AI
type Model struct {
    ID          string
    Name        string
    Description string
    ContextSize int
    Cost        ModelCost
    Capabilities ModelCapabilities
}

// ModelCost representa el coste del modelo
type ModelCost struct {
    InputTokens  float64 // Coste por millón de tokens de entrada
    OutputTokens float64 // Coste por millón de tokens de salida
}

// ModelCapabilities representa las capacidades del modelo
type ModelCapabilities struct {
    Vision      bool
    FunctionCall bool
    Streaming   bool
    JSON        bool
}

// ChatRequest representa una solicitud de chat
type ChatRequest struct {
    Model       string
    Messages    []Message
    Temperature float64
    MaxTokens   int
    Stream      bool
    Tools       []Tool
}

// Message representa un mensaje en la conversación
type Message struct {
    Role    string // system, user, assistant
    Content string
    ToolCalls []ToolCall
}

// ChatResponse representa una respuesta de chat
type ChatResponse struct {
    Content   string
    ToolCalls []ToolCall
    Usage     Usage
    Model     string
}

// Usage representa el uso de tokens
type Usage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
}

// StreamReader es un reader para respuestas en streaming
type StreamReader interface {
    io.ReadCloser
    Next() (*StreamChunk, error)
}

// StreamChunk representa un fragmento de respuesta streaming
type StreamChunk struct {
    Content string
    Delta   string
    Done    bool
}

// Capabilities representa las capacidades del proveedor
type Capabilities struct {
    Streaming    bool
    Vision       bool
    FunctionCall bool
    JSON         bool
    ContextCache bool
}

// Config representa la configuración de un proveedor
type Config struct {
    APIKey      string
    BaseURL     string
    Model       string
    Temperature float64
    MaxTokens   int
    Extra       map[string]interface{}
}

// Tool representa una herramienta disponible para el modelo
type Tool struct {
    Name        string
    Description string
    Parameters  interface{}
}

// ToolCall representa una llamada a una herramienta
type ToolCall struct {
    ID       string
    Name     string
    Args     map[string]interface{}
}
```

**Ejemplo de implementación (Anthropic):**

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
    // Convertir mensajes al formato de Anthropic
    messages := make([]anthropic.Message, len(req.Messages))
    for i, msg := range req.Messages {
        messages[i] = anthropic.Message{
            Role:    msg.Role,
            Content: msg.Content,
        }
    }
    
    // Crear solicitud
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
    // Implementar streaming
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

### 2. Implementación del Protocolo ACP

**Archivo:** `internal/acp/protocol/types.go`

```go
package protocol

import (
    "time"
)

// ACPVersion es la versión del protocolo ACP
const ACPVersion = "1.0"

// Message es el mensaje base de ACP
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

// MessageType define los tipos de mensajes ACP
type MessageType string

const (
    MessageTypeRequest     MessageType = "request"
    MessageTypeResponse    MessageType = "response"
    MessageTypeNotification MessageType = "notification"
    MessageTypeError       MessageType = "error"
    MessageTypeStream      MessageType = "stream"
)

// AgentID identifica un agente de forma única
type AgentID struct {
    Name      string `json:"name"`
    Instance  string `json:"instance"`
    Framework string `json:"framework"`
}

// AgentManifest describe las capacidades de un agente
type AgentManifest struct {
    ID           AgentID             `json:"id"`
    Version      string              `json:"version"`
    Description  string              `json:"description"`
    Capabilities []Capability        `json:"capabilities"`
    Endpoints    []Endpoint          `json:"endpoints"`
    Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Capability describe una capacidad del agente
type Capability struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    InputSchema interface{}            `json:"input_schema"`
    OutputSchema interface{}           `json:"output_schema"`
    Async       bool                   `json:"async"`
    Streaming   bool                   `json:"streaming"`
}

// Endpoint define un endpoint REST del agente
type Endpoint struct {
    Path        string   `json:"path"`
    Method      string   `json:"method"`
    Description string   `json:"description"`
    ContentType []string `json:"content_type"`
}

// TaskRequest representa una solicitud de tarea
type TaskRequest struct {
    Action      string                 `json:"action"`
    Parameters  map[string]interface{} `json:"parameters"`
    Context     map[string]interface{} `json:"context,omitempty"`
    Priority    int                    `json:"priority,omitempty"`
    Timeout     *time.Duration         `json:"timeout,omitempty"`
}

// TaskResponse representa una respuesta de tarea
type TaskResponse struct {
    Status  TaskStatus             `json:"status"`
    Result  interface{}            `json:"result,omitempty"`
    Error   *ErrorDetails          `json:"error,omitempty"`
    Metrics TaskMetrics            `json:"metrics,omitempty"`
}

// TaskStatus define el estado de una tarea
type TaskStatus string

const (
    TaskStatusPending   TaskStatus = "pending"
    TaskStatusRunning   TaskStatus = "running"
    TaskStatusCompleted TaskStatus = "completed"
    TaskStatusFailed    TaskStatus = "failed"
    TaskStatusCancelled TaskStatus = "cancelled"
)

// TaskMetrics contiene métricas de ejecución
type TaskMetrics struct {
    StartTime    time.Time     `json:"start_time"`
    EndTime      time.Time     `json:"end_time"`
    Duration     time.Duration `json:"duration"`
    TokensUsed   int           `json:"tokens_used,omitempty"`
    Cost         float64       `json:"cost,omitempty"`
}

// ErrorDetails proporciona información detallada del error
type ErrorDetails struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Details interface{} `json:"details,omitempty"`
}

// StreamChunk representa un fragmento de respuesta en streaming
type StreamChunk struct {
    TaskID    string      `json:"task_id"`
    Sequence  int         `json:"sequence"`
    Data      interface{} `json:"data"`
    Done      bool        `json:"done"`
    Timestamp time.Time   `json:"timestamp"`
}
```

**Archivo:** `internal/acp/server/server.go`

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

// Server es el servidor ACP
type Server struct {
    addr     string
    router   *mux.Router
    agents   map[string]*AgentHandler
    mu       sync.RWMutex
    server   *http.Server
}

// AgentHandler maneja las solicitudes para un agente específico
type AgentHandler struct {
    manifest protocol.AgentManifest
    executor TaskExecutor
}

// TaskExecutor ejecuta tareas del agente
type TaskExecutor interface {
    Execute(ctx context.Context, req protocol.TaskRequest) (*protocol.TaskResponse, error)
    Stream(ctx context.Context, req protocol.TaskRequest) (<-chan protocol.StreamChunk, error)
}

// NewServer crea un nuevo servidor ACP
func NewServer(addr string) *Server {
    router := mux.NewRouter()
    
    s := &Server{
        addr:   addr,
        router: router,
        agents: make(map[string]*AgentHandler),
    }
    
    // Rutas del protocolo ACP
    router.HandleFunc("/acp/v1/discover", s.handleDiscover).Methods("GET")
    router.HandleFunc("/acp/v1/agents", s.handleListAgents).Methods("GET")
    router.HandleFunc("/acp/v1/agents/{agent}", s.handleAgentManifest).Methods("GET")
    router.HandleFunc("/acp/v1/agents/{agent}/task", s.handleTask).Methods("POST")
    router.HandleFunc("/acp/v1/agents/{agent}/stream", s.handleStream).Methods("GET")
    
    return s
}

// RegisterAgent registra un nuevo agente en el servidor
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

// Start inicia el servidor
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

// handleDiscover maneja la solicitud de descubrimiento
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

// handleListAgents lista todos los agentes
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

// handleAgentManifest devuelve el manifiesto de un agente
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

// handleTask maneja una solicitud de tarea
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

// handleStream maneja una solicitud de streaming
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
    
    // Configurar SSE
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

**Archivo:** `internal/acp/client/client.go`

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

// Client es el cliente ACP
type Client struct {
    baseURL    string
    httpClient *http.Client
}

// NewClient crea un nuevo cliente ACP
func NewClient(baseURL string) *Client {
    return &Client{
        baseURL:    baseURL,
        httpClient: &http.Client{},
    }
}

// Discover descubre agentes disponibles
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

// SendTask envía una tarea a un agente
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

// StreamTask envía una tarea y recibe la respuesta en streaming
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

### 3. TUI Multi-Agente

**Archivo:** `internal/tui/multiagent/view.go`

```go
package multiagent

import (
    "fmt"
    
    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
    "github.com/digiogithub/opencode/internal/acp/protocol"
)

// Model es el modelo para la vista multi-agente
type Model struct {
    agents       []AgentPanel
    activeAgent  int
    width        int
    height       int
    layout       Layout
}

// AgentPanel representa el panel de un agente
type AgentPanel struct {
    ID       protocol.AgentID
    Manifest protocol.AgentManifest
    Messages []Message
    Status   AgentStatus
    Active   bool
}

// AgentStatus representa el estado de un agente
type AgentStatus string

const (
    AgentStatusIdle    AgentStatus = "idle"
    AgentStatusBusy    AgentStatus = "busy"
    AgentStatusError   AgentStatus = "error"
    AgentStatusOffline AgentStatus = "offline"
)

// Message representa un mensaje en el panel del agente
type Message struct {
    Role      string
    Content   string
    Timestamp string
}

// Layout define el diseño de los paneles
type Layout string

const (
    LayoutGrid       Layout = "grid"       // Cuadrícula
    LayoutHorizontal Layout = "horizontal" // Horizontal
    LayoutVertical   Layout = "vertical"   // Vertical
    LayoutFocus      Layout = "focus"      // Un agente en foco grande
)

// NewModel crea un nuevo modelo multi-agente
func NewModel() Model {
    return Model{
        agents:      make([]AgentPanel, 0),
        activeAgent: 0,
        layout:      LayoutGrid,
    }
}

// Init inicializa el modelo
func (m Model) Init() tea.Cmd {
    return nil
}

// Update actualiza el modelo
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "ctrl+c", "q":
            return m, tea.Quit
            
        case "tab":
            // Cambiar al siguiente agente
            m.activeAgent = (m.activeAgent + 1) % len(m.agents)
            return m, nil
            
        case "shift+tab":
            // Cambiar al agente anterior
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
        // Añadir mensaje al agente
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
        // Actualizar estado del agente
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

// View renderiza la vista
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

// gridView renderiza la vista en cuadrícula
func (m Model) gridView() string {
    if len(m.agents) == 0 {
        return ""
    }
    
    // Calcular dimensiones de la cuadrícula
    cols := 2
    if len(m.agents) == 1 {
        cols = 1
    }
    rows := (len(m.agents) + cols - 1) / cols
    
    panelWidth := m.width / cols
    panelHeight := (m.height - 3) / rows // -3 para barra de estado
    
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

// horizontalView renderiza la vista horizontal
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

// verticalView renderiza la vista vertical
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

// focusView renderiza la vista enfocada
func (m Model) focusView() string {
    if m.activeAgent >= len(m.agents) {
        return ""
    }
    
    agent := m.agents[m.activeAgent]
    panel := renderAgentPanel(agent, m.width, m.height-3, true)
    statusBar := renderStatusBar(m)
    
    return lipgloss.JoinVertical(lipgloss.Left, panel, statusBar)
}

// renderAgentPanel renderiza un panel de agente
func renderAgentPanel(agent AgentPanel, width, height int, active bool) string {
    // Estilos
    borderColor := lipgloss.Color("240")
    if active {
        borderColor = lipgloss.Color("86") // Verde
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
        statusColor = lipgloss.Color("86") // Verde
    case AgentStatusBusy:
        statusColor = lipgloss.Color("226") // Amarillo
    case AgentStatusError:
        statusColor = lipgloss.Color("196") // Rojo
    case AgentStatusOffline:
        statusColor = lipgloss.Color("240") // Gris
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

// renderStatusBar renderiza la barra de estado
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

// noAgentsView renderiza la vista cuando no hay agentes
func noAgentsView(width, height int) string {
    style := lipgloss.NewStyle().
        Width(width).
        Height(height).
        AlignHorizontal(lipgloss.Center).
        AlignVertical(lipgloss.Center)
    
    return style.Render("No agents connected.\n\nPress 'q' to quit.")
}

// Mensajes personalizados

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

## Plan de Implementación

### Fase 1: Actualización del Sistema de Proveedores (1-2 semanas)

**Tareas:**
1. ✅ Crear interfaz unificada `Provider` en `internal/llm/provider/provider.go`
2. ✅ Implementar proveedor Anthropic actualizado
3. ✅ Implementar proveedor OpenAI actualizado
4. ✅ Implementar proveedor Google Gemini
5. ✅ Implementar proveedor Groq
6. ✅ Implementar proveedor OpenRouter
7. ✅ Implementar proveedor Vercel AI Gateway
8. ✅ Crear sistema de registro de proveedores
9. ✅ Actualizar configuración para soportar múltiples proveedores
10. ✅ Pruebas unitarias para cada proveedor

**Archivos a crear/modificar:**
- `internal/llm/provider/provider.go` (nuevo)
- `internal/llm/provider/anthropic.go` (actualizar)
- `internal/llm/provider/openai.go` (actualizar)
- `internal/llm/provider/gemini.go` (actualizar)
- `internal/llm/provider/groq.go` (nuevo)
- `internal/llm/provider/openrouter.go` (nuevo)
- `internal/llm/provider/vercel.go` (nuevo)
- `internal/llm/provider/registry.go` (nuevo)
- `pkg/config/config.go` (actualizar)

### Fase 2: Implementación del Protocolo ACP (2-3 semanas)

**Tareas:**
1. ✅ Definir tipos del protocolo ACP en `internal/acp/protocol/`
2. ✅ Implementar servidor ACP
3. ✅ Implementar cliente ACP
4. ✅ Crear wrapper de agente OpenCode
5. ✅ Implementar descubrimiento de agentes
6. ✅ Implementar comunicación síncrona
7. ✅ Implementar comunicación asíncrona
8. ✅ Implementar streaming SSE
9. ✅ Pruebas de integración

**Archivos a crear:**
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

### Fase 3: TUI Multi-Agente (2-3 semanas)

**Tareas:**
1. ✅ Diseñar arquitectura de la vista multi-agente
2. ✅ Implementar modelo Bubble Tea para multi-agente
3. ✅ Implementar vista en cuadrícula
4. ✅ Implementar vista horizontal
5. ✅ Implementar vista vertical
6. ✅ Implementar vista enfocada
7. ✅ Implementar componente de panel de agente
8. ✅ Implementar barra de estado
9. ✅ Integrar con cliente ACP
10. ✅ Añadir hotkeys para navegación
11. ✅ Pruebas de interfaz

**Archivos a crear:**
- `internal/tui/multiagent/view.go`
- `internal/tui/multiagent/model.go`
- `internal/tui/multiagent/panel.go`
- `internal/tui/multiagent/layout.go`
- `internal/tui/components/agent_card.go`
- `internal/tui/components/status_bar.go`
- `internal/tui/components/chat_view.go`

### Fase 4: Integración y Pruebas (1-2 semanas)

**Tareas:**
1. ✅ Integrar sistema de proveedores con TUI
2. ✅ Integrar servidor ACP con agentes
3. ✅ Crear comandos CLI para gestionar agentes
4. ✅ Implementar persistencia de sesiones multi-agente
5. ✅ Documentación de uso
6. ✅ Ejemplos de configuración
7. ✅ Pruebas end-to-end
8. ✅ Optimización de rendimiento

---

## Configuración de Ejemplo

**Archivo:** `~/.opencode.json`

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

## Uso de la Aplicación

### Iniciar Servidor ACP

```bash
# Iniciar servidor ACP en segundo plano
opencode agent-server --config ~/.opencode.json --port 8080

# O como demonio
opencode agent-server --daemon
```

### Iniciar Cliente Multi-Agente

```bash
# Iniciar TUI con todos los agentes configurados
opencode multi

# Iniciar TUI conectándose a servidor remoto
opencode multi --server http://remote:8080

# Iniciar con layout específico
opencode multi --layout grid
```

### Comandos en TUI

- `Tab` / `Shift+Tab`: Navegar entre agentes
- `1`: Vista en cuadrícula
- `2`: Vista horizontal
- `3`: Vista vertical
- `4`: Vista enfocada (agente activo a pantalla completa)
- `Ctrl+N`: Nuevo mensaje al agente activo
- `Ctrl+S`: Cambiar proveedor del agente activo
- `Ctrl+D`: Desconectar agente
- `Ctrl+R`: Reconectar agente
- `Q`: Salir

### Conectar Agentes Externos

```bash
# Descubrir agentes disponibles
opencode agent discover --server http://localhost:8080

# Conectar agente externo
opencode agent connect --name external-agent --url http://external:9000
```

---

## Ventajas de la Arquitectura Propuesta

### 1. Flexibilidad de Proveedores
- ✅ Soporte para múltiples proveedores simultáneos
- ✅ Cambio dinámico de proveedor por agente
- ✅ Optimización de costes usando modelos apropiados por tarea

### 2. Interoperabilidad (ACP)
- ✅ Comunicación estándar entre agentes
- ✅ Independiente del framework (LangChain, CrewAI, etc.)
- ✅ Descubrimiento automático de agentes
- ✅ Soporte para agentes remotos

### 3. Experiencia de Usuario
- ✅ Visualización de múltiples agentes en tiempo real
- ✅ Múltiples layouts adaptables
- ✅ Interfaz familiar (Bubble Tea)
- ✅ Hotkeys intuitivos

### 4. Escalabilidad
- ✅ Arquitectura modular
- ✅ Fácil añadir nuevos proveedores
- ✅ Fácil añadir nuevos agentes
- ✅ Soporte para agentes distribuidos

### 5. Open Source & Comunidad
- ✅ Mantiene espíritu de OpenCode original
- ✅ Protocolo ACP estándar abierto (Linux Foundation)
- ✅ Compatible con ecosistema existente

---

## Dependencias Go

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

## Próximos Pasos

1. **Fork y Setup**
   - Clonar tu fork
   - Crear rama `feature/multi-agent-acp`
   - Setup estructura de carpetas

2. **Desarrollo Incremental**
   - Fase 1: Proveedores (mantener funcionalidad actual)
   - Fase 2: ACP (añadir capacidad de comunicación)
   - Fase 3: TUI Multi-Agente (nueva interfaz)

3. **Testing Continuo**
   - Pruebas unitarias por componente
   - Pruebas de integración entre fases
   - Testing manual del TUI

4. **Documentación**
   - README actualizado
   - Guías de uso
   - Ejemplos de configuración
   - API documentation para ACP

---

## Recursos Adicionales

### Referencias ACP
- Especificación: https://agentcommunicationprotocol.dev
- BeeAI Framework: https://github.com/i-am-bee/bee-agent-framework
- IBM Research: https://research.ibm.com/projects/agent-communication-protocol

### Referencias Crush
- Repo: https://github.com/charmbracelet/crush
- Docs: https://github.com/charmbracelet/crush/tree/main/docs

### Referencias Bubble Tea
- Repo: https://github.com/charmbracelet/bubbletea
- Tutorial: https://github.com/charmbracelet/bubbletea/tree/master/tutorials
- Examples: https://github.com/charmbracelet/bubbletea/tree/master/examples

---

## Contacto y Soporte

Para dudas sobre la implementación:
- Issues en el fork de GitHub
- Discusiones en el repo
- Documentación inline en el código

---

**Este plan proporciona una hoja de ruta completa para modernizar tu fork de OpenCode con capacidades multi-agente y comunicación ACP, manteniendo la esencia del proyecto original mientras añade características de nivel empresarial.**
