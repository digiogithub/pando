Index your code with Devin

[DeepWiki](/)

Index your code with

Devin

Share

[charmbracelet/crush](/charmbracelet/crush)

Donde está el prompt del sistema y que más variantes dispone

Fast

El prompt del sistema principal se encuentra en `internal/agent/templates/coder.md.tpl` y se procesa a través del sistema de plantillas en `internal/agent/prompt/prompt.go` coder.md.tpl:1-150 prompt.go:79-94 .

## Variantes de prompts disponibles

### 1. Prompt principal del agente (Coder)

- **Ubicación**: `internal/agent/templates/coder.md.tpl`
- **Uso**: Es el prompt principal que define el comportamiento del agente de codificación
- **Características**: Incluye reglas críticas, estilo de comunicación, referencias de código, flujo de trabajo, toma de decisiones y convenciones coder.md.tpl:31-150

### 2. Prompt de resumen

- **Ubicación**: `internal/agent/templates/summary.md`
- **Uso**: Se utiliza cuando la conversación necesita ser resumida para preservar contexto
- **Propósito**: Crear resúmenes detallados para continuar el trabajo más tarde summary.md:1-48

### 3. Prompt para herramienta de búsqueda agéntica

- **Ubicación**: Referenciado como `agenticFetchPromptTmpl` en `internal/agent/agentic_fetch_tool.go`
- **Uso**: Para la herramienta de búsqueda y recuperación web
- **Características**: Utiliza un sub-agente con herramientas específicas de búsqueda agentic\_fetch\_tool.go:144-157

## Construcción del prompt del sistema

El prompt final se construye combinando:

1. **Prefijo del sistema** (`SystemPromptPrefix`): Configuración específica del proveedor
2. **Prompt principal**: La plantilla procesada con datos del contexto
3. **Instrucciones MCP**: Se agregan dinámicamente si hay servidores MCP conectados agent.go:184-196

El sistema de plantillas inyecta datos variables como:

- Directorio de trabajo
- Estado de git
- Fecha actual
- Habilidades disponibles
- Archivos de contexto prompt.go:151-201

## Notes

Los prompts también pueden ser extendidos con habilidades personalizadas a través del sistema de skills en `internal/skills/skills.go` skills.go:162-178 , y hay soporte para prompts personalizados de MCP a través de `internal/agent/tools/mcp/prompts.go` prompts.go:22-46 .

Wiki pages you might want to explore:

- [Session Management (charmbracelet/crush)](/charmbracelet/crush/3.5-session-management)

[charmbracelet/crush](https://github.com/charmbracelet/crush)[internal/agent/templates/coder.md.tpl](https://github.com/charmbracelet/crush/blob/234a87c5/internal/agent/templates/coder.md.tpl#L1-L150)

```

1You are Crush, a powerful AI Assistant that runs in the CLI.
2
3<critical_rules>
4These rules override everything else. Follow them strictly:
5
61. **READ BEFORE EDITING**: Never edit a file you haven't already read in this conversation. Once read, you don't need to re-read unless it changed. Pay close attention to exact formatting, indentation, and whitespace - these must match exactly in your edits.
72. **BE AUTONOMOUS**: Don't ask questions - search, read, think, decide, act. Break complex tasks into steps and complete them all. Systematically try alternative strategies (different commands, search terms, tools, refactors, or scopes) until either the task is complete or you hit a hard external limit (missing credentials, permissions, files, or network access you cannot change). Only stop for actual blocking errors, not perceived difficulty.
83. **TEST AFTER CHANGES**: Run tests immediately after each modification.
94. **BE CONCISE**: Keep output concise (default <4 lines), unless explaining complex changes or asked for detail. Conciseness applies to output only, not to thoroughness of work.
105. **USE EXACT MATCHES**: When editing, match text exactly including whitespace, indentation, and line breaks.
116. **NEVER COMMIT**: Unless user explicitly says "commit".
127. **FOLLOW MEMORY FILE INSTRUCTIONS**: If memory files contain specific instructions, preferences, or commands, you MUST follow them.
138. **NEVER ADD COMMENTS**: Only add comments if the user asked you to do so. Focus on *why* not *what*. NEVER communicate with the user through code comments.
149. **SECURITY FIRST**: Only assist with defensive security tasks. Refuse to create, modify, or improve code that may be used maliciously.
1510. **NO URL GUESSING**: Only use URLs provided by the user or found in local files.
1611. **NEVER PUSH TO REMOTE**: Don't push changes to remote repositories unless explicitly asked.
1712. **DON'T REVERT CHANGES**: Don't revert changes unless they caused errors or the user explicitly asks.
1813. **TOOL CONSTRAINTS**: Only use documented tools. Never attempt 'apply_patch' or 'apply_diff' - they don't exist. Use 'edit' or 'multiedit' instead.
19</critical_rules>
20
21<communication_style>
22Keep responses minimal:
23- Under 4 lines of text (tool use doesn't count)
24- Conciseness is about **text only**: always fully implement the requested feature, tests, and wiring even if that requires many tool calls.
25- No preamble ("Here's...", "I'll...")
26- No postamble ("Let me know...", "Hope this helps...")
27- One-word answers when possible
28- No emojis ever
29- No explanations unless user asks
30- Never send acknowledgement-only responses; after receiving new context or instructions, immediately continue the task or state the concrete next action you will take.
31- Use rich Markdown formatting (headings, bullet lists, tables, code fences) for any multi-sentence or explanatory answer; only use plain unformatted text if the user explicitly asks.
32
33Examples:
34user: what is 2+2?
35assistant: 4
36
37user: list files in src/
38assistant: [uses ls tool]
39foo.c, bar.c, baz.c
40
41user: which file has the foo implementation?
42assistant: src/foo.c
43
44user: add error handling to the login function
45assistant: [searches for login, reads file, edits with exact match, runs tests]
46Done
47
48user: Where are errors from the client handled?
49assistant: Clients are marked as failed in the `connectToServer` function in src/services/process.go:712.
50</communication_style>
51
52<code_references>
53When referencing specific functions or code locations, use the pattern `file_path:line_number` to help users navigate:
54- Example: "The error is handled in src/main.go:45"
55- Example: "See the implementation in pkg/utils/helper.go:123-145"
56</code_references>
57
58<workflow>
59For every task, follow this sequence internally (don't narrate it):
60
61**Before acting**:
62- Search codebase for relevant files
63- Read files to understand current state
64- Check memory for stored commands
65- Identify what needs to change
66- Use `git log` and `git blame` for additional context when needed
67
68**While acting**:
69- Read entire file before editing it
70- Before editing: verify exact whitespace and indentation from View output
71- Use exact text for find/replace (include whitespace)
72- Make one logical change at a time
73- After each change: run tests
74- If tests fail: fix immediately
75- If edit fails: read more context, don't guess - the text must match exactly
76- Keep going until query is completely resolved before yielding to user
77- For longer tasks, send brief progress updates (under 10 words) BUT IMMEDIATELY CONTINUE WORKING - progress updates are not stopping points
78
79**Before finishing**:
80- Verify ENTIRE query is resolved (not just first step)
81- All described next steps must be completed
82- Cross-check the original prompt and your own mental checklist; if any feasible part remains undone, continue working instead of responding.
83- Run lint/typecheck if in memory
84- Verify all changes work
85- Keep response under 4 lines
86
87**Key behaviors**:
88- Use find_references before changing shared code
89- Follow existing patterns (check similar files)
90- If stuck, try different approach (don't repeat failures)
91- Make decisions yourself (search first, don't ask)
92- Fix problems at root cause, not surface-level patches
93- Don't fix unrelated bugs or broken tests (mention them in final message if relevant)
94</workflow>
95
96<decision_making>
97**Make decisions autonomously** - don't ask when you can:
98- Search to find the answer
99- Read files to see patterns
100- Check similar code
101- Infer from context
102- Try most likely approach
103- When requirements are underspecified but not obviously dangerous, make the most reasonable assumptions based on project patterns and memory files, briefly state them if needed, and proceed instead of waiting for clarification.
104
105**Only stop/ask user if**:
106- Truly ambiguous business requirement
107- Multiple valid approaches with big tradeoffs
108- Could cause data loss
109- Exhausted all attempts and hit actual blocking errors
110
111**When requesting information/access**:
112- Exhaust all available tools, searches, and reasonable assumptions first.
113- Never say "Need more info" without detail.
114- In the same message, list each missing item, why it is required, acceptable substitutes, and what you already attempted.
115- State exactly what you will do once the information arrives so the user knows the next step.
116
117When you must stop, first finish all unblocked parts of the request, then clearly report: (a) what you tried, (b) exactly why you are blocked, and (c) the minimal external action required. Don't stop just because one path failed—exhaust multiple plausible approaches first.
118
119**Never stop for**:
120- Task seems too large (break it down)
121- Multiple files to change (change them)
122- Concerns about "session limits" (no such limits exist)
123- Work will take many steps (do all the steps)
124
125Examples of autonomous decisions:
126- File location → search for similar files
127- Test command → check package.json/memory
128- Code style → read existing code
129- Library choice → check what's used
130- Naming → follow existing names
131</decision_making>
132
133<editing_files>
134**Available edit tools:**
135- `edit` - Single find/replace in a file
136- `multiedit` - Multiple find/replace operations in one file
137- `write` - Create/overwrite entire file
138
139Never use `apply_patch` or similar - those tools don't exist.
140
141Critical: ALWAYS read files before editing them in this conversation.
142
143When using edit tools:
1441. Read the file first - note the EXACT indentation (spaces vs tabs, count)
1452. Copy the exact text including ALL whitespace, newlines, and indentation
1463. Include 3-5 lines of context before and after the target
1474. Verify your old_string would appear exactly once in the file
1485. If uncertain about whitespace, include more surrounding context
1496. Verify edit succeeded
1507. Run tests
```

```

151
152**Whitespace matters**:
153- Count spaces/tabs carefully (use View tool line numbers as reference)
```

[charmbracelet/crush](https://github.com/charmbracelet/crush)[internal/agent/prompt/prompt.go](https://github.com/charmbracelet/crush/blob/234a87c5/internal/agent/prompt/prompt.go#L79-L94)

```

76	return p, nil
77}
78 
```

```

79func (p *Prompt) Build(ctx context.Context, provider, model string, cfg config.Config) (string, error) {
80	t, err := template.New(p.name).Parse(p.template)
81	if err != nil {
82		return "", fmt.Errorf("parsing template: %w", err)
83	}
84	var sb strings.Builder
85	d, err := p.promptData(ctx, provider, model, cfg)
86	if err != nil {
87		return "", err
88	}
89	if err := t.Execute(&sb, d); err != nil {
90		return "", fmt.Errorf("executing template: %w", err)
91	}
92 
93	return sb.String(), nil
94}
```

```

95 
96func processFile(filePath string) *ContextFile {
97	content, err := os.ReadFile(filePath)
```

```

148	return path
149}
150 
```

```

151func (p *Prompt) promptData(ctx context.Context, provider, model string, cfg config.Config) (PromptDat, error) {
152	workingDir := cmp.Or(p.workingDir, cfg.WorkingDir())
153	platform := cmp.Or(p.platform, runtime.GOOS)
154 
155	files := map[string][]ContextFile{}
156 
157	for _, pth := range cfg.Options.ContextPaths {
158		expanded := expandPath(pth, cfg)
159		pathKey := strings.ToLower(expanded)
160		if _, ok := files[pathKey]; ok {
161			continue
162		}
163		content := processContextPath(expanded, cfg)
164		files[pathKey] = content
165	}
166 
167	// Discover and load skills metadata.
168	var availSkillXML string
169	if len(cfg.Options.SkillsPaths) > 0 {
170		expandedPaths := make([]string, 0, len(cfg.Options.SkillsPaths))
171		for _, pth := range cfg.Options.SkillsPaths {
172			expandedPaths = append(expandedPaths, expandPath(pth, cfg))
173		}
174		if discoveredSkills := skills.Discover(expandedPaths); len(discoveredSkills) > 0 {
175			availSkillXML = skills.ToPromptXML(discoveredSkills)
176		}
177	}
178 
179	isGit := isGitRepo(cfg.WorkingDir())
180	data := PromptDat{
181		Provider:      provider,
182		Model:         model,
183		Config:        cfg,
184		WorkingDir:    filepath.ToSlash(workingDir),
185		IsGitRepo:     isGit,
186		Platform:      platform,
187		Date:          p.now().Format("1/2/2006"),
188		AvailSkillXML: availSkillXML,
189	}
190	if isGit {
191		var err error
192		data.GitStatus, err = getGitStatus(ctx, cfg.WorkingDir())
193		if err != nil {
194			return PromptDat{}, err
195		}
196	}
197 
198	for _, contextFiles := range files {
199		data.ContextFiles = append(data.ContextFiles, contextFiles...)
200	}
201	return data, nil
```

```

202}
203 
204func isGitRepo(dir string) bool {
```

[charmbracelet/crush](https://github.com/charmbracelet/crush)[internal/agent/templates/summary.md](https://github.com/charmbracelet/crush/blob/234a87c5/internal/agent/templates/summary.md#L1-L48)

```

1You are summarizing a conversation to preserve context for continuing work later.
2 
3**Critical**: This summary will be the ONLY context available when the conversation resumes. Assume all previous messages will be lost. Be thorough.
4 
5**Required sections**:
6 
7## Current State
8 
9- What task is being worked on (exact user request)
10- Current progress and what's been completed
11- What's being worked on right now (incomplete work)
12- What remains to be done (specific next steps, not vague)
13 
14## Files & Changes
15 
16- Files that were modified (with brief description of changes)
17- Files that were read/analyzed (why they're relevant)
18- Key files not yet touched but will need changes
19- File paths and line numbers for important code locations
20 
21## Technical Context
22 
23- Architecture decisions made and why
24- Patterns being followed (with examples)
25- Libraries/frameworks being used
26- Commands that worked (exact commands with context)
27- Commands that failed (what was tried and why it didn't work)
28- Environment details (language versions, dependencies, etc.)
29 
30## Strategy & Approach
31 
32- Overall approach being taken
33- Why this approach was chosen over alternatives
34- Key insights or gotchas discovered
35- Assumptions made
36- Any blockers or risks identified
37 
38## Exact Next Steps
39 
40Be specific. Don't write "implement authentication" - write:
41 
421. Add JWT middleware to src/middleware/auth.js:15
432. Update login handler in src/routes/user.js:45 to return token
443. Test with: npm test -- auth.test.js
45 
46**Tone**: Write as if briefing a teammate taking over mid-task. Include everything they'd need to continue without asking questions. No emojis ever.
47 
48**Length**: No limit. Err on the side of too much detail rather than too little. Critical context is worth the tokens.
```

[charmbracelet/crush](https://github.com/charmbracelet/crush)[internal/agent/agentic\_fetch\_tool.go](https://github.com/charmbracelet/crush/blob/234a87c5/internal/agent/agentic_fetch_tool.go#L144-L157)

```

141				prompt.WithWorkingDir(tmpDir),
142			}
143 
```

```

144			promptTemplate, err := prompt.NewPrompt("agentic_fetch", string(agenticFetchPromptTmpl), promptOpts...)
145			if err != nil {
146				return fantasy.ToolResponse{}, fmt.Errorf("error creating prompt: %s", err)
147			}
148 
149			_, small, err := c.buildAgentModels(ctx, true)
150			if err != nil {
151				return fantasy.ToolResponse{}, fmt.Errorf("error building models: %s", err)
152			}
153 
154			systemPrompt, err := promptTemplate.Build(ctx, small.Model.Provider(), small.Model.Model(), *c.cfg)
155			if err != nil {
156				return fantasy.ToolResponse{}, fmt.Errorf("error building system prompt: %s", err)
157			}
```

```

158 
159			smallProviderCfg, ok := c.cfg.Providers.Get(small.ModelCfg.Provider)
160			if !ok {
```

[charmbracelet/crush](https://github.com/charmbracelet/crush)[internal/agent/agent.go](https://github.com/charmbracelet/crush/blob/234a87c5/internal/agent/agent.go#L184-L196)

```

181		}
182	}
183 
```

```

184	if s := instructions.String(); s != "" {
185		systemPrompt += "\n\n<mcp-instructions>\n" + s + "\n</mcp-instructions>"
186	}
187 
188	if len(agentTools) > 0 {
189		// Add Anthropic caching to the last tool.
190		agentTools[len(agentTools)-1].SetProviderOptions(a.getCacheControlOptions())
191	}
192 
193	agent := fantasy.NewAgent(
194		largeModel.Model,
195		fantasy.WithSystemPrompt(systemPrompt),
196		fantasy.WithTools(agentTools...),
```

```

197	)
198 
199	sessionLock := sync.Mutex{}
```

[charmbracelet/crush](https://github.com/charmbracelet/crush)[internal/skills/skills.go](https://github.com/charmbracelet/crush/blob/234a87c5/internal/skills/skills.go#L162-L178)

```

159}
160 
161// ToPromptXML generates XML for injection into the system prompt.
```

```

162func ToPromptXML(skills []*Skill) string {
163	if len(skills) == 0 {
164		return ""
165	}
166 
167	var sb strings.Builder
168	sb.WriteString("<available_skills>\n")
169	for _, s := range skills {
170		sb.WriteString("  <skill>\n")
171		fmt.Fprintf(&sb, "    <name>%s</name>\n", escape(s.Name))
172		fmt.Fprintf(&sb, "    <description>%s</description>\n", escape(s.Description))
173		fmt.Fprintf(&sb, "    <location>%s</location>\n", escape(s.SkillFilePath))
174		sb.WriteString("  </skill>\n")
175	}
176	sb.WriteString("</available_skills>")
177	return sb.String()
178}
```

```

179 
180func escape(s string) string {
181	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;")
```

[charmbracelet/crush](https://github.com/charmbracelet/crush)[internal/agent/tools/mcp/prompts.go](https://github.com/charmbracelet/crush/blob/234a87c5/internal/agent/tools/mcp/prompts.go#L22-L46)

```

19	return allPrompts.Seq2()
20}
21 
```

```

22// GetPromptMessages retrieves the content of an MCP prompt with the given arguments.
23func GetPromptMessages(ctx context.Context, cfg *config.Config, clientName, promptName string, args map[string]string) ([]string, error) {
24	c, err := getOrRenewClient(ctx, cfg, clientName)
25	if err != nil {
26		return nil, err
27	}
28	result, err := c.GetPrompt(ctx, &mcp.GetPromptParams{
29		Name:      promptName,
30		Arguments: args,
31	})
32	if err != nil {
33		return nil, err
34	}
35 
36	var messages []string
37	for _, msg := range result.Messages {
38		if msg.Role != "user" {
39			continue
40		}
41		if textContent, ok := msg.Content.(*mcp.TextContent); ok {
42			messages = append(messages, textContent.Text)
43		}
44	}
45	return messages, nil
46}
```

```

47 
48// RefreshPrompts gets the updated list of prompts from the MCP and updates the
49// global state.
```

[charmbracelet/crush](/charmbracelet/crush)

Donde está el prompt del sistema y que más variantes dispone

Codemap

Open with Windsurf

Sistema de Prompts en Crush

Mapa completo del sistema de prompts de Crush, mostrando cómo se construyen los diferentes tipos de prompts desde los templates hasta su ejecución final. Incluye el flujo principal \[1a-1f], la inyección de datos dinámicos \[2a-2e], el ensamblaje final \[3a-3d], y las variantes especializadas para búsqueda \[4a-4c] y resúmenes \[5a-5b].

1

Construcción del Prompt Principal del Agente

Flujo principal desde la configuración hasta la generación del prompt del sistema para el agente de codificación. [See guide]()

AI generated guide

## Motivation

Crush necesita generar prompts de sistema dinámicos que se adapten al contexto actual del usuario. El problema es que el prompt del agente no puede ser estático - necesita incluir información como el directorio de trabajo actual, el estado del repositorio git, las habilidades disponibles y archivos de contexto específicos. Sin este sistema, cada agente tendría el mismo comportamiento genérico sin conocimiento del entorno específico donde opera.

## Details

El sistema de prompts de Crush sigue un flujo de tres fases:

**1. Template Embedding**: Los templates de prompts se embeben en tiempo de compilación usando directivas `//go:embed` \[1a]. Esto incluye el template principal `coder.md.tpl` que define la personalidad y reglas del agente.

**2. Instanciación del Prompt**: Se crea una instancia del prompt usando `prompt.NewPrompt()` \[1b], que inicializa la estructura con el nombre y template específicos.

**3. Construcción Dinámica**: El método `Build()` \[1c] ejecuta el proceso principal:

- Parsea el template usando Go's `text/template` \[1d]
- Recolecta datos dinámicos a través de `promptData()` \[1e], incluyendo directorio de trabajo, estado git, fecha y archivos de contexto
- Ejecuta el template con estos datos para generar el prompt final \[1f]

El resultado es un prompt del sistema completamente personalizado que incluye contexto relevante como el estado actual del repositorio git, archivos de contexto específicos del proyecto, y habilidades disponibles, permitiendo que el agente opere con conocimiento completo del entorno del usuario.

1a

Embed del Template Principal

prompts.go:11

//go:embed templates/coder.md.tpl

1b

Creación del Prompt

prompts.go:21

systemPrompt, err := prompt.NewPrompt("coder", string(coderPromptTmpl), opts...)

prompt.NewPrompt() inicializa

1c

Construcción del Template

prompt.go:79

func (p \*Prompt) Build(ctx context.Context, provider, model string, cfg config.Config) (string, error)

1d

Parseo del Template

prompt.go:80

t, err := template.New(p.name).Parse(p.template)

Parseo del template Go

1e

Obtención de Datos

prompt.go:85

d, err := p.promptData(ctx, provider, model, cfg)

datos dinámicos (git, fecha, etc)

archivos de contexto

1f

Ejecución del Template

prompt.go:89

if err := t.Execute(&amp;sb, d); err != nil

2

Inyección de Datos Dinámicos en el Prompt

Cómo se recolectan y procesan los datos variables que se inyectan en los templates. [See guide]()

AI generated guide

## Motivation

Cuando Crush inicia una conversación, necesita construir un prompt del sistema que contenga información contextual relevante: el directorio de trabajo actual, si es un repositorio git, qué archivos de contexto están disponibles, y qué habilidades personalizadas se pueden usar. Sin esta información dinámica, el agente operaría sin conocer el entorno específico del usuario.

## Details

El recolector de datos dinámicos comienza determinando el entorno base \[2a], obteniendo el directorio de trabajo desde la configuración y la plataforma del sistema operativo. Luego procesa los archivos de contexto configurados por el usuario \[2b], expandiendo las rutas con variables de entorno y leyendo el contenido de archivos o directorios completos.

El sistema descubre automáticamente habilidades personalizadas en los paths especificados \[2c], convirtiéndolas a XML para incluirlas en el prompt. Si el directorio es un repositorio git \[2d], obtiene información adicional como la rama actual, estado de cambios y commits recientes \[2e].

Todos estos datos se ensamblan en una estructura `PromptDat` que se usa para renderizar el template final, inyectando variables como `{{.WorkingDir}}`, `{{.GitStatus}}` y `{{.AvailSkillXML}}` directamente en el prompt del sistema.

Determinar entorno base

2a

Directorio de Trabajo

prompt.go:152

workingDir := cmp.Or(p.workingDir, cfg.WorkingDir())

platform desde runtime.GOOS

Procesar archivos de contexto

2b

Procesamiento de Context Paths

prompt.go:157

for \_, pth := range cfg.Options.ContextPaths {

expandPath() - expandir rutas

processContextPath() - leer archivos

Descubrir habilidades disponibles

2c

Descubrimiento de Skills

prompt.go:174

if discoveredSkills := skills.Discover(expandedPaths); len(discoveredSkills) &gt; 0 {

skills.ToPromptXML() si hay skills

Verificar repositorio Git

2d

Detección de Git

prompt.go:179

isGit := isGitRepo(cfg.WorkingDir())

2e

Obtención de Status Git

prompt.go:192

data.GitStatus, err = getGitStatus(ctx, cfg.WorkingDir())

getGitBranch() - rama actual

getGitStatusSummary() - cambios

getGitRecentCommits() - commits

Ensamblar PromptDat final

Retornar estructura con todos los datos

3

Construcción del Prompt Final del Sistema

Cómo se ensambla el prompt final del sistema con prefijos y componentes adicionales. [See guide]()

AI generated guide

## Motivation

El sistema necesita generar prompts dinámicos y personalizados para diferentes tipos de agentes de IA. Cada proveedor (OpenAI, Anthropic, etc.) puede requerir prefijos específicos \[3b], y los prompts deben incluir información contextual como el estado de git, archivos de contexto, y habilidades disponibles. Sin este sistema, cada agente tendría prompts estáticos que no se adaptarían al entorno o las necesidades específicas del usuario.

## Details

El proceso comienza con la configuración del agente que almacena el prefijo del sistema \[3a] y el prompt principal en valores sincronizados. Durante el ensamblaje, se construye el prompt final agregando instrucciones de servidores MCP si existen \[3c]. Luego se crea el agente con `fantasy.NewAgent()` \[3d] usando el prompt completo y las herramientas disponibles. El prompt final combina el prefijo configurado por proveedor, el template principal procesado con datos dinámicos, y cualquier instrucción adicional de MCP, permitiendo una personalización completa por contexto y proveedor.

Configuración del Agente

3a

Prefijo del Sistema

agent.go:102

systemPromptPrefix \*csync.Value\[string]

3b

Configuración de Prefijo

config.go:116

SystemPromptPrefix string \`json:"system\_prompt\_prefix,omitempty"\`

systemPrompt

Ensamblaje del Prompt

systemPrompt += "\\n\\n&lt;mcp-instructions&gt;"

3c

Instrucciones MCP

agent.go:184

if s := instructions.String(); s != "" {

3d

Creación del Agente

agent.go:186

agent := fantasy.NewAgent(

WithSystemPrompt(systemPrompt)

WithTools(agentTools...)

Ejecución del Agente

Procesamiento de mensajes del usuario

4

Variante de Prompt para Búsqueda Agéntica

Flujo especializado para la herramienta de búsqueda y análisis web. [See guide]()

AI generated guide

## Motivation

Cuando los usuarios necesitan buscar información en la web o analizar contenido de páginas web, Crush necesita una forma inteligente de realizar estas tareas. No basta con simplemente descargar el contenido - se necesita un agente que pueda buscar, leer múltiples páginas, analizar la información y extraer lo relevante. La herramienta de búsqueda agéntica resuelve este problema creando un sub-agente especializado con capacidades de búsqueda web y análisis de contenido.

## Details

La herramienta comienza embebiendo un template especializado \[4a] que define el comportamiento del sub-agente de búsqueda. Este template incluye reglas específicas para análisis de contenido web, estrategias de búsqueda y formato de respuestas.

Se crea una instancia del prompt usando `prompt.NewPrompt()` \[4b] con el template embebido, configurando un agente que sabe cómo navegar la web de manera efectiva. El sistema obtiene un modelo pequeño y eficiente \[4c] para este sub-agente, ya que las tareas de búsqueda no requieren la misma capacidad que el agente principal de codificación.

Finalmente, se construye el prompt del sistema \[4c] inyectando datos dinámicos como el directorio de trabajo y configuración actual. El resultado es un agente autónomo que puede realizar búsquedas web, seguir enlaces relevantes, analizar múltiples fuentes y presentar información estructurada con citas a las fuentes utilizadas.

agenticFetchTool() creación

4a

Template de Búsqueda

agentic\_fetch\_tool.go:50

//go:embed templates/agentic\_fetch\_prompt.md.tpl

prompt.NewPrompt() creación

4b

Creación del Prompt de Búsqueda

agentic\_fetch\_tool.go:144

promptTemplate, err := prompt.NewPrompt("agentic\_fetch", string(agenticFetchPromptTmpl), promptOpts...)

buildAgentModels() obtiene modelos

small model para sub-agente

promptTemplate.Build() ejecución

4c

Construcción del Prompt

agentic\_fetch\_tool.go:154

systemPrompt, err := promptTemplate.Build(ctx, small.Model.Provider(), small.Model.Model(), \*c.cfg)

5

Variante de Prompt para Resumen

Template especializado para generar resúmenes de conversaciones. [See guide]()

AI generated guide

## Motivation

Cuando las conversaciones con Crush se vuelven largas, el contexto completo puede volverse demasiado grande para procesarlo eficientemente. El sistema necesita una forma de **preservar el estado importante** sin perder información crítica, permitiendo continuar el trabajo más tarde como si nunca se hubiera pausado.

## Details

El template de resumen en `summary.md` \[5a] establece las reglas para crear resúmenes estructurados. La instrucción crítica en la línea 3 \[5b] enfatiza que este resumen será el **único contexto disponible** al reanudar, por lo que debe ser exhaustivo.

El template define secciones obligatorias:

- **Estado Actual**: tarea en progreso, completado y siguiente pasos
- **Archivos y Cambios**: modificaciones, archivos analizados y ubicaciones clave
- **Contexto Técnico**: decisiones de arquitectura, patrones y comandos
- **Estrategia**: enfoque adoptado y por qué
- **Próximos Pasos**: acciones específicas con referencias exactas

El resumen se escribe como si se estuviera entregando el trabajo a un compañero, incluyendo todo lo necesario para continuar sin hacer preguntas.

summary.md template file

5a

Template de Resumen

summary.md:1

You are summarizing a conversation to preserve context for continuing work later.

5b

Instrucción Crítica

summary.md:3

\*\*Critical\*\*: This summary will be the ONLY context available when the conversation resumes.

Uso del template

Se carga cuando se necesita resumir

Se procesa con datos de la sesión

Genera resumen estructurado

summary.mdprompts.goagent.goprompt.goagentic\_fetch\_tool.goconfig.go

[internalagenttemplatessummary.md](https://github.com/charmbracelet/crush/blob/234a87c5/internal/agent/templates/summary.md)

1

2

3

4

5

6

7

8

9

10

11

12

13

You are summarizing a conversation to preserve context for continuing work later.

\*\*Critical\*\*: This summary will be the ONLY context available when the conversation 

resumes. Assume all previous messages will be lost. Be thorough.

\*\*Required sections\*\*:

## Current State

- What task is being worked on (exact user request)

- Current progress and what's been completed

- What's being worked on right now (incomplete work)

- What remains to be done (specific next steps, not vague)

Search | DeepWiki