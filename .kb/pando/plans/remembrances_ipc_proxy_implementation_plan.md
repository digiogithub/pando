# Implementation Plan: IPC Proxy for Remembrances Writes (KB, Events, Code Indexer)

**Date**: 2026-05-27  
**Status**: Ready for execution  
**Project**: pando  
**Author**: Pando Agent  

---

## 1. Background and Diagnosis

Currently, Pando implements a single-writer model for SQLite where:
- The **primary** instance opens the `pando.db` file in read-write mode (`rw`) and has exclusive control over mutating transactions.
- **Secondary** instances open the database in read-only mode (`ro`) and redirect writes from the traditional `db.Querier` through ZMQ RPC calls (`db.write`) to the primary, which executes them serialized using the `writecoordinator`.

However, **Remembrances** operations (Knowledge Base, Events, and Code Indexing) execute direct SQL with `*sql.DB` (`BeginTx`, `ExecContext`) instead of the sqlc-generated `db.Querier`. 
This causes critical failures (`database is readonly`) in secondary instances (e.g., sub-agents running in parallel under the `engine=pando` engine or secondary background processes) when they try to:
1. Add/Update/Delete documents in the knowledge base (`KBStore`).
2. Record session events or decisions (`EventStore`).
3. Record/Update code indexing state (`CodeIndexer`).

### Design Challenge
The network dispatcher `dbproxy.dispatchWrite` only has access to the `db.Querier` object. It has no access to the `RemembrancesService` or its local sub-stores. 
To solve this elegantly and in accordance with the user's directive (*"it surely involves a class implementation for this different use so the original doesn't need to be expanded if it complicates or requires more modifications"*), we will design a **modular decoupling** through an optional dispatcher interface.

---

## 2. Solution Architecture

```
[Secondary Instance (RO SQL)]
   │
   ├─► KBStore/EventStore/CodeIndexer (mutating writes)
   │      │
   │      └─► dbproxy.DBProxy (WriteWithRetry / ProxyWriteWithResult)
   │             │
   │             └─► ZMQ RPC ("db.write", Method: "KBAddDocument", params)
   ▼
[Primary Instance (RW SQL)]
   │
   └─► ZMQ RPC Bus ("db.write")
         │
         └─► writecoordinator.Coordinator (Serializes writes)
                │
                └─► dbproxy.DispatchWrite()
                       │
                       ├─► Match sqlc write method (e.g. "CreateSession") -> db.Querier
                       │
                       └─► Method not matched -> Delegated to:
                             └─► RemembrancesDispatcher (Interface hook)
                                    │
                                    └─► RemembrancesWriteDispatcher (Primary Server-side class)
                                           │
                                           ├─► KBStore.AddDocumentPrecomputed(...)
                                           ├─► EventStore.SaveEventPrecomputed(...)
                                           └─► CodeIndexer.InsertFilePrecomputed(...)
```

---

## 3. Phased Implementation Plan

### Phase 1: Abstraction and Interface in `dbproxy`

To avoid circular dependencies between the `dbproxy` package (IPC network infrastructure) and the `rag` packages (business logic), we will introduce an interface and dynamic registry in `internal/ipc/dbproxy`.

1. **Define the `RemembrancesDispatcher` interface** in `internal/ipc/dbproxy/proxy.go` or a new file `internal/ipc/dbproxy/remembrances.go`:
    ```go
    package dbproxy

    import (
        "context"
        "encoding/json"
    )

    // RemembrancesDispatcher defines the contract for dispatching RAG writes
    // on the primary instance.
    type RemembrancesDispatcher interface {
        DispatchRemembrancesWrite(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
    }

    var remembrancesDispatcher RemembrancesDispatcher

    // RegisterRemembrancesDispatcher registers the RAG dispatcher on the primary.
    func RegisterRemembrancesDispatcher(d RemembrancesDispatcher) {
        remembrancesDispatcher = d
    }
    ```

2. **Modify `dispatchWrite`** in `internal/ipc/dbproxy/handlers.go` to use this dispatcher in the `default` block:
    ```go
    // ... inside dispatchWrite ...
    default:
        if remembrancesDispatcher != nil {
            return remembrancesDispatcher.DispatchRemembrancesWrite(ctx, req.Method, req.Params)
        }
        return nil, &WriteError{
            Code:    ErrCodeMethodNotFound,
            Method:  req.Method,
            Message: fmt.Sprintf("unknown write method %q", req.Method),
        }
    ```

---

### Phase 2: Low-Level Write Methods (Primary-Side Stores)

When the secondary generates a document or event, it computes chunking and vector embeddings locally (using client API keys) to avoid overloading the primary's CPU. The RPC payload already travels with precomputed vectors and chunks.
We need to ensure the primary's local stores can insert this data directly without re-computing it.

1. **`KBStore` (`internal/rag/kb/kb.go`)**:
    - `AddDocument` and `UpdateDocument` already support flows through the proxy.
    - Implement a local method on the primary (or extract the internal database flow) that accepts precomputed chunks and embeddings:
      ```go
      func (s *KBStore) AddDocumentWithEmbeddings(ctx context.Context, filePath, content string, metadata map[string]interface{}, chunks []string, embeddings [][]float32) error
      ```
    - This method will encapsulate the current SQL transaction (`INSERT INTO kb_documents`, `INSERT INTO kb_chunks`, `INSERT INTO kb_fts`) avoiding calls to `s.embedder.EmbedDocuments`.

2. **`EventStore` (`internal/rag/events/events.go`)**:
    - Add a low-level method that receives the already-generated embedding:
      ```go
      func (s *EventStore) SaveEventWithEmbedding(ctx context.Context, subject, content string, metadata map[string]interface{}, embedding []float32) (int64, error)
      ```
    - This method performs the `INSERT INTO events` and `INSERT INTO events_fts` without re-invoking `s.embedder.EmbedQuery`.

3. **`CodeIndexer` (`internal/rag/code/indexer.go`)**:
    - Adapt transactional insertions for projects (`code_projects`) and files (`code_files` / `code_symbols`) so they can be cleanly triggered on the primary from deserialized RPC structs.

---

### Phase 3: `RemembrancesWriteDispatcher` Class Implementation

We will create a dedicated dispatcher module that will act as the "class bridge for this different use" suggested by the user.

1. **Create the file `internal/rag/proxy/dispatcher.go`** or `internal/rag/dispatcher.go`:
    ```go
    package proxy // or inside the rag package

    import (
        "context"
        "encoding/json"
        "fmt"
        "github.com/digiogithub/pando/internal/rag"
        "github.com/digiogithub/pando/internal/ipc/dbproxy"
    )

    type RemembrancesWriteDispatcher struct {
        svc *rag.RemembrancesService
    }

    func NewRemembrancesWriteDispatcher(svc *rag.RemembrancesService) *RemembrancesWriteDispatcher {
        return &RemembrancesWriteDispatcher{svc: svc}
    }

    func (d *RemembrancesWriteDispatcher) DispatchRemembrancesWrite(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
        switch method {
        case "KBAddDocument":
            var req kbAddDocumentRequest // Defined in kb.go or accessible
            if err := json.Unmarshal(params, &req); err != nil {
                return nil, err
            }
            err := d.svc.KB.AddDocumentWithEmbeddings(ctx, req.FilePath, req.Content, req.Metadata, req.Chunks, req.Embeddings)
            return nil, err

        case "KBUpdateDocument":
            var req kbAddDocumentRequest
            if err := json.Unmarshal(params, &req); err != nil {
                return nil, err
            }
            // Delete previous and insert new
            _ = d.svc.KB.DeleteDocument(ctx, req.FilePath)
            err := d.svc.KB.AddDocumentWithEmbeddings(ctx, req.FilePath, req.Content, req.Metadata, req.Chunks, req.Embeddings)
            return nil, err

        case "KBDeleteDocument":
            var filePath string
            if err := json.Unmarshal(params, &filePath); err != nil {
                return nil, err
            }
            err := d.svc.KB.DeleteDocument(ctx, filePath)
            return nil, err

        case "SaveEvent":
            var req saveEventRequest // Defined in events.go or accessible
            if err := json.Unmarshal(params, &req); err != nil {
                return nil, err
            }
            id, err := d.svc.Events.SaveEventWithEmbedding(ctx, req.Subject, req.Content, req.Metadata, req.Embedding)
            if err != nil {
                return nil, err
            }
            return json.Marshal(id)

        case "CodeUpsertProject":
            // Deserialization logic and direct call to c.Code.UpsertProjectLocal(...)
            // ...
        
        case "CodeSetProjectStatus":
            // Deserialization logic and direct update in code_projects
            // ...

        case "CodeIndexFile":
            // Deserialization logic for codeIndexFileRequest and transaction execution
            // ...

        case "CodeDeleteProject":
            // Local deletion logic on the primary
            // ...

        default:
            return nil, fmt.Errorf("unsupported remembrances write method: %s", method)
        }
    }
    ```

---

### Phase 4: Lifecycle Registration and Integration (`internal/app/app.go`)

1. **Resolve the current compilation error in `internal/app/app.go`** by ensuring the correct import of `"github.com/digiogithub/pando/internal/ipc/dbproxy"`.
2. **Register the dispatcher on the primary**:
   Inside `App.SetupIPC` in `internal/app/app.go`:
    ```go
    func (app *App) SetupIPC(bus *ipc.Bus) {
        app.IPCBus = bus
        app.IPCIsPrimary = true
        session.SetIPCPublisher(bus)

        // REMEMBRANCES DISPATCHER REGISTRATION
        if app.Remembrances != nil {
            dispatcher := ragproxy.NewRemembrancesWriteDispatcher(app.Remembrances)
            dbproxy.RegisterRemembrancesDispatcher(dispatcher)
            logging.Info("Remembrances IPC write dispatcher registered successfully on primary")
        }
        logging.Info("IPC bus wired to session service", "pubAddr", bus.PubAddr, "rpcAddr", bus.RPCAddr)
    }
    ```

---

### Phase 5: Tests and Validation

1. **Dispatcher unit test (`internal/ipc/dbproxy/handlers_test.go`)**:
    - Create a test that registers a mock dispatcher and verifies that `dbproxy.DispatchWrite` successfully delegates unresolved requests to that dispatcher.
2. **End-to-end integration test**:
    - Run a simulation with a secondary `RemembrancesService` client configured with a mock IPC proxy pointing to a bus with the primary's real dispatcher, and verify correct persistence in the target database.

---

## 4. Risk Mitigation and Best Practices

1. **Atomic Transactions**: Ensure that every dispatch operation on the primary opens and completes its own SQLite transaction atomically to prevent inconsistent states (especially misaligned FTS).
2. **Proper Timeouts**: Indexing code or processing large KB documents may take longer than the default limit. Using the `WriteWithRetry` channel with `dbproxy.DefaultWriteTimeouts.Long` (30 seconds) ensures tolerance for IPC network latency spikes.
3. **No Redundancy**: Chunking and embeddings must be completed strictly on the originating node (secondary), avoiding costly double calls to the external network on the primary.
