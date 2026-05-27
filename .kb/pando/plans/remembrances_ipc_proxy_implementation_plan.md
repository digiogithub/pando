# Plan de Implementación: Proxy IPC para Escrituras de Remembrances (KB, Events, Code Indexer)

**Fecha**: 2026-05-27  
**Estado**: Listo para ejecución  
**Proyecto**: pando  
**Autor**: Pando Agent  

---

## 1. Antecedentes y Diagnóstico

Actualmente, Pando implementa un modelo de único escritor (single-writer) para SQLite donde:
- La instancia **primaria** abre el archivo `pando.db` en modo de lectura y escritura (`rw`) y tiene el control exclusivo de las transacciones mutadoras.
- Las instancias **secundarias** abren la base de datos en modo solo lectura (`ro`) y redirigen las escrituras del `db.Querier` tradicional a través de llamadas RPC ZMQ (`db.write`) hacia la primaria, que las ejecuta de forma serializada usando el `writecoordinator`.

Sin embargo, las operaciones de **Remembrances** (Knowledge Base, Events e Indexación de Código) se ejecutan mediante SQL directo con `*sql.DB` (`BeginTx`, `ExecContext`) en lugar del `db.Querier` generado por sqlc. 
Esto causa fallas críticas (`database is readonly`) en instancias secundarias (por ejemplo, sub-agentes ejecutados en paralelo bajo el motor `engine=pando` o procesos de fondo de secondaries) cuando intentan:
1. Añadir/Actualizar/Borrar documentos en la base de conocimientos (`KBStore`).
2. Registrar eventos de sesión o decisiones (`EventStore`).
3. Registrar/Actualizar el estado de indexación de código (`CodeIndexer`).

### Desafío de Diseño
El despachador de red `dbproxy.dispatchWrite` solo posee acceso al objeto `db.Querier`. No tiene acceso al `RemembrancesService` ni a sus sub-stores locales. 
Para resolver esto de forma elegante y conforme con la directriz del usuario (*"seguramente implique una implementación de clase para este uso diferente por lo que no tiene porqué ampliarse el original si complica o exige más modificaciones"*), diseñaremos un **desacoplamiento modular** por medio de una interfaz despachadora opcional.

---

## 2. Arquitectura de la Solución

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

## 3. Plan de Implementación por Fases

### Fase 1: Abstracción e Interfaz en `dbproxy`

Para evitar dependencias circulares entre el paquete `dbproxy` (infraestructura de red IPC) y los paquetes de `rag` (lógica de negocio), introduciremos una interfaz y un registro dinámico en `internal/ipc/dbproxy`.

1. **Definir la interfaz `RemembrancesDispatcher`** en `internal/ipc/dbproxy/proxy.go` o un archivo nuevo `internal/ipc/dbproxy/remembrances.go`:
   ```go
   package dbproxy

   import (
       "context"
       "encoding/json"
   )

   // RemembrancesDispatcher define el contrato para despachar escrituras de RAG
   // en la instancia primaria.
   type RemembrancesDispatcher interface {
       DispatchRemembrancesWrite(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
   }

   var remembrancesDispatcher RemembrancesDispatcher

   // RegisterRemembrancesDispatcher registra el despachador de RAG en la primaria.
   func RegisterRemembrancesDispatcher(d RemembrancesDispatcher) {
       remembrancesDispatcher = d
   }
   ```

2. **Modificar `dispatchWrite`** en `internal/ipc/dbproxy/handlers.go` para usar este despachador en el bloque `default`:
   ```go
   // ... dentro de dispatchWrite ...
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

### Fase 2: Métodos de Escritura de Bajo Nivel (Primary-Side Stores)

Cuando la secundaria genera un documento o evento, calcula el chunking y los embeddings vectoriales localmente (usando las API keys del cliente) para no sobrecargar de CPU a la primaria. El payload de RPC ya viaja con vectores y chunks precomputados.
Necesitamos asegurarnos de que las tiendas locales de la primaria puedan insertar estos datos directamente sin re-calcularlos.

1. **`KBStore` (`internal/rag/kb/kb.go`)**:
   - `AddDocument` y `UpdateDocument` ya admiten flujos a través del proxy.
   - Implementar un método local en la primaria (o extraer el flujo interno de base de datos) que acepte chunks y embeddings precomputados:
     ```go
     func (s *KBStore) AddDocumentWithEmbeddings(ctx context.Context, filePath, content string, metadata map[string]interface{}, chunks []string, embeddings [][]float32) error
     ```
   - Este método encapsulará la transacción SQL actual (`INSERT INTO kb_documents`, `INSERT INTO kb_chunks`, `INSERT INTO kb_fts`) evitando llamar a `s.embedder.EmbedDocuments`.

2. **`EventStore` (`internal/rag/events/events.go`)**:
   - Agregar un método de bajo nivel que reciba el embedding ya generado:
     ```go
     func (s *EventStore) SaveEventWithEmbedding(ctx context.Context, subject, content string, metadata map[string]interface{}, embedding []float32) (int64, error)
     ```
   - Este método realiza el `INSERT INTO events` y el `INSERT INTO events_fts` sin volver a invocar a `s.embedder.EmbedQuery`.

3. **`CodeIndexer` (`internal/rag/code/indexer.go`)**:
   - Adaptar las inserciones transaccionales de proyectos (`code_projects`) y archivos (`code_files` / `code_symbols`) para que puedan ser gatilladas de forma limpia en la primaria a partir de structs serializados de deserialización RPC.

---

### Fase 3: Implementación de la Clase `RemembrancesWriteDispatcher`

Crearemos un módulo despachador dedicado que actuará como el "puente de clase para este uso diferente" sugerido por el usuario.

1. **Crear el archivo `internal/rag/proxy/dispatcher.go`** o `internal/rag/dispatcher.go`:
   ```go
   package proxy // o dentro del paquete rag

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
           var req kbAddDocumentRequest // Definido en kb.go o accesible
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
           // Eliminar previo e insertar nuevo
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
           var req saveEventRequest // Definido en events.go o accesible
           if err := json.Unmarshal(params, &req); err != nil {
               return nil, err
           }
           id, err := d.svc.Events.SaveEventWithEmbedding(ctx, req.Subject, req.Content, req.Metadata, req.Embedding)
           if err != nil {
               return nil, err
           }
           return json.Marshal(id)

       case "CodeUpsertProject":
           // Lógica de deserialización y llamada directa a c.Code.UpsertProjectLocal(...)
           // ...
       
       case "CodeSetProjectStatus":
           // Lógica de deserialización y actualización directa en code_projects
           // ...

       case "CodeIndexFile":
           // Lógica de deserialización de codeIndexFileRequest y ejecución de transacciones
           // ...

       case "CodeDeleteProject":
           // Lógica de borrado local en la primaria
           // ...

       default:
           return nil, fmt.Errorf("unsupported remembrances write method: %s", method)
       }
   }
   ```

---

### Fase 4: Registro e Integración de Ciclo de Vida (`internal/app/app.go`)

1. **Resolver el error de compilación actual en `internal/app/app.go`** asegurando la importación correcta de `"github.com/digiogithub/pando/internal/ipc/dbproxy"`.
2. **Registrar el despachador en la primaria**:
   Dentro de `SetupIPC` de `App` en `internal/app/app.go`:
   ```go
   func (app *App) SetupIPC(bus *ipc.Bus) {
       app.IPCBus = bus
       app.IPCIsPrimary = true
       session.SetIPCPublisher(bus)

       // REGISTRO DEL DISPATCHER DE REMEMBRANCES
       if app.Remembrances != nil {
           dispatcher := ragproxy.NewRemembrancesWriteDispatcher(app.Remembrances)
           dbproxy.RegisterRemembrancesDispatcher(dispatcher)
           logging.Info("Remembrances IPC write dispatcher registered successfully on primary")
       }
       logging.Info("IPC bus wired to session service", "pubAddr", bus.PubAddr, "rpcAddr", bus.RPCAddr)
   }
   ```

---

### Fase 5: Pruebas y Validación

1. **Prueba unitaria del despachador (`internal/ipc/dbproxy/handlers_test.go`)**:
   - Crear un test que registre un despachador ficticio (`mockDispatcher`) y verifique que `dbproxy.DispatchWrite` de forma exitosa delega las peticiones no resueltas a dicho despachador.
2. **Prueba de integración extremo a extremo**:
   - Ejecutar una simulación con un cliente secundario de `RemembrancesService` configurado con un proxy IPC ficticio que apunte a un bus con el despachador real de la primaria y verificar la persistencia correcta en la base de datos de destino.

---

## 4. Mitigación de Riesgos y Buenas Prácticas

1. **Transacciones Atómicas**: Asegurar que toda operación de despachado en la primaria abra y complete su propia transacción en SQLite de manera atómica para evitar estados inconsistentes (especialmente FTS desalineados).
2. **Timeouts Adecuados**: Indexar código o procesar documentos KB de gran tamaño puede tardar más tiempo del límite predeterminado. Usar el canal `WriteWithRetry` con `dbproxy.DefaultWriteTimeouts.Long` (30 segundos) garantiza tolerancia a picos de latencia de red de IPC.
3. **No Redundancia**: El chunking y embeddings deben completarse de forma estricta en el nodo originador (secundario), evitando dobles llamadas costosas de red externa en la primaria.
