<img src="https://r2cdn.perplexity.ai/pplx-full-logo-primary-dark%402x.png" style="height:64px;margin-right:32px"/>

# en Claude Code cuando una tool devuelve más de 15k tokens o palabras lo vuelva a fichero que indica que se ha volcado a un fichero temporal para ser leído, también he visto que los ficheros largos genera ficheros temporales donde numera cada línea. puedes explicarme e investigar porque lo hace y como funciona ese mecanismo de optimización?

Aquí tienes una explicación detallada del mecanismo de optimización de contexto que usa Claude Code:

## El problema de fondo: el context window

Claude Code opera dentro de una ventana de contexto finita (200k tokens en los modelos estándar) [^1_1]. Cada vez que una herramienta devuelve datos —salida de un comando Bash, contenido de un archivo, resultado de una búsqueda— esos datos se consumen directamente del presupuesto de tokens de la sesión. Sin optimización, un solo `cat archivo_grande.log` podría colapsar el contexto antes de escribir una sola línea de código [^1_2].

## Límite de 2000 líneas en la herramienta `View`

La herramienta `View` (lectura de ficheros) tiene un **límite hard-coded de 2000 líneas por lectura** [^1_3]. Como se puede ver en el prompt interno de Claude Code, la descripción oficial de la herramienta dice literalmente:

> *"By default, reads up to 2000 lines starting from the beginning of the file."* [^1_4]

Este límite no es configurable por el usuario [^1_3]. Cuando Claude necesita leer un fichero más largo, lo hace en **chunks secuenciales** usando offsets de línea, leyendo rangos como líneas 1–2000, luego 2001–4000, etc.

## Por qué numera las líneas

Claude Code antepone números de línea al contenido de los ficheros leídos como mecanismo de **anclaje de precisión**. Esto sirve para dos cosas:

- **Ediciones quirúrgicas**: las herramientas `Edit` y `Replace` necesitan referencias exactas de línea para hacer cambios sin reescribir el fichero entero.
- **Reducir errores de localización**: sin numeración, Claude tiende a descontar líneas en hunks de diff, apuntando a la línea equivocada con ~40% de error; con numeración el acierto sube al ~85% [^1_5].


## Por qué vuelca outputs grandes a ficheros temporales

Cuando una tool (típicamente `Bash`) devuelve más de un umbral de tokens (~15k), Claude Code usa el mecanismo de **fichero temporal** en lugar de inyectar el contenido directamente en el contexto [^1_6]. La lógica es:

1. El output se escribe en un fichero temporal en disco (e.g. `/tmp/claude-output-XXXX`)
2. En el contexto solo se inserta un **mensaje de referencia** indicando la ruta del fichero
3. Claude puede luego leer selectivamente secciones del fichero con `View` + offsets, consumiendo solo los tokens realmente necesarios

Esto evita el problema descrito en issues de GitHub donde outputs masivos de herramientas (como trazas de compilador) causaban overflow del contexto directamente [^1_6][^1_7].

## El ciclo completo de gestión de contexto

| Situación | Comportamiento |
| :-- | :-- |
| Fichero ≤ 2000 líneas | Lo lee completo con numeración de líneas |
| Fichero > 2000 líneas | Lee en chunks de 2000 líneas con offsets |
| Tool output > ~15k tokens | Vuelca a fichero temporal, inserta referencia en contexto |
| Contexto al 75% de capacidad | Dispara **auto-compact** para resumir el historial [^1_2] |

## Implicación práctica para tus proyectos

Si trabajas con ficheros grandes en Go/Rust, Claude usará `grep`/`head`/`tail` vía Bash como alternativa más eficiente al `View` chunked, ya que permite extraer solo las líneas relevantes sin saturar el contexto [^1_8]. Para outputs de herramientas propias (MCP servers), si quieres evitar el volcado a temporal y que Claude vea el contenido directamente, mantén las respuestas bajo ese umbral de ~15k tokens o bien estructura el output con paginación explícita.
<span style="display:none">[^1_10][^1_11][^1_12][^1_13][^1_14][^1_15][^1_16][^1_17][^1_18][^1_19][^1_20][^1_21][^1_22][^1_23][^1_24][^1_25][^1_26][^1_27][^1_28][^1_29][^1_30][^1_9]</span>

<div align="center">⁂</div>

[^1_1]: https://www.datastudios.org/post/claude-long-context-window-and-handling-of-very-large-documents-context-size-practical-strategies

[^1_2]: https://hyperdev.matsuoka.com/p/how-claude-code-got-better-by-protecting

[^1_3]: https://www.reddit.com/r/ClaudeAI/comments/1lg17nc/how_to_allow_claude_code_to_read_large_files/

[^1_4]: https://kirshatrov.com/posts/claude-code-internals

[^1_5]: https://www.reddit.com/r/ClaudeAI/comments/1qfrb48/getting_claude_to_output_accurate_line_numbers_in/

[^1_6]: https://github.com/anthropics/claude-code/issues/805

[^1_7]: https://github.com/anthropics/claude-code/issues/12054

[^1_8]: https://www.reddit.com/r/ClaudeAI/comments/1lmxzhp/how_are_you_handling_projects_that_are_too_big/

[^1_9]: https://www.reddit.com/r/ClaudeAI/comments/1ohjjwq/claude_now_truncates_output_when_working_with/

[^1_10]: https://www.youtube.com/watch?v=TcTKbF6rWBw

[^1_11]: https://github.com/anthropics/claude-code/issues/14723

[^1_12]: https://www.reddit.com/r/ClaudeAI/comments/1n39xro/claude_code_truncates_long_maven_outputs_whats/

[^1_13]: https://platform.claude.com/docs/en/agents-and-tools/tool-use/implement-tool-use

[^1_14]: https://github.com/openai/codex/issues/5913

[^1_15]: https://github.com/anthropics/claude-code/issues/11348

[^1_16]: https://forum.langchain.com/t/filemanagement-toolkits-file-write-doesnt-work-with-claude-sonnet-3-7-4-for-slightly-big-files/1077

[^1_17]: https://www.reddit.com/r/ClaudeAI/comments/1lypm28/claude_codes_tiny_context_window_is_driving_me/

[^1_18]: https://github.com/anthropics/claude-code/issues/2638

[^1_19]: https://github.com/anthropics/claude-code/issues/10448

[^1_20]: https://code.claude.com/docs/en/settings

[^1_21]: https://sankalp.bearblog.dev/my-experience-with-claude-code-20-and-how-to-get-better-at-using-coding-agents/

[^1_22]: https://www.scribd.com/document/953839474/Claude-Code-CLI-Large-Document-Solutions

[^1_23]: https://www.datastudios.org/post/claude-ai-file-upload-and-reading-supported-formats-context-handling-structured-comprehension

[^1_24]: https://www.youtube.com/watch?v=S3SnmD0YEhU

[^1_25]: https://www.youtube.com/watch?v=5Khch9rlg4A

[^1_26]: https://code.claude.com/docs/en/cli-reference

[^1_27]: https://gist.github.com/bgauryy/0cdb9aa337d01ae5bd0c803943aa36bd

[^1_28]: https://www.producttalk.org/how-to-use-claude-code-safely/

[^1_29]: https://news.ycombinator.com/item?id=46098838

[^1_30]: https://github.com/anthropics/claude-code/issues/16130


---

# hay más reseñas de tools internas en el prompt del sistema de Claude Code que tengan detalles de optimización?

Sí. En esas filtraciones/gists del prompt interno hay **bastantes** pistas adicionales sobre optimización, no solo la del volcado a fichero temporal. Lo más claro es que Claude Code está diseñado para **ahorrar contexto, reducir exploración innecesaria y paralelizar trabajo** siempre que puede. [^2_1]

## Pistas claras

En el bloque de `Bash` aparece que, si la salida supera **30000 caracteres**, se trunca antes de devolverse al modelo, lo que confirma un límite explícito de protección de contexto en esa tool. [^2_1] También se insiste en no usar `cat`, `head`, `tail`, `grep`, `find`, `sed`, `awk` o `echo` cuando existan tools dedicadas, porque esas tools parecen estar más controladas y devuelven resultados más compactos y predecibles. [^2_1]

En `Read` se documenta otra optimización fuerte: por defecto lee solo **2000 líneas**, trunca líneas de más de **2000 caracteres**, y devuelve el contenido en formato `cat -n`, es decir, con numeración de líneas. [^2_1] Eso reduce el volumen bruto, permite lecturas parciales con `offset` y `limit`, y mejora la precisión posterior de `Edit` y `MultiEdit`, que dependen de coincidencias exactas. [^2_1]

## Búsqueda y exploración

Las tools `Glob` y `Grep` están descritas como herramientas “rápidas” que funcionan bien incluso con codebases grandes, y se recomienda usarlas en lugar de shell commands equivalentes. [^2_1] Además, en otra versión del prompt se empuja mucho el uso de `Task` o agentes de tipo `Explore` para búsquedas abiertas, con la razón explícita de **reducir uso de contexto** y delegar exploración iterativa fuera del hilo principal. [^2_1]

También hay instrucciones para hacer llamadas en **paralelo** cuando no haya dependencias entre ellas. [^2_1] Eso no ahorra tokens directamente, pero sí reduce rondas agente-herramienta y evita que el modelo tenga que “recordar” durante demasiados turnos búsquedas que podrían resolverse de una vez. [^2_1]

## Caché y background

`WebFetch` menciona una **caché autolimpiable de 15 minutos**, pensada para accesos repetidos al mismo recurso sin rehacer todo el trabajo. [^2_1] En la variante con `Task` y `TaskOutput`, también se ve otro patrón de optimización: permitir agentes en **background** con un `output_file`, para no meter toda la ejecución y su salida continuamente en el contexto principal. [^2_1]

Eso encaja muy bien con lo que viste de ficheros temporales: el sistema externaliza resultados grandes a disco y luego solo reincorpora fragmentos o resúmenes cuando hacen falta. [^2_1]

## Qué nos dice del diseño

El patrón general parece ser este: **contexto principal mínimo, acceso diferido al detalle, y herramientas especializadas con límites duros**. [^2_1] Por eso vemos chunking por líneas, truncado por caracteres, line numbering, agentes exploradores, ejecución en background, caché temporal y preferencia por tools estructuradas frente a Bash crudo. [^2_1]

Un ejemplo práctico: en vez de meter 50k líneas de log en el prompt, Claude Code prefiere guardar el output, enseñarle al modelo solo una referencia o una parte, y luego dejar que pida los rangos concretos que necesita. [^2_1]

## Lo menos seguro

Lo que **sí** parece bien respaldado por el prompt es el límite de `Read`, el truncado de `Bash`, la preferencia por tools específicas, el uso de agentes para exploración abierta y la cache de `WebFetch`. [^2_1] Lo que ya es más difícil de afirmar con certeza absoluta solo desde estas fuentes es el umbral exacto en **tokens** del volcado a fichero temporal, porque en los textos accesibles se ve mejor el límite en **caracteres** y la estrategia general que la implementación exacta interna. [^2_2][^2_1]

Si quieres, en el siguiente paso te hago una **tabla tool por tool** con todas las optimizaciones internas inferibles del prompt de Claude Code.
<span style="display:none">[^2_10][^2_11][^2_12][^2_13][^2_14][^2_15][^2_16][^2_17][^2_3][^2_4][^2_5][^2_6][^2_7][^2_8][^2_9]</span>

<div align="center">⁂</div>

[^2_1]: https://www.reddit.com/r/ClaudeAI/comments/1ohjjwq/claude_now_truncates_output_when_working_with/

[^2_2]: https://github.com/anthropics/claude-code/issues/805

[^2_3]: https://gist.github.com/chigkim/1f37bb2be98d97c952fd79cbb3efb1c6

[^2_4]: https://arize.com/blog/claude-md-best-practices-learned-from-optimizing-claude-code-with-prompt-learning/

[^2_5]: https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/claude-prompting-best-practices

[^2_6]: https://www.reddit.com/r/ClaudeAI/comments/1e39tvj/sonnet_35_coding_system_prompt_v2_with_explainer/

[^2_7]: https://www.reddit.com/r/ClaudeAI/comments/1o66m77/understanding_claude_codes_3_system_prompt/

[^2_8]: https://gist.github.com/wong2/e0f34aac66caf890a332f7b6f9e2ba8f

[^2_9]: https://simonwillison.net/2025/May/25/claude-4-system-prompt/

[^2_10]: https://mikhail.io/2025/09/sonnet-4-5-system-prompt-changes/

[^2_11]: https://weaxsey.org/en/articles/2025-10-12/

[^2_12]: https://code.claude.com/docs/en/best-practices

[^2_13]: https://generativeai.pub/mind-reading-claude-ais-complete-system-prompt-bb498276fe3d

[^2_14]: https://claudefa.st/blog/guide/mechanics/interactive-mode

[^2_15]: https://dejan.ai/blog/claude-system-internals/

[^2_16]: https://www.reddit.com/r/ClaudeAI/comments/1r9tmct/claude_code_works_because_of_bash_noncoding/

[^2_17]: https://www.anthropic.com/engineering/advanced-tool-use


---

# adelante, hazme una tabla

Sí; del prompt interno se pueden extraer varias optimizaciones bastante concretas. [^3_1]

## Tabla

| Tool / mecanismo | Detalle observado | Qué optimiza realmente | Señal de diseño |
| :-- | :-- | :-- | :-- |
| `Bash` | La salida se **trunca si supera 30000 caracteres**. [^3_1] | Evita que logs enormes, builds o volcados de comandos llenen el contexto de golpe. [^3_1] | Hay un límite duro de serialización de output. [^3_1] |
| `Bash` vs tools dedicadas | El prompt dice que se **evite** `find`, `grep`, `cat`, `head`, `tail`, `ls` y se usen `Glob`, `Grep`, `Read`, `LS`. [^3_1] | Reduce ruido, hace outputs más estructurados y más baratos de consumir por el modelo. [^3_1] | Priorizan herramientas con semántica controlada frente a shell libre. [^3_1] |
| `Read` | Lee por defecto solo **2000 líneas**, permite `offset` y `limit`. [^3_1] | Chunking nativo de ficheros largos para leer solo la porción necesaria. [^3_1] | Acceso incremental en vez de ingestión completa. [^3_1] |
| `Read` | Las líneas de más de **2000 caracteres** se truncan. [^3_1] | Protege contra minificados, JSON gigantes, blobs o líneas patológicas. [^3_1] | Hay defensas tanto por número de líneas como por longitud de línea. [^3_1] |
| `Read` | Devuelve en formato `cat -n`, con **números de línea**. [^3_1] | Facilita ediciones precisas y referencias robustas en `Edit`/`MultiEdit`. [^3_1] | El sistema está optimizado para localizar y parchear, no solo para leer. [^3_1] |
| `Edit` | Requiere haber hecho `Read` antes, y exige coincidencia exacta del texto. [^3_1] | Evita ediciones “a ciegas” y reduce errores de reemplazo por contexto insuficiente. [^3_1] | El flujo está pensado como read-then-patch. [^3_1] |
| `MultiEdit` | Varias ediciones en una sola operación, aplicadas en secuencia y de forma **atómica**. [^3_1] | Menos rondas tool/model y menos relecturas del mismo archivo. [^3_1] | Se intenta compactar cambios múltiples en una sola transacción. [^3_1] |
| `Glob` | Se define como búsqueda de patrones **rápida** que funciona con codebases de cualquier tamaño. [^3_1] | Encontrar candidatos por nombre sin recorrer manualmente el árbol con shell. [^3_1] | Búsqueda indexada o al menos más optimizada que Bash genérico. [^3_1] |
| `Grep` | Se define como búsqueda de contenido **rápida** sobre codebases grandes. [^3_1] | Descubrir archivos relevantes sin abrirlos todos ni llenar contexto. [^3_1] | Favorece recuperación de rutas antes que volcar contenido. [^3_1] |
| `Agent` / `Task` | Para búsquedas abiertas recomienda delegar a un agente aparte y además lanzarlos **concurrentemente**. [^3_1] | Saca exploración iterativa del hilo principal y reduce rondas de contexto local. [^3_1] | Externalización de trabajo exploratorio y paralelización. [^3_1] |
| `Agent` / `Task` | Cada agente es **stateless** y devuelve un único informe final. [^3_1] | Evita historiales largos por subagente y fuerza resultados compactos. [^3_1] | Diseño orientado a compresión de contexto por subtarea. [^3_1] |
| `WebFetch` | Puede **resumir** resultados si el contenido es muy grande. [^3_1] | No mete páginas enteras en contexto cuando basta una extracción guiada. [^3_1] | Lectura mediada por un modelo pequeño/rápido. [^3_1] |
| `WebFetch` | Tiene una **caché autolimpiable de 15 minutos**. [^3_1] | Evita recomputar fetch + parse + resumen en accesos repetidos. [^3_1] | Hay optimización temporal además de contextual. [^3_1] |
| `TodoRead` / `TodoWrite` | El prompt insiste en usarlos con mucha frecuencia. [^3_1] | Externaliza el plan y estado de trabajo fuera del texto libre conversacional. [^3_1] | Parte de la memoria operativa se saca a una estructura resumida. [^3_1] |
| Estilo de respuesta | El prompt ordena **minimizar tokens**, responder corto y evitar preámbulos. [^3_1] | Menor gasto de ventana de contexto en mensajes del propio modelo. [^3_1] | La optimización no solo está en tools; también en la política de salida. [^3_1] |

## Lectura técnica

El patrón común es que Claude Code intenta trabajar con **referencias, rangos, coincidencias y resúmenes**, no con blobs completos. [^3_1] Eso explica por qué ves truncado de outputs, lectura parcial, numeración de líneas, edición exacta y delegación a agentes para búsquedas amplias. [^3_1]

También se ve que hay dos niveles de optimización: uno de **I/O estructurado** (`Read`, `Glob`, `Grep`, `MultiEdit`, `WebFetch`) y otro de **disciplina conversacional** (respuestas cortas, agentes stateless, uso de todos para memoria operativa). [^3_1]

## Qué inferir con cautela

La tabla anterior recoge cosas que sí aparecen de forma bastante directa en el prompt publicado. [^3_1] Lo que ya entra más en inferencia es si detrás hay índices locales, memoria mmap, ficheros temporales por tool concreta o umbrales tokenizados exactos distintos de los límites por caracteres/líneas visibles en el prompt. [^3_1]

Si quieres, el siguiente paso te lo hago como **mapa de arquitectura interna inferida**: `Bash/Read -> truncado -> referencia temporal -> reread selectivo -> edit exacto`.

<div align="center">⁂</div>

[^3_1]: https://www.reddit.com/r/ClaudeAI/comments/1ohjjwq/claude_now_truncates_output_when_working_with/

