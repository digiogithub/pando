# Plan de Implementación: Session RAG Storage para Pando

> **Proyecto**: Pando
> **Fecha**: 2026-03-15
> **Fact IDs en Remembrances**: `session_rag_fase1_schema_store`, `session_rag_fase2_indexing`, `session_rag_fase3_search_integration`, `session_rag_fase4_tools_lifecycle`

---

## Resumen Ejecutivo

Implementar almacenamiento de sesiones de conversación con RAG (Retrieval-Augmented Generation) usando embeddings y chunks, de forma análoga al sistema de KB (`internal/rag/kb/`) de remembrances pero **exclusivamente en base de datos** (sin generar ficheros .md). Las sesiones se almacenan como chunks con embeddings en SQLite, permitiendo búsqueda semántica posterior a través de la tool de KB existente, que devolverá tanto chunks de documentos KB como de sesiones.

### Diferencias clave con KB

| Aspecto | KB (actual) | Session RAG (nuevo) |
|---------|------------|---------------------|
| Origen datos | Contenido externo (markdown, docs) | Conversaciones internas (mensajes user/assistant) |
| Almacenamiento | DB + ficheros .md opcionales | Solo DB (no ficheros) |
| Chunking | Por contenido del documento | Por mensajes/turnos de conversación |
| Metadata | file_path, user metadata | session_id, role, timestamp, model |
| Lifecycle | Manual (add/delete) | Automático (al finalizar sesión/turno) |
| Búsqueda | kb_search_documents tool | Integrada en kb_search_documents (unified) |

## Estado Actual del Código

### Sessions (`internal/session/`)
- `Session`: ID, ParentSessionID, Title, MessageCount, PromptTokens, CompletionTokens, Cost, CreatedAt
- `Service`: Create, Get, List, Save, Delete — CRUD sobre SQLite via sqlc
- DB table `sessions`: id, parent_session_id, title, message_count, prompt_tokens, completion_tokens, cost, updated_at, created_at, summary_message_id

### Messages (`internal/message/`)
- `Message`: ID, SessionID, Role, Parts []ContentPart, Model, CreatedAt, UpdatedAt
- Parts types: TextContent, ReasoningContent, ToolCall, ToolResult, Finish, BinaryContent, ImageURLContent
- DB table `messages`: id, session_id, role, parts (JSON), model, created_at, updated_at, finished_at

### KB Store (`internal/rag/kb/`)
- `KBStore`: db, embedder, chunkSize, chunkOverlap
- Tables: kb_documents (id, file_path, content, metadata, timestamps), kb_chunks (id, document_id, chunk_index, content, embedding BLOB), kb_fts (FTS5)
- `SearchDocuments(ctx, query, limit)` — hybrid search: vector similarity + FTS5, RRF fusion

### RAG Service (`internal/rag/service.go`)
- `RemembrancesService`: KB, Events, Code, docEmbedder, codeEmbedder
- Compartido: embedder, SQLite DB connection

### Agent (`internal/llm/agent/agent.go`)
- `streamAndHandleEvents()`: procesa stream del LLM, ejecuta tools, almacena mensajes
- Hook `hook_agent_response_finish` ya se ejecuta al completar respuesta — punto ideal para trigger de indexación

## Arquitectura

```
┌──────────────────────────────────────────────────┐
│            Session Complete / Turn End             │
│  agent.go → hook_agent_response_finish            │
└──────────────────────┬───────────────────────────┘
                       │ async goroutine
┌──────────────────────▼───────────────────────────┐
│          SessionRAGIndexer                        │
│  - ExtractConversationText(session, messages)     │
│  - ChunkConversation(text, metadata)              │
│  - EmbedAndStore(chunks) → SQLite                 │
└──────────────────────┬───────────────────────────┘
                       │ writes to
┌──────────────────────▼───────────────────────────┐
│  SQLite Tables (new)                              │
│  session_rag_documents: session_id, content, meta │
│  session_rag_chunks: doc_id, chunk, embedding     │
│  session_rag_fts: FTS5 index                      │
└──────────────────────┬───────────────────────────┘
                       │ searched via
┌──────────────────────▼───────────────────────────┐
│  Unified KB Search                                │
│  kb_search_documents ahora busca en:              │
│  - kb_chunks (documentos KB)                      │
│  - session_rag_chunks (sesiones)                  │
│  Ambos resultados fusionados con RRF              │
└──────────────────────────────────────────────────┘
```

---

## Fase 1: Schema & Session Store (fact: session_rag_fase1_schema_store)

**Prioridad**: Crítica (bloquea todo)
**Esfuerzo**: Medio
**Archivos**:
- `internal/rag/sessions/types.go` (nuevo)
- `internal/rag/sessions/sessions.go` (nuevo)
- Migración SQLite (new tables)

### Tareas:

1. **Crear tablas SQLite** (via migración o init):
   ```sql
   CREATE TABLE IF NOT EXISTS session_rag_documents (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       session_id TEXT NOT NULL,
       title TEXT NOT NULL DEFAULT '',
       content TEXT NOT NULL,
       metadata TEXT NOT NULL DEFAULT '{}',
       message_count INTEGER NOT NULL DEFAULT 0,
       turn_count INTEGER NOT NULL DEFAULT 0,
       model TEXT DEFAULT '',
       created_at DATETIME NOT NULL,
       updated_at DATETIME NOT NULL,
       UNIQUE(session_id)
   );
   
   CREATE TABLE IF NOT EXISTS session_rag_chunks (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       document_id INTEGER NOT NULL REFERENCES session_rag_documents(id),
       chunk_index INTEGER NOT NULL,
       content TEXT NOT NULL,
       embedding BLOB,
       role TEXT DEFAULT '',        -- 'user', 'assistant', 'mixed'
       turn_start INTEGER DEFAULT 0, -- message index of turn start
       turn_end INTEGER DEFAULT 0,   -- message index of turn end  
       created_at DATETIME NOT NULL
   );
   
   CREATE VIRTUAL TABLE IF NOT EXISTS session_rag_fts USING fts5(
       content,
       content_rowid='id',
       tokenize='porter unicode61'
   );
   
   CREATE INDEX IF NOT EXISTS idx_session_rag_docs_session ON session_rag_documents(session_id);
   CREATE INDEX IF NOT EXISTS idx_session_rag_chunks_doc ON session_rag_chunks(document_id);
   ```

2. **Crear tipos** en `internal/rag/sessions/types.go`:
   ```go
   type SessionDocument struct {
       ID           int64
       SessionID    string
       Title        string
       Content      string
       Metadata     map[string]interface{}
       MessageCount int
       TurnCount    int
       Model        string
       CreatedAt    time.Time
       UpdatedAt    time.Time
   }
   
   type SessionChunk struct {
       ID          int64
       DocumentID  int64
       ChunkIndex  int
       Content     string
       Embedding   []float32
       Role        string // "user", "assistant", "mixed"
       TurnStart   int
       TurnEnd     int
       CreatedAt   time.Time
   }
   
   type SessionSearchResult struct {
       SessionID  string
       Title      string
       Content    string
       Role       string
       Similarity float64
       Score      float64
       Source     string // "session_rag"
   }
   ```

3. **Crear `SessionRAGStore`** en `internal/rag/sessions/sessions.go`:
   ```go
   type SessionRAGStore struct {
       db           *sql.DB
       embedder     embeddings.Embedder
       chunkSize    int
       chunkOverlap int
   }
   
   func NewSessionRAGStore(db *sql.DB, embedder embeddings.Embedder, chunkSize, chunkOverlap int) *SessionRAGStore
   func (s *SessionRAGStore) InitTables(ctx context.Context) error
   func (s *SessionRAGStore) IndexSession(ctx context.Context, sessionID, title, content string, metadata map[string]interface{}, messageCount, turnCount int, model string) error
   func (s *SessionRAGStore) DeleteSession(ctx context.Context, sessionID string) error
   func (s *SessionRAGStore) SearchSessions(ctx context.Context, query string, limit int) ([]SessionSearchResult, error)
   func (s *SessionRAGStore) GetSessionDocument(ctx context.Context, sessionID string) (*SessionDocument, error)
   ```

### Chunking Strategy para Sesiones:
- Concatenar mensajes en formato: `[user]: texto\n[assistant]: texto\n`
- Chunk por turnos completos (user+assistant), no por caracteres arbitrarios
- Si un turno es muy largo, sub-chunk con overlap
- Metadata por chunk: role predominante, rango de turnos

---

## Fase 2: Conversation Indexer (fact: session_rag_fase2_indexing)

**Prioridad**: Alta
**Esfuerzo**: Medio
**Depende de**: Fase 1
**Archivos**:
- `internal/rag/sessions/indexer.go` (nuevo)
- `internal/rag/service.go` (extender)

### Tareas:

1. **Crear `SessionIndexer`** en `internal/rag/sessions/indexer.go`:
   ```go
   type SessionIndexer struct {
       store    *SessionRAGStore
       messages message.Service
   }
   
   func (idx *SessionIndexer) IndexSession(ctx context.Context, sessionID string) error {
       // 1. Obtener mensajes de la sesión
       // 2. Filtrar: solo TextContent y ReasoningContent (ignorar ToolCall, ToolResult, Binary, Finish)
       // 3. Formatear como conversación legible
       // 4. Obtener session metadata (title, model, etc)
       // 5. Llamar store.IndexSession()
   }
   
   func (idx *SessionIndexer) ExtractConversationText(msgs []message.Message) string {
       // Concatena mensajes user/assistant como texto plano
       // Formato: "[user]: ...\n[assistant]: ...\n"
       // Ignora tool calls/results pero incluye su resumen
       // Trunca mensajes muy largos (>2000 chars) con "...[truncated]"
   }
   ```

2. **Extender `RemembrancesService`** en `service.go`:
   ```go
   type RemembrancesService struct {
       KB           *kb.KBStore
       Events       *events.EventStore
       Code         *code.CodeIndexer
       Sessions     *sessions.SessionRAGStore  // NEW
       SessionIdx   *sessions.SessionIndexer   // NEW
       docEmbedder  embeddings.Embedder
       codeEmbedder embeddings.Embedder
   }
   ```
   - Inicializar SessionRAGStore y SessionIndexer en `NewRemembrancesService()`
   - Llamar `Sessions.InitTables()` al inicio

3. **Configuración** en `RemembrancesConfig`:
   ```go
   type RemembrancesConfig struct {
       // ... campos existentes ...
       SessionRAGEnabled     bool `json:"session_rag_enabled" toml:"SessionRAGEnabled"`
       SessionRAGAutoIndex   bool `json:"session_rag_auto_index" toml:"SessionRAGAutoIndex"`   // auto-index al cerrar sesión
       SessionMinMessages    int  `json:"session_min_messages" toml:"SessionMinMessages"`       // mínimo mensajes para indexar (default: 4)
   }
   ```

### Cuándo indexar:
- **Opción A (recomendada)**: Al finalizar una respuesta completa del agente (hook_agent_response_finish), en async goroutine — indexa incrementalmente
- **Opción B**: Al cerrar/cambiar sesión — indexa toda la sesión de una vez
- **Opción C**: Ambas — incremental en cada turno + re-index completo al cerrar
- Para el MVP, usar **Opción B**: más simple, indexar sesión completa al cambiar/cerrar

---

## Fase 3: Unified Search Integration (fact: session_rag_fase3_search_integration)

**Prioridad**: Alta
**Esfuerzo**: Medio
**Depende de**: Fase 2
**Archivos**:
- `internal/rag/kb/kb.go` (extender SearchDocuments)
- `internal/rag/sessions/sessions.go` (SearchSessions ya implementado en Fase 1)
- `internal/llm/tools/remembrances_kb.go` (modificar tool)

### Tareas:

1. **Extender KBStore con búsqueda unificada** o crear servicio wrapper:
   
   Opción elegida: **Crear `UnifiedSearcher`** que combine resultados:
   ```go
   // internal/rag/search.go (extender existente)
   type UnifiedSearcher struct {
       kb       *kb.KBStore
       sessions *sessions.SessionRAGStore
   }
   
   type UnifiedSearchResult struct {
       Source     string  // "kb" | "session"
       Content    string
       FilePath   string  // para KB
       SessionID  string  // para sessions
       Title      string
       Score      float64
       Similarity float64
       Metadata   map[string]interface{}
   }
   
   func (u *UnifiedSearcher) Search(ctx context.Context, query string, limit int, sources ...string) ([]UnifiedSearchResult, error) {
       // Si sources vacío o contiene "all": buscar en ambos
       // Buscar en KB y Sessions en paralelo (goroutines)
       // Fusionar resultados con RRF (Reciprocal Rank Fusion)
       // Retornar top-N unificados
   }
   ```

2. **Modificar `KBSearchDocumentsTool`** en `remembrances_kb.go`:
   - Añadir parámetro opcional `source` (string): "kb", "sessions", "all" (default: "all")
   - Si el RemembrancesService tiene Sessions habilitado, usar UnifiedSearcher
   - Si no, comportamiento actual
   - Los resultados incluyen indicador de source en la respuesta

3. **Formato de respuesta** para resultados de sesión:
   ```json
   {
     "source": "session",
     "session_id": "abc-123",
     "title": "Implement login feature", 
     "content": "[user]: How do I add JWT auth?\n[assistant]: You can use...",
     "score": 0.87,
     "turn_range": "3-5"
   }
   ```

### RRF Fusion:
- Usar el mismo algoritmo RRF que ya existe en `internal/rag/search.go`
- Ponderar: KB results weight = 1.0, Session results weight = 0.8 (sessions menos prioritarias por defecto)
- Configurable via search params

---

## Fase 4: Tools, Lifecycle & Cleanup (fact: session_rag_fase4_tools_lifecycle)

**Prioridad**: Media
**Esfuerzo**: Medio
**Depende de**: Fase 3
**Archivos**:
- `internal/llm/tools/remembrances_sessions.go` (nuevo)
- `internal/llm/agent/tools.go` (registrar nuevas tools)
- `internal/llm/agent/agent.go` (trigger indexación)

### Tareas:

1. **Crear tools dedicadas para session RAG**:
   - `session_rag_search` — Búsqueda semántica en sesiones anteriores
     - Params: query (string), limit (int, default 5)
     - Usa SessionRAGStore.SearchSessions()
   - `session_rag_index` — Forzar indexación de sesión actual
     - Params: session_id (string, optional — default current)
   - `session_rag_delete` — Eliminar indexación de una sesión
     - Params: session_id (string)

2. **Auto-indexación lifecycle**:
   - Subscribirse a `session.Service` pubsub events
   - On session change/close: indexar sesión anterior si tiene >= SessionMinMessages
   - Async goroutine para no bloquear UI
   ```go
   // En la inicialización de la app
   sessions.Subscribe(func(event pubsub.Event, session Session) {
       if event == pubsub.UpdatedEvent && remembrances.Sessions != nil {
           go func() {
               ctx := context.Background()
               if err := remembrances.SessionIdx.IndexSession(ctx, session.ID); err != nil {
                   logging.Error("Failed to index session", "session_id", session.ID, "error", err)
               }
           }()
       }
   })
   ```

3. **Cleanup & TTL**:
   - Opcionalmente, borrar session RAG documents de sesiones eliminadas
   - Subscribirse a session delete events
   - Config: `session_rag_ttl_days` (0 = sin expiración)

4. **Registrar tools en el agent** (`internal/llm/agent/tools.go`):
   ```go
   if remembrances != nil && remembrances.Sessions != nil {
       baseTools = append(baseTools,
           tools.NewSessionRAGSearchTool(remembrances.Sessions),
           tools.NewSessionRAGIndexTool(remembrances.SessionIdx),
           tools.NewSessionRAGDeleteTool(remembrances.Sessions),
       )
   }
   ```

---

## Consideraciones

### Performance
- Embeddings generation es la operación más costosa — siempre async
- Indexar solo sesiones con >= N mensajes (configurable, default 4)
- Limitar chunks por sesión (max ~50 chunks por sesión)
- Reusar el mismo embedder que KB para no duplicar conexiones

### Privacidad
- Las sesiones contienen código y conversaciones del usuario — todo local en SQLite
- No se transmite nada externo más allá de las llamadas al embedder (que puede ser local con Ollama)
- El usuario puede eliminar sesiones indexadas

### Consistencia con KB
- Mismo formato de embedding (float32 BLOB)
- Mismo motor FTS5
- Misma estrategia de hybrid search (vector + FTS + RRF)
- Diferente: chunking por turnos vs por caracteres

## Dependencias

- No se necesitan nuevas dependencias Go
- Reutiliza toda la infraestructura RAG existente (embeddings, chunking, search)
- Reutiliza el servicio de mensajes existente para obtener contenido

## Testing

- Unit tests: `internal/rag/sessions/sessions_test.go` — CRUD, search, chunking
- Unit tests: `internal/rag/sessions/indexer_test.go` — extracción texto, chunking por turnos
- Integration test: crear sesión con mensajes → indexar → buscar → verificar resultados
- Integration test: búsqueda unificada KB + sessions
