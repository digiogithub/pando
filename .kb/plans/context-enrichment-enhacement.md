## Cómo funciona hoy el context enrichment

El flujo actual es muy directo:

- En `internal/app/app.go`, si `cfg.Remembrances.ContextEnrichmentEnabled` está activo:
  - se crea `rag.NewContextEnricher(...)`
  - se inyecta en el agent con `agent.SetContextEnricher(enricher)`

- En `internal/llm/agent/agent.go`, antes de enviar el prompt al LLM:
  - si `globalContextEnricher != nil`
  - hace `globalContextEnricher.EnrichContext(ctx, content)`
  - y concatena el resultado al prompt original

- En `internal/rag/enricher.go`, `EnrichContext`:
  - lanza en paralelo:
    - `searchKB(ctx, query)`
    - `searchEvents(ctx, query)`
    - `searchCode(ctx, query)`
  - usa **el prompt original tal cual** como query en las tres APIs
  - filtra solo por `minScore`
  - formatea los resultados completos en un bloque `<context source="remembrances">`

## Problemas del diseño actual

Veo varios motivos por los que los resultados se alejan del prompt real y gastan tokens:

### 1. La misma query cruda se reutiliza para todo
Ahora mismo el sistema pasa el **prompt del usuario sin transformar** a:

- KB search
- events search
- code hybrid search

Eso funciona mal cuando el prompt contiene:
- instrucciones operativas: “haz”, “analiza”, “cambia”
- contexto conversacional irrelevante
- varios objetivos mezclados
- texto largo con ruido

Ejemplo típico:
- prompt: _“revisa cómo funciona el context enrichment de pando con remembrances cuando está activo...”_
- query enviada a Remembrances: exactamente eso

Para búsqueda semántica, esa query no es óptima. Mezcla:
- intención de acción
- dominio
- condición
- petición secundaria

Eso degrada recall/precision.

---

### 2. No hay estrategia distinta por fuente
Las tres fuentes usan el mismo query shape, pero cada una necesita algo diferente:

- **KB**: mejor con consulta conceptual resumida
- **Code**: mejor con términos técnicos, símbolos, subsistemas, nombres de archivo/feature
- **Events**: mejor con área/proyecto/tema y quizá ventana temporal o subject específico

Hoy no hay especialización.

---

### 3. El filtro solo usa `minScore`
Aunque exista `minScore`, eso no basta porque:

- si el embedding/search backend devuelve resultados “medianamente parecidos”, entran
- no hay reranking cruzado
- no hay deduplicación semántica
- no hay límite de presupuesto por sección más allá del número de resultados
- no hay compresión/selección posterior

Resultado: entra contexto “válido” desde el punto de vista del motor, pero poco útil para resolver el prompt.

---

### 4. Se inyecta demasiado texto bruto
`searchKB`, `searchEvents`, `searchCode` hacen truncado simple, pero siguen metiendo:
- chunks literales
- eventos largos
- source snippets de hasta 400 chars por símbolo

Eso puede consumir muchos tokens aunque el resultado sea marginal.

---

### 5. No hay reescritura de query ni selección contextual
Falta una fase intermedia del tipo:
- “¿Qué busca realmente el usuario?”
- “¿Qué conceptos/símbolos conviene usar para cada backend?”
- “¿Qué resultados merecen entrar en el prompt final?”

Ese es justo el gap que notas.

---

## Qué mejoraría en las consultas directas a Remembrances

Yo lo dividiría en dos niveles: **mejoras sin modelo** y **mejoras con subagente/modelo dedicado**.

---

# 1) Mejoras sin modelo adicional

Estas son baratas, seguras y útiles incluso si no añades subagente.

## A. Reescritura heurística mínima de la query
Antes de consultar Remembrances, derivar desde el prompt original:

- `rawQuery`: prompt original
- `semanticQuery`: prompt limpiado para búsqueda semántica
- `codeQuery`: query enfocada a código
- `eventsQuery`: query enfocada a historial/proyecto

Ejemplo de transformación:
- quitar verbos de ejecución genéricos: “haz”, “analiza”, “revisa”
- quitar frases meta: “quiero que”, “por favor”
- preservar nombres técnicos:
  - `context enrichment`
  - `remembrances`
  - `pando`
  - `subagente`
  - `summary`
  - `title`
  - `modelo fallback`

Incluso una limpieza simple ya mejoraría bastante.

---

## B. Estrategia por fuente

### KB
Usar `semanticQuery`, más corta y conceptual.

### Code
Usar `codeQuery`, priorizando:
- nombres de feature
- nombres exactos de config
- símbolos o conceptos técnicos

En este caso, algo como:
- `"context enrichment remembrances ContextEnricher summary title fallback model"`

sería mejor que el prompt completo.

### Events
Usar una query más breve y contextual:
- `"context enrichment remembrances"`
o incluso
- `"context enricher"`

Y si el subject está vacío, considerar un default más restrictivo para este caso (`project`), porque los eventos generales tienden a meter ruido.

---

## C. Budget por sección y budget total
Además de `kbResults/codeResults/eventsResults`, añadir límites de salida:
- `ContextEnrichmentKBMaxChars`
- `ContextEnrichmentCodeMaxChars`
- `ContextEnrichmentEventsMaxChars`
- `ContextEnrichmentMaxChars`

Porque el problema no es solo cuántos resultados entran, sino **cuánto texto final consumen**.

---

## D. Deduplicación y selección posterior
Tras recoger resultados:
- deduplicar paths/símbolos repetidos
- si un resultado de code ya cubre algo muy concreto, bajar peso de KB/eventos redundantes
- priorizar:
  1. code
  2. KB
  3. events  
  para prompts claramente de ingeniería/código

Hoy el orden fijo no es el problema principal, pero sí la falta de priorización por tipo de tarea.

---

## E. Compresión mejor del contexto
En vez de meter chunks casi crudos:
- KB: título/path + 1-2 frases clave
- Events: 1 línea de por qué ese evento es relevante
- Code: símbolo + archivo + docstring breve, y solo source si aporta algo

Es decir: pasar de “retrieval dump” a “retrieval summary”.

---

## F. Ajustar defaults
Los defaults actuales:
- KB 3
- Code 5
- Events 3
- minScore 0.45

Para evitar ruido, yo probaría:
- `KBResults = 2`
- `CodeResults = 3`
- `EventsResults = 2`
- `MinScore = 0.55` o `0.60`

Sobre todo mientras no haya reranking.

---

# 2) Mejoras con modelo/subagente dedicado

Esta parte encaja muy bien con tu idea.

## Viabilidad arquitectónica

Sí, es viable y además el proyecto **ya tiene el patrón**.

En `internal/llm/agent/agent.go`:

- para títulos:
  - `titleProvider`
  - creado con `createAgentProvider(..., config.AgentTitle, ...)`

- para summaries:
  - `summarizeProvider`
  - `summarizeFallbackProvider`
  - y en `sendSummaryRequest(...)`:
    - primero usa `summarizeProvider`
    - si falla, hace fallback a `summarizeFallbackProvider` (el coder)

Esto es exactamente el patrón que propones:
- modelo especializado
- fallback al coder si falla

## Qué propondría

Añadir un agente/modelo nuevo, por ejemplo:

- `AgentContextEnricher`
o
- `AgentRetriever`

con configuración propia en `config`.

### Config sugerida
En `internal/config/config.go` y defaults:

- `AgentContextEnricher AgentName = "context-enricher"`

Y en config:
- `agents.context-enricher.model`
- `agents.context-enricher.max_tokens`

Más una sección Remembrances:
- `ContextEnrichmentUseAgent bool`
- `ContextEnrichmentAgentFallbackToCoder bool`

Opcionalmente:
- `ContextEnrichmentAgentEnabled bool`

---

## Qué haría ese subagente/modelo

No debería hacer todo el trabajo de responder. Solo esto:

### Fase 1: query planning
Dado el prompt original, produce algo estructurado como:

```json
{
  "intent": "analyze current context enrichment and propose improvements",
  "kb_query": "context enrichment remembrances pando",
  "code_query": "ContextEnricher remembrances context enrichment summary title fallback model",
  "events_query": "context enrichment remembrances",
  "preferred_sources": ["code", "kb", "events"],
  "keywords": ["ContextEnricher", "SetContextEnricher", "AgentSummarizer", "AgentTitle", "fallback"],
  "max_results": {
    "kb": 2,
    "code": 3,
    "events": 2
  }
}
```

### Fase 2: retrieval
El sistema ejecuta las búsquedas reales contra Remembrances con esas queries.

### Fase 3: contextual compression
El mismo subagente, o una función local simple, resume lo encontrado en un bloque corto:
- qué es relevante
- por qué
- qué ignorar

### Fase 4: inject
Se inyecta un contexto compacto, no el dump bruto.

---

## Dos diseños posibles

## Opción A — subagente solo para reescribir queries
Más simple y robusta.

Flujo:
1. prompt original
2. modelo de context-enrichment genera `kb_query`, `code_query`, `events_query`
3. Go ejecuta búsquedas
4. Go formatea contexto

### Ventajas
- implementación más fácil
- menor riesgo
- el modelo no necesita acceso a tools
- fallback sencillo
- controlas tú el retrieval

### Desventaja
- la compresión final sigue siendo relativamente tonta si no la mejoras

**Esta es la opción que haría primero.**

---

## Opción B — subagente también resume el retrieval
Flujo:
1. prompt original
2. modelo genera queries
3. Go hace retrieval
4. modelo recibe resultados brutos
5. devuelve “contexto final recomendado”

### Ventajas
- muchísimo mejor control del ruido
- mejor relación relevancia/tokens

### Desventajas
- más latencia
- más coste
- más complejidad
- más puntos de fallo

Yo la dejaría como fase 2.

---

# Cómo encaja con el patrón de summary/title

Perfectamente.

## Patrón existente reutilizable
Hoy ya existe esto para summary:

- `summarizeProvider`
- `summarizeFallbackProvider`
- `sendSummaryRequest(...)`
- si falla el modelo dedicado:
  - retry con el coder

Esto se puede replicar para enrichment:

- `contextEnrichmentProvider`
- `contextEnrichmentFallbackProvider`

y una función tipo:

- `planContextEnrichmentQuery(...)`
o
- `buildEnrichmentPlan(...)`

con lógica:

1. usar `contextEnrichmentProvider`
2. si falla:
   - log warning
   - usar fallback coder
3. si también falla:
   - degradar al modo heurístico/directo actual

Eso te da **triple resiliencia**:
- modelo dedicado
- fallback coder
- fallback heurístico sin modelo

Muy buen diseño.

---

# Diseño concreto recomendado

## Nuevo componente
En vez de meterlo todo en `rag.ContextEnricher`, yo separaría:

- `ContextEnricher` = orquestador
- `QueryPlanner` = genera consultas
- `ContextFormatter` = comprime/sintetiza salida

### Interfaces sugeridas
Algo como:

```go
type EnrichmentQueryPlanner interface {
    Plan(ctx context.Context, prompt string) (*EnrichmentPlan, error)
}
```

```go
type EnrichmentPlan struct {
    Intent           string
    KBQuery          string
    CodeQuery        string
    EventsQuery      string
    PreferredSources []string
    KBResults        int
    CodeResults      int
    EventsResults    int
    Keywords         []string
}
```

Luego `ContextEnricher.EnrichContext(...)` haría:

1. `plan := planner.Plan(...)`
2. si falla → fallback planner
3. ejecutar búsquedas
4. filtrar
5. resumir
6. devolver bloque `<context>`

---

## Planner implementations

### 1. Heuristic planner
Sin LLM, siempre disponible.

### 2. LLM planner
Usa provider dedicado:
- `config.AgentContextEnricher`
- fallback al coder

---

# Qué habría que tocar en código

## 1. Config
### `internal/config/config.go`
Añadir:
- `AgentContextEnricher`
- defaults de modelo por provider
- budgets apropiados

Y en `RemembrancesConfig`, nuevos flags tipo:
- `ContextEnrichmentUseQueryPlanner`
- `ContextEnrichmentPlannerAgentEnabled`
- `ContextEnrichmentPlannerFallbackToCoder`

Opcional:
- `ContextEnrichmentMaxChars`
- `ContextEnrichmentUseCompression`

---

## 2. Agent
### `internal/llm/agent/agent.go`
Si quieres seguir el patrón de summary/title dentro del agent principal:
- añadir `contextEnrichmentPlannerProvider`
- añadir `contextEnrichmentPlannerFallbackProvider`

Pero sinceramente, para esta feature **mejor no acoplarlo al agent principal**, porque el enrichment sucede antes del prompt final y pertenece más a `rag` o a una capa de preparación de contexto.

Yo movería la lógica del provider dedicado a un componente del paquete `rag` o `llm/context`.

---

## 3. RAG
### `internal/rag/enricher.go`
Refactorizar a:
- usar plan derivado
- no usar el prompt raw directamente para todo
- aplicar budgets y compresión

---

## 4. Prompt/template
Opcionalmente, podrías marcar el contexto con una estructura más explícita:

```xml
<context source="remembrances">
  <intent>...</intent>
  <retrieval_summary>...</retrieval_summary>
  <code>...</code>
  <kb>...</kb>
  <events>...</events>
</context>
```

Eso suele ayudar al modelo final a usar mejor el contexto.

---

# Mejoras concretas de retrieval que yo haría primero

Si tuviera que priorizar, haría esto:

## Fase 1 — mejora inmediata, bajo riesgo
1. Añadir planner heurístico
2. Query distinta por fuente
3. Subir `minScore`
4. Reducir defaults de results
5. Añadir budget por chars
6. Compactar mejor salida

Con esto ya deberías notar mucha menos basura.

## Fase 2 — modelo dedicado con fallback
1. Nuevo agente `context-enricher`
2. Planner con LLM
3. Fallback al coder
4. Si falla todo, planner heurístico

## Fase 3 — compresión asistida por LLM
1. retrieval con queries reescritas
2. resumen final del contexto por el modelo dedicated/fallback

---

# Riesgos y consideraciones

## Latencia
Cada llamada extra a modelo añade latencia antes de responder.
Mitigaciones:
- planner muy corto, max tokens bajo
- modelo rápido/barato
- timeout corto
- fallback inmediato

## Coste
Si activas planner en cada prompt, sube coste.
Mitigaciones:
- solo cuando prompt supere cierto umbral de complejidad
- o cuando haya Remembrances activos y suficientes fuentes
- o cachear plan por turno

## Fragilidad
Si el planner devuelve JSON malformado:
- fallback al coder
- si vuelve a fallar, fallback heurístico

## Sobreingeniería
No conviene lanzar un subagente completo tipo Mesnada para esto si basta con un provider dedicado.
La analogía con `summary/title` sugiere mejor:
- **provider específico**, no un subproceso/orchestrated agent completo

A menos que quieras explícitamente usar herramientas desde ese subagente, lo cual no parece necesario en la primera versión.

---

# Mi valoración final

## Sí, las consultas directas a Remembrances se pueden mejorar claramente
Principalmente porque hoy:
- usan el prompt completo sin reescritura
- no distinguen por fuente
- no hacen reranking/compresión real
- inyectan demasiado texto bruto

## Sí, se puede añadir un modelo/subagente específico con fallback al coder
Y además el proyecto ya tiene el patrón implementado en `summarizer`, que es la mejor referencia.

## Recomendación de diseño
La mejor solución práctica sería:

1. **No consultar Remembrances con el prompt raw directamente**
2. Introducir un **planner de enrichment**
3. Ese planner puede ser:
   - heurístico por defecto
   - o LLM-based con modelo dedicado
4. Si el modelo dedicado falla:
   - fallback al coder
5. Si el coder también falla:
   - fallback heurístico
6. Mantener budgets estrictos y compresión del contexto final

---

Si quieres, el siguiente paso puedo hacerlo en dos formatos:

1. **análisis técnico más aterrizado a código**, con propuesta de structs, interfaces y archivos exactos a modificar  
2. **implementación directa** de una primera versión mínima:
   - planner heurístico
   - queries por fuente
   - fallback path preparado para luego añadir el modelo dedicado

## User

Realiza un plan de implementación de la fase 1 y la fase 2 indicadas, además un nuevo selector en ACP para activar o desactivar el context enrichment manualmente, también a través de un comando en tui y en webuiguarda el plan de implementación que generes en un documento en kb

## User

continua generando el plan de implementación
