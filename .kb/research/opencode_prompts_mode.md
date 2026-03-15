Index your code with Devin

[DeepWiki](/)

Index your code with

Devin

Share

[sst/opencode](/sst/opencode)

Donde están las instrucciones del sistema para una conversación y que variantes dispone. Hace modificaciones adicionales sobre los prompts del usuario?

Fast

## Instrucciones del Sistema y Variantes

Las instrucciones del sistema para una conversación se construyen combinando múltiples componentes en `SessionPrompt.loop()` prompt.ts:652-656 :

1. **Instrucciones base**: `SystemPrompt.environment(model)` - instrucciones específicas del modelo
2. **Instrucciones personalizadas**: `InstructionPrompt.system()` - instrucciones configuradas por el usuario
3. **Instrucciones de formato**: Se añade `STRUCTURED_OUTPUT_SYSTEM_PROMPT` cuando se requiere salida JSON

### Variantes Disponibles

Los modelos soportan múltiples variantes con diferentes configuraciones models.mdx:138-149 :

- **Anthropic**: `high` (presupuesto de pensamiento alto, predeterminado), `max` (presupuesto máximo)
- **Variantes personalizadas**: Puedes definir variantes extendiendo las integradas con configuraciones como `reasoningEffort`, `textVerbosity`, `reasoningSummary`

Las variantes se pueden ciclar durante una sesión usando el keybind `variant_cycle` index.tsx:1131-1135 .

### Modificaciones Adicionales a los Prompts

OpenCode realiza varias modificaciones automáticas a los prompts del usuario:

1. **Recordatorios system-reminder**: Para mensajes pendientes en pasos &gt; 1, se envuelven con recordatorios para mantener el enfoque prompt.ts:630-647
2. **Transformación via plugins**: Se ejecuta `Plugin.trigger("experimental.chat.messages.transform")` permitiendo a los plugins modificar los mensajes prompt.ts:649
3. **Instrucciones de archivos**: Puedes configurar archivos de instrucciones adicionales vía la opción `instructions` en `opencode.json` config.mdx:552-564
4. **Herramienta StructuredOutput**: Se inyecta automáticamente cuando se habilita el modo `json_schema` prompt.ts:614-621

## Notes

Las instrucciones del sistema siguen una jerarquía de precedencia donde las configuraciones más específicas (nivel agente) sobrescriben las globales. Las variantes del modelo permiten ajustar comportamientos como el esfuerzo de razonamiento sin cambiar el modelo base.

Wiki pages you might want to explore:

- [Architecture Overview (sst/opencode)](/sst/opencode/1.2-architecture-overview)

[sst/opencode](https://github.com/sst/opencode)[packages/opencode/src/session/prompt.ts](https://github.com/sst/opencode/blob/715b844c/packages/opencode/src/session/prompt.ts#L614-L621)

```

611      })
612 
613      // Inject StructuredOutput tool if JSON schema mode enabled
```

```

614      if (lastUser.format?.type === "json_schema") {
615        tools["StructuredOutput"] = createStructuredOutputTool({
616          schema: lastUser.format.schema,
617          onSuccess(output) {
618            structuredOutput = output
619          },
620        })
621      }
```

```

622 
623      if (step === 1) {
624        SessionSummary.summarize({
```

```

627        })
628      }
629 
```

```

630      // Ephemerally wrap queued user messages with a reminder to stay on track
631      if (step > 1 && lastFinished) {
632        for (const msg of msgs) {
633          if (msg.info.role !== "user" || msg.info.id <= lastFinished.id) continue
634          for (const part of msg.parts) {
635            if (part.type !== "text" || part.ignored || part.synthetic) continue
636            if (!part.text.trim()) continue
637            part.text = [
638              "<system-reminder>",
639              "The user sent the following message:",
640              part.text,
641              "",
642              "Please address this message and continue with your tasks.",
643              "</system-reminder>",
644            ].join("\n")
645          }
646        }
647      }
```

```

648 
```

```

649      await Plugin.trigger("experimental.chat.messages.transform", {}, { messages: msgs })
```

```

650 
651      // Build system prompt, adding structured output instruction if needed
```

```

652      const system = [...(await SystemPrompt.environment(model)), ...(await InstructionPrompt.system())]
653      const format = lastUser.format ?? { type: "text" }
654      if (format.type === "json_schema") {
655        system.push(STRUCTURED_OUTPUT_SYSTEM_PROMPT)
656      }
```

```

657 
658      const result = await processor.process({
659        user: lastUser,
```

[sst/opencode](https://github.com/sst/opencode)[packages/web/src/content/docs/es/models.mdx](https://github.com/sst/opencode/blob/715b844c/packages/web/src/content/docs/es/models.mdx#L138-L149)

```

135
136---
137
```

```

138## Variantes
139
140Muchos modelos admiten múltiples variantes con diferentes configuraciones. OpenCode se envía con variantes predeterminadas integradas para proveedores populares.
141
142### Variantes integradas
143
144OpenCode se envía con variantes predeterminadas para muchos proveedores:
145
146**Anthropic**:
147
148- `high` - Presupuesto de pensamiento alto (predeterminado)
149- `max` - Presupuesto de pensamiento máximo
```

```

150
151**OpenAI**:
152
```

[sst/opencode](https://github.com/sst/opencode)[packages/opencode/src/cli/cmd/tui/component/prompt/index.tsx](https://github.com/sst/opencode/blob/715b844c/packages/opencode/src/cli/cmd/tui/component/prompt/index.tsx#L1131-L1135)

```

1128            <box gap={2} flexDirection="row">
1129              <Switch>
1130                <Match when={store.mode === "normal"}>
```

```

1131                  <Show when={local.model.variant.list().length > 0}>
1132                    <text fg={theme.text}>
1133                      {keybind.print("variant_cycle")} <span style={{ fg: theme.textMuted }}>variants</span>
1134                    </text>
1135                  </Show>
```

```

1136                  <text fg={theme.text}>
1137                    {keybind.print("agent_cycle")} <span style={{ fg: theme.textMuted }}>agents</span>
1138                  </text>
```

[sst/opencode](https://github.com/sst/opencode)[packages/web/src/content/docs/es/config.mdx](https://github.com/sst/opencode/blob/715b844c/packages/web/src/content/docs/es/config.mdx#L552-L564)

```

549
550---
551
```

````

552### Instrucciones
553
554Puedes configurar las instrucciones para el modelo que estás usando a través de la opción `instructions`.
555
556```json title="opencode.json"
557{
558  "$schema": "https://opencode.ai/config.json",
559  "instructions": ["CONTRIBUTING.md", "docs/guidelines.md", ".cursor/rules/*.md"]
560}
561```
562
563Esto requiere una variedad de rutas y patrones globales para archivos de instrucciones. [Más información
564sobre las reglas aquí](/docs/rules).
````

```

565
566---
567
```

[sst/opencode](/sst/opencode)

Como funcionan los recordatorios

Fast

## Funcionamiento de los Recordatorios System-Reminder

Los recordatorios `system-reminder` se activan automáticamente en conversaciones multi-paso para mantener el enfoque del modelo en mensajes pendientes del usuario prompt.ts:630-647 .

### Condiciones de Activación

Los recordatorios se aplican cuando se cumplen estas condiciones:

1. **Paso posterior al primero**: `step > 1` - no es el primer paso de la conversación
2. **Mensaje anterior completado**: `lastFinished` existe - hay un mensaje anterior finalizado
3. **Mensaje de usuario pendiente**: El mensaje es de tipo `user` con `id > lastFinished.id`
4. **Parte de texto válida**: La parte es tipo `text`, no está `ignored`, no es `synthetic`, y tiene contenido

### Formato del Recordatorio

Cuando se activa, el texto del usuario se envuelve con:

```
<system-reminder>  
The user sent the following message:  
[texto original del usuario]  
  
Please address this message and continue with your tasks.  
</system-reminder>  
```

### Propósito

Este mecanismo asegura que en conversaciones largas o cuando el modelo está ejecutando tareas múltiples, los mensajes del usuario que aún no han sido procesados permanezcan visibles y reciban atención explícita del modelo prompt.ts:637-644 .

## Notes

Los recordatorios son efímeros y solo afectan la presentación del mensaje al modelo en ese turno específico. No modifican permanentemente el mensaje almacenado en la base de datos.

Wiki pages you might want to explore:

- [Architecture Overview (sst/opencode)](/sst/opencode/1.2-architecture-overview)

[sst/opencode](https://github.com/sst/opencode)[packages/opencode/src/session/prompt.ts](https://github.com/sst/opencode/blob/715b844c/packages/opencode/src/session/prompt.ts#L630-L647)

```

627        })
628      }
629 
```

```

630      // Ephemerally wrap queued user messages with a reminder to stay on track
631      if (step > 1 && lastFinished) {
632        for (const msg of msgs) {
633          if (msg.info.role !== "user" || msg.info.id <= lastFinished.id) continue
634          for (const part of msg.parts) {
635            if (part.type !== "text" || part.ignored || part.synthetic) continue
636            if (!part.text.trim()) continue
637            part.text = [
638              "<system-reminder>",
639              "The user sent the following message:",
640              part.text,
641              "",
642              "Please address this message and continue with your tasks.",
643              "</system-reminder>",
644            ].join("\n")
645          }
646        }
647      }
```

```

648 
649      await Plugin.trigger("experimental.chat.messages.transform", {}, { messages: msgs })
650 
```

[sst/opencode](/sst/opencode)

Como funcionan las variantes personalizadas

Fast

## Variantes Personalizadas

Las variantes personalizadas te permiten configurar diferentes ajustes para el mismo modelo sin crear entradas duplicadas. Se definen en el archivo `opencode.json` bajo la sección `provider.<provider>.models.<model>.variants` models.mdx:108-134 .

### Definición y Fusión

Las variantes personalizadas se fusionan con las variantes integradas usando `mergeDeep()` provider.ts:1029-1039 . El proceso es:

1. **Generación**: Se generan variantes base para modelos con capacidades de razonamiento
2. **Fusión**: Las variantes personalizadas se fusionan con las integradas
3. **Filtrado**: Se eliminan las variantes marcadas como `disabled: true`

### Ejemplo de Configuración

```

{  
  "provider": {  
    "openai": {  
      "models": {  
        "gpt-5": {  
          "variants": {  
            "high": {  
              "reasoningEffort": "high",  
              "textVerbosity": "low",  
              "reasoningSummary": "auto"  
            },  
            "low": {  
              "reasoningEffort": "low",  
              "textVerbosity": "low"  
            },  
            "fast": {  
              "disabled": true  
            }  
          }  
        }  
      }  
    }  
  }  
}
```

### Comportamiento de Deshabilitación

Las variantes marcadas con `disabled: true` se eliminan completamente del resultado final provider.ts:1035-1038 . Esto permite deshabilitar variantes integradas que no necesitas.

### Variantes para Proveedores Personalizados

También puedes definir variantes para proveedores personalizados provider.test.ts:2087-2098 :

```

{  
  "provider": {  
    "custom-reasoning": {  
      "models": {  
        "reasoning-model": {  
          "variants": {  
            "low": { "reasoningEffort": "low" },  
            "medium": { "reasoningEffort": "medium" },  
            "high": { "reasoningEffort": "high", "disabled": true },  
            "custom": { "reasoningEffort": "custom", "budgetTokens": 5000 }  
          }  
        }  
      }  
    }  
  }  
}
```

## Notes

Las variantes personalizadas extienden pero no reemplazan completamente las variantes integradas. Los parámetros específicos del proveedor como `thinking.budgetTokens` para Anthropic o `reasoningEffort` para OpenAI se pueden ajustar según las capacidades del modelo. Usa el keybind `variant_cycle` para cambiar entre variantes durante una sesión.

Wiki pages you might want to explore:

- [Architecture Overview (sst/opencode)](/sst/opencode/1.2-architecture-overview)

[sst/opencode](https://github.com/sst/opencode)[packages/web/src/content/docs/es/models.mdx](https://github.com/sst/opencode/blob/715b844c/packages/web/src/content/docs/es/models.mdx#L108-L134)

```

105
106También puede configurar estas opciones para cualquier agente que esté utilizando. La configuración del agente anula cualquier opción global aquí. [Más información](/docs/agents/#additional).
107
```

````

108También puede definir variantes personalizadas que amplíen las integradas. Las variantes le permiten configurar diferentes ajustes para el mismo modelo sin crear entradas duplicadas:
109
110```jsonc title="opencode.jsonc" {6-21}
111{
112  "$schema": "https://opencode.ai/config.json",
113  "provider": {
114    "opencode": {
115      "models": {
116        "gpt-5": {
117          "variants": {
118            "high": {
119              "reasoningEffort": "high",
120              "textVerbosity": "low",
121              "reasoningSummary": "auto",
122            },
123            "low": {
124              "reasoningEffort": "low",
125              "textVerbosity": "low",
126              "reasoningSummary": "auto",
127            },
128          },
129        },
130      },
131    },
132  },
133}
134```
````

```

135
136---
137
```

[sst/opencode](https://github.com/sst/opencode)[packages/opencode/src/provider/provider.ts](https://github.com/sst/opencode/blob/715b844c/packages/opencode/src/provider/provider.ts#L1029-L1039)

```

1026        )
1027          delete provider.models[modelID]
1028 
```

```

1029        model.variants = mapValues(ProviderTransform.variants(model), (v) => v)
1030 
1031        // Filter out disabled variants from config
1032        const configVariants = configProvider?.models?.[modelID]?.variants
1033        if (configVariants && model.variants) {
1034          const merged = mergeDeep(model.variants, configVariants)
1035          model.variants = mapValues(
1036            pickBy(merged, (v) => !v.disabled),
1037            (v) => omit(v, ["disabled"]),
1038          )
1039        }
```

```

1040      }
1041 
1042      if (Object.keys(provider.models).length === 0) {
```

[sst/opencode](https://github.com/sst/opencode)[packages/opencode/test/provider/provider.test.ts](https://github.com/sst/opencode/blob/715b844c/packages/opencode/test/provider/provider.test.ts#L2087-L2098)

```

2084              npm: "@ai-sdk/openai-compatible",
2085              env: [],
2086              models: {
```

```

2087                "reasoning-model": {
2088                  name: "Reasoning Model",
2089                  tool_call: true,
2090                  reasoning: true,
2091                  limit: { context: 128000, output: 16000 },
2092                  variants: {
2093                    low: { reasoningEffort: "low" },
2094                    medium: { reasoningEffort: "medium" },
2095                    high: { reasoningEffort: "high", disabled: true },
2096                    custom: { reasoningEffort: "custom", budgetTokens: 5000 },
2097                  },
2098                },
```

```

2099              },
2100              options: { apiKey: "test-key" },
2101            },
```

[sst/opencode](/sst/opencode)

Dame los prompts del sistema implementados

Fast

View as codemap

## Prompts del Sistema Implementados

OpenCode implementa múltiples capas de prompts del sistema que se combinan dinámicamente según el modelo, configuración y contexto de la conversación.

### Prompts Base por Proveedor

Los prompts base se seleccionan según el modelo en `SystemPrompt.provider()` system.ts:19-27 :

- **GPT-5**: `PROMPT_CODEX` (desde `./prompt/codex_header.txt`)
- **GPT-/o1/o3**: `PROMPT_BEAST` (desde `./prompt/beast.txt`)
- **Gemini**: `PROMPT_GEMINI` (desde `./prompt/gemini.txt`)
- **Claude**: `PROMPT_ANTHROPIC` (desde `./prompt/anthropic.txt`)
- **Trinity**: `PROMPT_TRINITY` (desde `./prompt/trinity.txt`)
- **Default**: `PROMPT_ANTHROPIC_WITHOUT_TODO` (desde `./prompt/qwen.txt`)

### Prompt de Entorno

`SystemPrompt.environment()` genera información contextual dinámica system.ts:29-53 :

```
You are powered by the model named ${model.api.id}. The exact model ID is ${model.providerID}/${model.api.id}  
Here is some useful information about the environment you are running in:  
<env>  
  Working directory: ${Instance.directory}  
  Is directory a git repo: ${project.vcs === "git" ? "yes" : "no"}  
  Platform: ${process.platform}  
  Today's date: ${new Date().toDateString()}  
</env>  
<directories>  
  ${project.vcs === "git" && false ? await Ripgrep.tree(...) : ""}  
</directories>  
```

### Instrucciones Personalizadas

`InstructionPrompt.system()` carga instrucciones desde archivos configurados instruction.ts:117-142 :

1. **Archivos locales**: Desde rutas en `config.instructions` (ej: `CONTRIBUTING.md`, `docs/guidelines.md`)
2. **URLs remotas**: Fetch desde URLs HTTP/HTTPS con timeout de 5 segundos
3. **AGENTS.md**: Busca en directorio del proyecto, perfil de configuración, o global

### Prompt de Salida Estructurada

`STRUCTURED_OUTPUT_SYSTEM_PROMPT` se añade cuando `format.type === "json_schema"` prompt.ts:652-656 .

### Prompt de Límite de Pasos

`MAX_STEPS` se inyecta cuando se alcanza el límite máximo de iteraciones prompt.ts:666-672 .

### Construcción del Prompt Final

En `SessionPrompt.loop()`, el prompt final se construye concatenando prompt.ts:652-656 :

```

const system = [  
  ...(await SystemPrompt.environment(model)),  
  ...(await InstructionPrompt.system())  
]
```

## Notes

Los prompts específicos del proveedor como `anthropic.txt` incluyen ejemplos detallados de uso de herramientas y políticas anthropic.txt:31-105 . Los plugins pueden modificar los mensajes del sistema mediante el hook `experimental.chat.system.transform` llm.ts:82-93 .

Wiki pages you might want to explore:

- [Architecture Overview (sst/opencode)](/sst/opencode/1.2-architecture-overview)

[sst/opencode](https://github.com/sst/opencode)[packages/opencode/src/session/system.ts](https://github.com/sst/opencode/blob/715b844c/packages/opencode/src/session/system.ts#L19-L27)

```

16    return PROMPT_CODEX.trim()
17  }
18 
```

```

19  export function provider(model: Provider.Model) {
20    if (model.api.id.includes("gpt-5")) return [PROMPT_CODEX]
21    if (model.api.id.includes("gpt-") || model.api.id.includes("o1") || model.api.id.includes("o3"))
22      return [PROMPT_BEAST]
23    if (model.api.id.includes("gemini-")) return [PROMPT_GEMINI]
24    if (model.api.id.includes("claude")) return [PROMPT_ANTHROPIC]
25    if (model.api.id.toLowerCase().includes("trinity")) return [PROMPT_TRINITY]
26    return [PROMPT_ANTHROPIC_WITHOUT_TODO]
27  }
```

```

28 
```

```

29  export async function environment(model: Provider.Model) {
30    const project = Instance.project
31    return [
32      [
33        `You are powered by the model named ${model.api.id}. The exact model ID is ${model.providerID}/${model.api.id}`,
34        `Here is some useful information about the environment you are running in:`,
35        `<env>`,
36        `  Working directory: ${Instance.directory}`,
37        `  Is directory a git repo: ${project.vcs === "git" ? "yes" : "no"}`,
38        `  Platform: ${process.platform}`,
39        `  Today's date: ${new Date().toDateString()}`,
40        `</env>`,
41        `<directories>`,
42        `  ${
43          project.vcs === "git" && false
44            ? await Ripgrep.tree({
45                cwd: Instance.directory,
46                limit: 50,
47              })
48            : ""
49        }`,
50        `</directories>`,
51      ].join("\n"),
52    ]
53  }
```

```

54}
55 
56 
```

[sst/opencode](https://github.com/sst/opencode)[packages/opencode/src/session/instruction.ts](https://github.com/sst/opencode/blob/715b844c/packages/opencode/src/session/instruction.ts#L117-L142)

```

114    return paths
115  }
116 
```

```

117  export async function system() {
118    const config = await Config.get()
119    const paths = await systemPaths()
120 
121    const files = Array.from(paths).map(async (p) => {
122      const content = await Filesystem.readText(p).catch(() => "")
123      return content ? "Instructions from: " + p + "\n" + content : ""
124    })
125 
126    const urls: string[] = []
127    if (config.instructions) {
128      for (const instruction of config.instructions) {
129        if (instruction.startsWith("https://") || instruction.startsWith("http://")) {
130          urls.push(instruction)
131        }
132      }
133    }
134    const fetches = urls.map((url) =>
135      fetch(url, { signal: AbortSignal.timeout(5000) })
136        .then((res) => (res.ok ? res.text() : ""))
137        .catch(() => "")
138        .then((x) => (x ? "Instructions from: " + url + "\n" + x : "")),
139    )
140 
141    return Promise.all([...files, ...fetches]).then((result) => result.filter(Boolean))
142  }
```

```

143 
144  export function loaded(messages: MessageV2.WithParts[]) {
145    const paths = new Set<string>()
```

[sst/opencode](https://github.com/sst/opencode)[packages/opencode/src/session/prompt.ts](https://github.com/sst/opencode/blob/715b844c/packages/opencode/src/session/prompt.ts#L652-L656)

```

649      await Plugin.trigger("experimental.chat.messages.transform", {}, { messages: msgs })
650 
651      // Build system prompt, adding structured output instruction if needed
```

```

652      const system = [...(await SystemPrompt.environment(model)), ...(await InstructionPrompt.system())]
653      const format = lastUser.format ?? { type: "text" }
654      if (format.type === "json_schema") {
655        system.push(STRUCTURED_OUTPUT_SYSTEM_PROMPT)
656      }
```

```

657 
658      const result = await processor.process({
659        user: lastUser,
```

```

663        system,
664        messages: [
665          ...MessageV2.toModelMessages(msgs, model),
```

```

666          ...(isLastStep
667            ? [
668                {
669                  role: "assistant" as const,
670                  content: MAX_STEPS,
671                },
672              ]
```

```

673            : []),
674        ],
675        tools,
```

[sst/opencode](https://github.com/sst/opencode)[packages/opencode/src/session/prompt/anthropic.txt](https://github.com/sst/opencode/blob/715b844c/packages/opencode/src/session/prompt/anthropic.txt#L31-L105)

```

28
29Examples:
30
```

```

31<example>
32user: Run the build and fix any type errors
33assistant: I'm going to use the TodoWrite tool to write the following items to the todo list:
34- Run the build
35- Fix any type errors
36
37I'm now going to run the build using Bash.
38
39Looks like I found 10 type errors. I'm going to use the TodoWrite tool to write 10 items to the todo list.
40
41marking the first todo as in_progress
42
43Let me start working on the first item...
44
45The first item has been fixed, let me mark the first todo as completed, and move on to the second item...
46..
47..
48</example>
49In the above example, the assistant completes all the tasks, including the 10 error fixes and running the build and fixing all errors.
50
51<example>
52user: Help me write a new feature that allows users to track their usage metrics and export them to various formats
53assistant: I'll help you implement a usage metrics tracking and export feature. Let me first use the TodoWrite tool to plan this task.
54Adding the following todos to the todo list:
551. Research existing metrics tracking in the codebase
562. Design the metrics collection system
573. Implement core metrics tracking functionality
584. Create export functionality for different formats
59
60Let me start by researching the existing codebase to understand what metrics we might already be tracking and how we can build on that.
61
62I'm going to search for any existing metrics or telemetry code in the project.
63
64I've found some existing telemetry code. Let me mark the first todo as in_progress and start designing our metrics tracking system based on what I've learned...
65
66[Assistant continues implementing the feature step by step, marking todos as in_progress and completed as they go]
67</example>
68
69
70# Doing tasks
71The user will primarily request you perform software engineering tasks. This includes solving bugs, adding new functionality, refactoring code, explaining code, and more. For these tasks the following steps are recommended:
72- 
73- Use the TodoWrite tool to plan the task if required
74
75- Tool results and user messages may include <system-reminder> tags. <system-reminder> tags contain useful information and reminders. They are automatically added by the system, and bear no direct relation to the specific tool results or user messages in which they appear.
76
77
78# Tool usage policy
79- When doing file search, prefer to use the Task tool in order to reduce context usage.
80- You should proactively use the Task tool with specialized agents when the task at hand matches the agent's description.
81
82- When WebFetch returns a message about a redirect to a different host, you should immediately make a new WebFetch request with the redirect URL provided in the response.
83- You can call multiple tools in a single response. If you intend to call multiple tools and there are no dependencies between them, make all independent tool calls in parallel. Maximize use of parallel tool calls where possible to increase efficiency. However, if some tool calls depend on previous calls to inform dependent values, do NOT call these tools in parallel and instead call them sequentially. For instance, if one operation must complete before another starts, run these operations sequentially instead. Never use placeholders or guess missing parameters in tool calls.
84- If the user specifies that they want you to run tools "in parallel", you MUST send a single message with multiple tool use content blocks. For example, if you need to launch multiple agents in parallel, send a single message with multiple Task tool calls.
85- Use specialized tools instead of bash commands when possible, as this provides a better user experience. For file operations, use dedicated tools: Read for reading files instead of cat/head/tail, Edit for editing instead of sed/awk, and Write for creating files instead of cat with heredoc or echo redirection. Reserve bash tools exclusively for actual system commands and terminal operations that require shell execution. NEVER use bash echo or other command-line tools to communicate thoughts, explanations, or instructions to the user. Output all communication directly in your response text instead.
86- VERY IMPORTANT: When exploring the codebase to gather context or to answer a question that is not a needle query for a specific file/class/function, it is CRITICAL that you use the Task tool instead of running search commands directly.
87<example>
88user: Where are errors from the client handled?
89assistant: [Uses the Task tool to find the files that handle client errors instead of using Glob or Grep directly]
90</example>
91<example>
92user: What is the codebase structure?
93assistant: [Uses the Task tool]
94</example>
95
96IMPORTANT: Always use the TodoWrite tool to plan and track tasks throughout the conversation.
97
98# Code References
99
100When referencing specific functions or pieces of code include the pattern `file_path:line_number` to allow the user to easily navigate to the source code location.
101
102<example>
103user: Where are errors from the client handled?
104assistant: Clients are marked as failed in the `connectToServer` function in src/services/process.ts:712.
105</example>
```

[sst/opencode](https://github.com/sst/opencode)[packages/opencode/src/session/llm.ts](https://github.com/sst/opencode/blob/715b844c/packages/opencode/src/session/llm.ts#L82-L93)

```

79        .join("\n"),
80    )
81 
```

```

82    const header = system[0]
83    await Plugin.trigger(
84      "experimental.chat.system.transform",
85      { sessionID: input.sessionID, model: input.model },
86      { system },
87    )
88    // rejoin to maintain 2-part structure for caching if header unchanged
89    if (system.length > 2 && system[0] === header) {
90      const rest = system.slice(1)
91      system.length = 0
92      system.push(header, rest.join("\n"))
93    }
```

```

94 
95    const variant =
96      !input.small && input.model.variants && input.user.variant ? input.model.variants[input.user.variant] : {}
```

Search | DeepWiki