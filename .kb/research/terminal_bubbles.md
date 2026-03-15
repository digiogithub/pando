<img src="https://r2cdn.perplexity.ai/pplx-full-logo-primary-dark%402x.png" style="height:64px;margin-right:32px"/>

# busco una librería en la que pueda integrar una view de terminal en una app tui hecha con bubbles de charmbracelet

Sí: la opción más directa que he encontrado para **integrar una vista de terminal** con una app basada en Bubble Tea/Bubbles es `github.com/BigJk/crt`, porque está pensada justo como un “minimal terminal emulator for Bubbletea” y se puede adjuntar a un `tea.Program` mediante su adaptador para Bubble Tea. [^1_1]

## Opción principal

`BigJk/crt` está descrita como una librería que proporciona un emulador de terminal simple que puede adjuntarse a `tea.Program`, con soporte para TrueColor, ratón y teclado, e interpretación de secuencias CSI usadas por Bubble Tea. [^1_1] Además, su README muestra un ejemplo de uso con `bubbleadapter.Window(...)`, donde le pasas tu `tea.Model` y lo renderiza en una ventana propia. [^1_1]

Esto encaja bien si lo que buscas no es solo un panel de texto, sino una terminal “real” embebida para una app TUI/terminal-like. [^1_1]

## Matiz importante

Si por “terminal view” te vale un área scrollable para mostrar salida ANSI o logs, `bubbles/viewport` existe y sirve para renderizar un viewport dentro de Bubble Tea, con scroll vertical y horizontal programático. [^1_2][^1_3] Pero `viewport` no es un emulador de terminal ni ejecuta PTY/shell; es un componente de visualización dentro de Bubble Tea. [^1_3]

En cambio, `crt` sí apunta al caso de emulación/render de terminal asociada a Bubble Tea, aunque usa `ebitengine` para dibujar y su propio README avisa de limitaciones: no todas las secuencias CSI están implementadas y el mapeo de teclas aún puede ser algo irregular. [^1_1]

## Recomendación práctica

Te diría:

- Usa `BigJk/crt` si quieres una terminal embebida de verdad dentro del ecosistema Bubble Tea. [^1_1]
- Usa `bubbles/viewport` si solo necesitas un panel con contenido desplazable, como logs, salida de comandos ya procesada o texto ANSI limitado. [^1_2][^1_3]


## Encaje con Bubbles

Bubble Tea es el framework base, y Bubbles aporta componentes reutilizables como `viewport`, listas, inputs y tablas, así que tu arquitectura natural sería seguir con Bubble Tea/Bubbles para la UI y añadir `crt` solo en la zona donde necesites la terminal embebida. [^1_1][^1_3][^1_1]

Si quieres, puedo proponerte ahora una arquitectura concreta para **Bubble Tea + Bubbles + PTY real** con ejemplo en Go para incrustar una shell dentro de tu TUI.
<span style="display:none">[^1_10][^1_11][^1_12][^1_13][^1_14][^1_15][^1_4][^1_5][^1_6][^1_7][^1_8][^1_9]</span>

<div align="center">⁂</div>

[^1_1]: https://github.com/charmbracelet/bubbletea

[^1_2]: https://pkg.go.dev/github.com/charmbracelet/bubbles/viewport

[^1_3]: https://github.com/charmbracelet/bubbles

[^1_4]: https://www.reddit.com/r/golang/comments/sh656f/how_to_read_value_from_terminal_using_bubble_tea/

[^1_5]: https://www.prskavec.net/post/bubbletea/

[^1_6]: https://dasroot.net/posts/2026/03/build-tui-apps-go-bubbletea/

[^1_7]: https://penchev.com/posts/create-tui-with-go/

[^1_8]: https://packagemain.tech/p/terminal-ui-bubble-tea

[^1_9]: https://oneuptime.com/blog/post/2026-01-30-how-to-build-command-line-tools-with-bubbletea-in-go/view

[^1_10]: https://bbs.archlinux.org/viewtopic.php?id=303257

[^1_11]: https://harrisoncramer.me/terminal-applications-in-go

[^1_12]: https://www.youtube.com/watch?v=_gzypL-Qv-g

[^1_13]: https://ratatui.rs

[^1_14]: https://lobehub.com/nl/skills/333-333-333-vim-roguelike-bubbletea

[^1_15]: https://github.com/BigJk/crt


---

# alguna otra librería para embeber un terminal en una tui Golang?

Sí: además de `crt`, una alternativa bastante clara hoy es `github.com/taigrr/bubbleterm`, que se define como un emulador de terminal headless y embebible en Go, con salida compatible con Bubble Tea. [^2_1]

## Librerías útiles

`bubbleterm` parece más alineada con tu caso que `crt` si quieres seguir dentro de una TUI tradicional, porque no renderiza con `ebitengine`, sino que expone un `Model` para Bubble Tea y está pensada para correr comandos dentro de un “terminal bubble”. [^2_1] Su README indica soporte para parsing ANSI, estado de pantalla, 256 colores y true color, scrollback, teclado, ratón, resize y compatibilidad con `xterm-256color`. [^2_1]

`crt`, por contraste, también sirve para acoplar una terminal a Bubble Tea, pero su enfoque es más de ventana renderizada con `ebitengine`, no tanto de componente TUI nativo renderizado como string dentro del árbol de vistas de Bubble Tea. [^2_2]

## Diferencias clave

| Librería | Enfoque | Encaje con Bubble Tea |
| :-- | :-- | :-- |
| `taigrr/bubbleterm` | Emulador headless embebible; Bubble component; pensado para PTY + TUI. [^2_1] | Muy bueno si quieres un panel terminal dentro de tu layout Bubble Tea. [^2_1] |
| `BigJk/crt` | Emulador simple acoplado a `tea.Program`, renderizado con `ebitengine`. [^2_2] | Útil si aceptas ese backend gráfico/render distinto. [^2_2] |
| `creack/pty` / `danielgatis/go-pty` | Gestión de PTY/procesos, no emulación completa de terminal por sí sola. [^2_3][^2_1] | Sirve como base, pero necesitas por encima una capa de emulación/render. [^2_3][^2_1] |

## Mi recomendación

Si tu app ya está hecha con Bubbles y quieres una integración limpia, probaría primero `bubbleterm`. [^2_1] De hecho, su documentación incluye `NewWithCommand`, `Update`, `View`, `Resize`, `Focus`, `SendInput` y un ejemplo explícito de integrar `htop` dentro de Bubble Tea, que es justo el tipo de API que suele encajar bien en un layout con panes. [^2_1]

Si lo que quieres es montar tú el stack, entonces la combinación más flexible sería `creack/pty` o `go-pty` para el proceso/PTY y una librería de emulación como `bubbleterm` por encima. [^2_3][^2_1]

## Limitaciones

En `bubbleterm` se documentan limitaciones actuales: no hay damage tracking completo, puede redibujar toda la pantalla en cada frame y el borrado de caracteres puede fallar en algunos casos; incluso mencionan que ejecutar `tmux` dentro del emulador mitiga parte de esos problemas. [^2_1] Eso sugiere que está muy prometedora, pero todavía conviene validarla con casos reales como `bash`, `zsh`, `vim`, `less`, `htop` o `lazygit`. [^2_1]

Si quieres, te preparo una comparación más técnica entre `bubbleterm`, `crt` y montar `pty + vt10x/tcell` para elegir según tu caso.
<span style="display:none">[^2_10][^2_11][^2_12][^2_13][^2_14][^2_15][^2_16][^2_4][^2_5][^2_6][^2_7][^2_8][^2_9]</span>

<div align="center">⁂</div>

[^2_1]: https://github.com/charmbracelet/bubbletea

[^2_2]: https://github.com/BigJk/crt

[^2_3]: https://github.com/danielgatis/go-pty

[^2_4]: https://github.com/ratatui/awesome-ratatui

[^2_5]: https://www.reddit.com/r/commandline/comments/1ro4jrh/gotui_terminal_uis_in_go_with_inline_mode_flexbox/

[^2_6]: https://dev.to/bmf_san/understanding-terminal-specifications-to-help-with-tui-development-749

[^2_7]: https://stackoverflow.com/questions/50711439/how-to-use-vim-in-a-part-of-terminal-with-go

[^2_8]: https://www.youtube.com/watch?v=aiWOTYiIzyE

[^2_9]: https://libraries.io/go/github.com%2Ftaigrr%2Fbubbleterm

[^2_10]: https://go.libhunt.com/bubbletea-alternatives

[^2_11]: https://pkg.go.dev/github.com/taigrr/bubbleterm

[^2_12]: https://stackoverflow.com/questions/72765557/using-a-pty-without-a-command

[^2_13]: https://github.com/rothgar/awesome-tuis

[^2_14]: https://www.reddit.com/r/golang/comments/gaxsua/recommendations_for_a_terminal_emulation_library/

[^2_15]: https://www.reddit.com/r/commandline/comments/1mb5q3p/zuse_a_modern_irc_chat_for_the_terminal_made_in/

[^2_16]: https://raku-advent.blog/2025/12/21/a-terminals-tale/


---

# hazme una tabla comparativa

Sí. Aquí tienes una tabla pensada para elegir entre las opciones más razonables para embeber una terminal en una TUI Go, especialmente si ya estás en Bubble Tea. [^3_1][^3_2][^3_3]

## Tabla

| Opción | Qué es | Integración con Bubble Tea | Pros | Contras | Cuándo la usaría |
| :-- | :-- | :-- | :-- | :-- | :-- |
| `taigrr/bubbleterm` | Emulador de terminal headless y embebible, con salida compatible con Bubble Tea. [^3_1][^3_2] | Muy alta: expone `Init`, `Update`, `View`, `Resize`, `SendInput`, `StartCommand` y ejemplo directo con Bubble Tea. [^3_1] | Diseñada para este caso; soporta ANSI, UTF-8, 256/true color, scrollback, teclado, ratón y resize. [^3_1][^3_4] | Proyecto joven; la propia doc menciona limitaciones de redibujado/damage tracking según el estado de la implementación publicada. [^3_5][^3_4] | Si quieres un panel-terminal dentro de tu layout Bubble Tea sin inventarte la capa de emulación. [^3_1] |
| `BigJk/crt` | Emulador mínimo de terminal orientado a Bubble Tea. [^3_6][^3_7] | Media-alta: se acopla a `tea.Program`, pero su implementación usa `ebiten`, así que el modelo de render no es el típico componente string-only de Bubble Tea. [^3_8][^3_5] | Enfoque directo, pensado específicamente para Bubble Tea; útil si aceptas su backend gráfico. [^3_6][^3_5] | Menos “nativo TUI” en el sentido clásico; depende de `ebiten` y no de un render textual puro. [^3_8] | Si quieres algo funcional rápido y no te importa salirte un poco del patrón tradicional de Bubbles. [^3_6][^3_8] |
| `creack/pty` | Librería PTY para lanzar y controlar procesos con pseudo-terminal en Unix. [^3_9][^3_3] | Baja por sí sola: no renderiza terminal ni interpreta ANSI; solo gestiona el PTY/proceso. [^3_9] | Muy usada y madura; buena base para shells, `vim`, `htop`, `less`, etc. [^3_3] | Necesitas añadir por encima emulación, parser ANSI, pantalla, input mapping y render. [^3_9][^3_1] | Si quieres construir tu propio terminal embebido o combinarla con otra librería de emulación. [^3_9] |

## Lectura rápida

Si buscas la opción más natural para una app ya hecha con Bubbles, `bubbleterm` es la candidata más clara ahora mismo. [^3_1][^3_2] Si priorizas una base robusta de PTY para montar tu propia solución, `creack/pty` es la pieza de infraestructura correcta, pero no te resuelve la vista de terminal por sí sola. [^3_9][^3_3]

`crt` queda en medio: sirve para el caso Bubble Tea, pero su enfoque técnico es distinto del patrón habitual de componentes textuales de Bubbles. [^3_8][^3_5]

## Recomendación

Para un proyecto nuevo, haría esta elección:

- `bubbleterm` si quieres velocidad de integración y una API tipo Bubble Tea. [^3_1]
- `creack/pty` si necesitas máximo control y no te importa implementar más capas. [^3_9][^3_3]
- `crt` solo si su modelo de render con `ebiten` te encaja bien desde el principio. [^3_8]

Si quieres, te hago la siguiente tabla comparando ya con criterios más de ingeniería: **mantenimiento, madurez, complejidad de integración, compatibilidad con apps interactivas (`vim`, `tmux`, `lazygit`) y riesgo técnico**.
<span style="display:none">[^3_10][^3_11][^3_12][^3_13][^3_14][^3_15][^3_16][^3_17]</span>

<div align="center">⁂</div>

[^3_1]: https://pkg.go.dev/github.com/taigrr/bubbleterm

[^3_2]: https://github.com/taigrr/bubbleterm

[^3_3]: https://github.com/creack/pty

[^3_4]: https://libraries.io/go/github.com%2Ftaigrr%2Fbubbleterm

[^3_5]: https://github.com/charmbracelet/bubbletea

[^3_6]: https://github.com/BigJk/crt

[^3_7]: https://github.com/BigJk/crt/labels

[^3_8]: https://github.com/BigJk/crt/blob/main/crt.go

[^3_9]: https://pkg.go.dev/github.com/creack/pty

[^3_10]: https://pkg.go.dev/github.com/taigrr/bubbleterm/cmd/multiwindow

[^3_11]: https://github.com/napisani/proctmux/blob/main/go.mod

[^3_12]: https://github.com/taigrr

[^3_13]: https://github.com/creack/pty/releases

[^3_14]: https://github.com/dlvhdr

[^3_15]: https://github.com/BigJk/crt/issues

[^3_16]: https://github.com/taigrr/taigrr

[^3_17]: https://github.com/BigJk/crt/pulls


---

# he integrado bubbluterm pero se cuelga la aplicación cuando intento cargar un terminal, luego hice cambios para que no se colgase pero no aparecía el shell en la view donde cargo el terminal

Lo que describes suena a dos fallos distintos: primero, el terminal se queda bloqueado porque el loop de Bubble Tea no está recibiendo o propagando bien las actualizaciones del `bubbleterm`; después, al evitar el bloqueo, el proceso puede arrancar pero no se está renderizando en la vista correcta o no tiene tamaño/foco válido para dibujarse. [^4_1][^4_2][^4_3]

## Posibles causas

`bubbleterm` documenta un patrón muy concreto: crear el terminal con `NewWithCommand(width, height, cmd)`, devolver `m.terminal.Init()` en `Init()`, reenviar los mensajes a `m.terminal.Update(msg)` dentro de `Update()`, y devolver `m.terminal.View()` en `View()`. [^4_1][^4_2] Si una de esas piezas falta, el proceso puede existir pero la pantalla no actualizarse, o quedarse esperando eventos que nunca llegan. [^4_1]

Además, la librería expone `SetAutoPoll(false)` y `UpdateTerminal()` para polling manual, lo que implica que si desactivaste el auto-poll y no llamas periódicamente a `UpdateTerminal()`, el shell puede estar ejecutándose pero no generar repaints visibles. [^4_1][^4_2] Eso encaja bastante con “ya no se cuelga, pero no aparece nada”. [^4_1]

## Qué revisaría

- Asegúrate de que el terminal se crea con ancho y alto reales, no con `0x0` ni con dimensiones aún no inicializadas. [^4_1][^4_3]
- Propaga `tea.WindowSizeMsg` al terminal y llama a `Resize(width, height)` cuando cambie el layout, porque Bubble Tea basa mucho del render en ese mensaje y hay casos documentados donde el tamaño no se refresca bien si no lo reinyectas al loop. [^4_1][^4_3]
- Si usas foco entre panes, confirma que el panel del terminal está en `Focus()` cuando esperas que reciba input. [^4_1]
- Si cambiaste a polling manual, ejecuta `UpdateTerminal()` con un ticker; si no, deja `SetAutoPoll(true)` o no lo toques. [^4_1][^4_2]
- Verifica que en tu `View()` principal realmente concatenas o enrutas `m.terminal.View()` dentro del pane visible, porque si la vista activa devuelve otra cosa, el shell puede estar vivo pero fuera del árbol renderizado. [^4_1]


## Patrón mínimo

La documentación de `bubbleterm` propone algo conceptualmente así: crear el comando, construir el terminal, usar `Init()` del terminal, reenviar todos los `msg` al terminal en `Update()`, y renderizar con `View()`. [^4_1][^4_2] También expone `StartCommand`, `Resize`, `SendInput` y `UpdateTerminal`, así que cualquier integración que reemplace ese flujo base necesita seguir alimentando esos puntos de entrada. [^4_1]

Un fallo muy típico en layouts con Bubbles es crear el terminal antes de conocer el tamaño final del pane, con lo que se inicializa pequeño o vacío, y luego no hacer `Resize()` cuando llega `tea.WindowSizeMsg`. [^4_3][^4_4] En Bubble Tea, el tamaño correcto suele fijarse al recibir `tea.WindowSizeMsg`, no antes. [^4_3][^4_4]

## Diagnóstico rápido

Haz estas comprobaciones en orden:

1. El comando existe y arranca, por ejemplo `/bin/bash`, `zsh` o `sh`, no uno interactivo raro que falle por entorno. [^4_1]
2. El terminal recibe `Init()` y luego `Update()` en cada mensaje del programa. [^4_1]
3. El pane tiene tamaño mayor que cero y llamas a `Resize()` cuando conoces ese tamaño. [^4_1][^4_3]
4. No has desactivado el auto-poll sin sustituirlo por ticks que llamen a `UpdateTerminal()`. [^4_1]
5. El `View()` del pane visible devuelve realmente `m.terminal.View()`. [^4_1]

## Lo más probable

Por cómo lo cuentas, apostaría más por una de estas dos:

- Desactivaste el bloqueo, pero dejaste el terminal sin polling o sin `Init()`, así que el proceso no refresca la pantalla. [^4_1][^4_2]
- El shell sí arranca, pero el componente tiene tamaño `0`, no recibe `Resize()`, o su `View()` no está siendo insertada en el pane activo. [^4_1][^4_3]

Pégame el `model`, `Init`, `Update` y `View` de esa pantalla, más el punto donde creas `bubbleterm`, y te digo exactamente dónde está el problema.
<span style="display:none">[^4_10][^4_11][^4_12][^4_13][^4_14][^4_15][^4_16][^4_5][^4_6][^4_7][^4_8][^4_9]</span>

<div align="center">⁂</div>

[^4_1]: https://pkg.go.dev/github.com/taigrr/bubbleterm

[^4_2]: https://libraries.io/go/github.com%2Ftaigrr%2Fbubbleterm

[^4_3]: https://github.com/charmbracelet/bubbletea

[^4_4]: https://gist.github.com/meowgorithm/8631017bddd592e4336bba4576bbfde5

[^4_5]: https://pkg.go.dev/github.com/taigrr/bubbleterm/emulator

[^4_6]: https://gitlab.com/gnachman/iterm2/-/issues/6828

[^4_7]: https://finance.yahoo.com/news/assessing-shell-lse-shel-valuation-031108957.html

[^4_8]: https://byteiota.com/bubble-tea-v2-10x-faster-terminal-uis-for-go-developers/

[^4_9]: https://github.com/charmbracelet/bubbletea/discussions/661

[^4_10]: https://patches.guix-patches.cbaines.net/project/guix-patches/patch/20240902121843.31430-28-herman@rimm.ee/

[^4_11]: https://www.ndtv.com/health/kidney-stone-to-fatty-liver-study-says-bubble-boba-tea-raises-all-sorts-of-health-risks-11162065?pfrom=home-ndtv_health

[^4_12]: https://groups.google.com/g/linux.debian.devel/c/Q8jqTbyhDAw

[^4_13]: https://stackoverflow.com/questions/79202221/terminal-not-reciving-input-when-makepkg-is-called-and-asks-for-sudo-using-bubb

[^4_14]: https://github.com/charmbracelet/bubbletea/commit/7d708384a105005dfbcec2290bfe4ea1d0e8d9f0

[^4_15]: https://www.eclipse.org/forums/index.php/t/1105662/

[^4_16]: https://e.vnexpress.net/news/business/companies/banker-left-seven-figure-job-to-sell-bubble-tea-now-his-company-makes-500m-a-year-5044789.html


---

# Ejemplo código completo para integrar bubbleterm en Bubble Tea

Sí. Este es un ejemplo **completo y mínimo** para integrar `bubbleterm` dentro de una app Bubble Tea, siguiendo la API documentada por la librería: crear el terminal con `NewWithCommand`, reenviar mensajes con `Update`, inicializar con `Init`, redimensionar con `Resize`, y renderizar con `View`. [^5_1][^5_2]

## Ejemplo completo

La documentación de `bubbleterm` muestra que el flujo correcto es `NewWithCommand(width, height, cmd)`, `m.terminal.Init()`, `m.terminal.Update(msg)` y `m.terminal.View()`. [^5_1] También expone `Resize`, `Focus`, `Blur`, `Close`, `StartCommand` y `UpdateTerminal`, que sirven para una integración más robusta. [^5_1]

```go
package main

import (
	"fmt"
	"os"
	"os/exec"

	bubbleterm "github.com/taigrr/bubbleterm"
	tea "github.com/charmbracelet/bubbletea"
)

type model struct {
	terminal *bubbleterm.Model
	width    int
	height   int
	ready    bool
	err      error
}

func newModel() model {
	return model{}
}

func (m model) Init() tea.Cmd {
	// Esperamos a WindowSizeMsg para crear el terminal con tamaño real.
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if !m.ready {
			cmd := exec.Command("bash")
			cmd.Env = append(os.Environ(),
				"TERM=xterm-256color",
			)

			term, err := bubbleterm.NewWithCommand(m.width, m.height, cmd)
			if err != nil {
				m.err = err
				return m, nil
			}

			m.terminal = term
			m.terminal.Focus()
			m.ready = true

			// Init es parte del flujo documentado por bubbleterm.
			return m, m.terminal.Init()
		}

		if m.terminal != nil {
			return m, m.terminal.Resize(m.width, m.height)
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.terminal != nil {
				_ = m.terminal.Close()
			}
			return m, tea.Quit
		}
	}

	if m.terminal != nil {
		terminalModel, cmd := m.terminal.Update(msg)
		if t, ok := terminalModel.(*bubbleterm.Model); ok {
			m.terminal = t
		}
		return m, cmd
	}

	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("error creando terminal: %v\n", m.err)
	}

	if !m.ready || m.terminal == nil {
		return "inicializando terminal..."
	}

	return m.terminal.View()
}

func main() {
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
```


## Por qué funciona

Este ejemplo retrasa la creación del terminal hasta recibir `tea.WindowSizeMsg`, que en Bubble Tea es el momento fiable para conocer el tamaño real de la pantalla o del pane. [^5_3][^5_1] Eso evita uno de los problemas más comunes: crear el terminal con tamaño `0x0` y luego no ver nada en pantalla. [^5_1]

Además, reenvía **todos** los mensajes a `m.terminal.Update(msg)` después de manejar los globales como resize o quit, que es justo el patrón que la documentación enseña para que el terminal procese input, repaints y eventos internos. [^5_1][^5_2]

## Versión con layout

Si quieres meter el terminal en un pane y no a pantalla completa, tienes que crear y redimensionar `bubbleterm` con el tamaño del pane, no con el tamaño completo de la terminal. [^5_1] En ese caso, el `View()` de tu modelo principal debe insertar `m.terminal.View()` dentro del layout visible, porque si devuelves otra vista, el shell puede estar corriendo pero no dibujarse donde esperas. [^5_1]

Un patrón típico sería calcular `leftWidth`, `rightWidth` y `contentHeight`, y luego hacer `m.terminal.Resize(rightWidth, contentHeight)` cuando llegue `WindowSizeMsg`. [^5_1][^5_3]

## Errores típicos

- Crear `bubbleterm` antes de tener tamaño real. [^5_1]
- No llamar a `m.terminal.Init()`. [^5_1]
- No reenviar `msg` a `m.terminal.Update(msg)`. [^5_1]
- No hacer `Resize()` cuando cambia el tamaño. [^5_1]
- Cerrar el cuelgue desactivando el polling, pero olvidar `UpdateTerminal()` si has usado `SetAutoPoll(false)`. [^5_1]


## Variante recomendada

Si quieres algo más estable para empezar, usa `sh` o `bash` sencillo antes de probar `zsh`, `vim`, `htop` o `lazygit`, porque así reduces variables durante el diagnóstico. [^5_1] La propia documentación usa `htop` como ejemplo de integración, lo que confirma que la intención de la librería es soportar procesos interactivos dentro de Bubble Tea. [^5_2][^5_4]

Puedo darte ahora una segunda versión más realista con:

- header + footer,
- terminal en panel central,
- foco entre panes,
- resize correcto,
- y ticker manual por si quieres usar `SetAutoPoll(false)`.
<span style="display:none">[^5_10][^5_11][^5_12][^5_13][^5_14][^5_15][^5_5][^5_6][^5_7][^5_8][^5_9]</span>

<div align="center">⁂</div>

[^5_1]: https://pkg.go.dev/github.com/taigrr/bubbleterm

[^5_2]: https://libraries.io/go/github.com%2Ftaigrr%2Fbubbleterm

[^5_3]: https://pkg.go.dev/charm.land/bubbletea/v2

[^5_4]: https://github.com/taigrr/bubbleterm

[^5_5]: https://pkg.go.dev/github.com/taigrr/bubbleterm/emulator

[^5_6]: https://libraries.io/go/github.com%2FIgnoramuss%2Fbubbleterm

[^5_7]: https://github.com/charmbracelet/bubbletea

[^5_8]: https://awesomegolibs.com/library/bubbletea

[^5_9]: https://www.reddit.com/r/KittyTerminal/comments/1gufc7g/developing_a_terminal_app_in_go_with_bubble_tea/

[^5_10]: https://www.reddit.com/r/developersIndia/comments/1qcp5rl/i_built_a_real_web_terminal_that_actually_works/

[^5_11]: https://speakerdeck.com/abtris/build-nice-terminal-ui-with-bubble-tea

[^5_12]: https://pkg.go.dev/github.com/charmbracelet/bubbletea

[^5_13]: https://itsfoss.com/htop-alternatives/

[^5_14]: https://gist.github.com/mdamien/e9933c7c75df64b09ed54504af411566

[^5_15]: https://dev.to/andyhaskell/intro-to-bubble-tea-in-go-21lg


---

# Ejemplo avanzado con soporte de resize dinámico

Sí. Este ejemplo es más **avanzado**: usa `bubbleterm` en un layout con header, footer y panel central; soporta resize dinámico; crea el terminal solo cuando ya conoce el tamaño útil; y además usa polling manual con `SetAutoPoll(false)` + `UpdateTerminal()` para evitar bloqueos y tener más control. [^6_1][^6_2]

## Código completo

La API documentada por `bubbleterm` incluye `NewWithCommand`, `Init`, `Update`, `View`, `Resize`, `Focus`, `Blur`, `SetAutoPoll(false)` y `UpdateTerminal()`, así que este ejemplo se apoya exactamente en esas piezas. [^6_1][^6_2]

```go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	bubbleterm "github.com/taigrr/bubbleterm"
	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type model struct {
	width  int
	height int

	headerHeight int
	footerHeight int

	termX int
	termY int
	termW int
	termH int

	terminal *bubbleterm.Model
	ready    bool
	err      error
}

func initialModel() model {
	return model{
		headerHeight: 1,
		footerHeight: 2,
	}
}

func (m model) Init() tea.Cmd {
	return tickCmd()
}

func (m *model) contentSize() (int, int) {
	w := m.width
	h := m.height - m.headerHeight - m.footerHeight
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

func (m *model) ensureTerminal() tea.Cmd {
	if m.ready || m.termW <= 0 || m.termH <= 0 {
		return nil
	}

	cmd := exec.Command("bash")
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
	)

	term, err := bubbleterm.NewWithCommand(m.termW, m.termH, cmd)
	if err != nil {
		m.err = err
		return nil
	}

	term.SetAutoPoll(false)
	term.Focus()

	m.terminal = term
	m.ready = true

	return tea.Batch(
		m.terminal.Init(),
		m.terminal.UpdateTerminal(),
	)
}

func (m *model) resizeLayout() tea.Cmd {
	m.termX = 0
	m.termY = m.headerHeight

	m.termW, m.termH = m.contentSize()

	if m.ready && m.terminal != nil {
		return m.terminal.Resize(m.termW, m.termH)
	}

	return m.ensureTerminal()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.resizeLayout()

	case tickMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, tickCmd())

		if m.ready && m.terminal != nil {
			cmds = append(cmds, m.terminal.UpdateTerminal())
		}

		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.terminal != nil {
				_ = m.terminal.Close()
			}
			return m, tea.Quit

		case "ctrl+l":
			if m.ready && m.terminal != nil {
				return m, m.terminal.SendInput("clear\n")
			}

		case "ctrl+r":
			if m.ready && m.terminal != nil {
				_ = m.terminal.Close()
				m.terminal = nil
				m.ready = false
				return m, m.ensureTerminal()
			}
		}
	}

	if m.ready && m.terminal != nil {
		terminalModel, cmd := m.terminal.Update(msg)
		if t, ok := terminalModel.(*bubbleterm.Model); ok {
			m.terminal = t
		}
		return m, cmd
	}

	return m, nil
}

func (m model) headerView() string {
	title := " Bubbleterm demo "
	size := fmt.Sprintf(" %dx%d ", m.width, m.height)
	line := title + size
	if len(line) < m.width {
		line += spaces(m.width - len(line))
	}
	return trimWidth(line, m.width)
}

func (m model) footerView() string {
	line1 := "q/ctrl+c salir • ctrl+l clear • ctrl+r reiniciar shell"
	line2 := "resize dinámico activo"
	if m.ready && m.terminal != nil && m.terminal.Focused() {
		line2 = "resize dinámico activo • terminal con foco"
	}
	return trimWidth(padRight(line1, m.width), m.width) + "\n" +
		trimWidth(padRight(line2, m.width), m.width)
}

func (m model) bodyView() string {
	if m.err != nil {
		return padBlock(fmt.Sprintf("error: %v", m.err), m.termW, m.termH)
	}

	if !m.ready || m.terminal == nil {
		return padBlock("inicializando terminal...", m.termW, m.termH)
	}

	return padBlock(m.terminal.View(), m.termW, m.termH)
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "esperando tamaño de terminal..."
	}

	body := m.bodyView()

	return m.headerView() + "\n" + body + "\n" + m.footerView()
}

func padBlock(s string, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	lines := splitLines(s)
	out := make([]string, 0, height)

	for i := 0; i < height; i++ {
		if i < len(lines) {
			out = append(out, trimWidth(padRight(lines[i], width), width))
		} else {
			out = append(out, spaces(width))
		}
	}

	return joinLines(out)
}

func splitLines(s string) []string {
	lines := make([]string, 0)
	cur := ""
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	lines = append(lines, cur)
	return lines
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	out := lines[^6_0]
	for i := 1; i < len(lines); i++ {
		out += "\n" + lines[i]
	}
	return out
}

func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len([]rune(s)) >= width {
		return trimWidth(s, width)
	}
	return s + spaces(width-len([]rune(s)))
}

func trimWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width])
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]rune, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
```


## Qué hace bien

Este patrón evita crear el shell antes de conocer el tamaño útil del área central, que es uno de los problemas más comunes en Bubble Tea cuando un componente depende de `tea.WindowSizeMsg`. [^6_3][^6_1] Además, cada resize recalcula el layout y llama a `terminal.Resize(...)`, que es exactamente la operación que `bubbleterm` expone para adaptar las dimensiones del emulador. [^6_1]

También usa polling manual porque la documentación de `bubbleterm` indica que, si desactivas auto-polling con `SetAutoPoll(false)`, debes llamar a `UpdateTerminal()` desde un ticker externo. [^6_1][^6_2] Esto te da más control en integraciones complejas y ayuda a evitar estados donde el proceso existe pero la view no se repinta. [^6_1]

## Puntos críticos

En Bubble Tea, el tamaño correcto llega por `tea.WindowSizeMsg`, así que no conviene inicializar el terminal en `Init()` con tamaños ficticios. [^6_3][^6_4] En casos donde el terminal externo cambia de tamaño y no se propaga bien, la comunidad de Bubble Tea también ha usado un ticker que reinyecta eventos de resize para forzar actualización coherente del layout. [^6_5]

Si el shell no aparece, revisa cuatro cosas: que `View()` principal realmente incluya `m.terminal.View()`, que el panel tenga ancho y alto mayores que cero, que el terminal reciba `Update(msg)` para todos los mensajes, y que el polling manual esté activo si has llamado a `SetAutoPoll(false)`. [^6_1][^6_2]

## Ajustes útiles

Puedes cambiar `exec.Command("bash")` por `exec.Command("zsh")` o por una app como `htop`, porque `bubbleterm` está pensado para ejecutar procesos interactivos dentro de un bubble. [^6_2][^6_6] Si quieres integrarlo en un layout más serio, una opción complementaria es usar un gestor de layout como `bubblelayout`, que traduce `tea.WindowSizeMsg` a tamaños exactos por panel. [^6_7]

Pásame tu código actual y te lo devuelvo adaptado a este patrón, con tus panes y navegación.
<span style="display:none">[^6_10][^6_11][^6_12][^6_13][^6_14][^6_15][^6_16][^6_8][^6_9]</span>

<div align="center">⁂</div>

[^6_1]: https://pkg.go.dev/github.com/taigrr/bubbleterm

[^6_2]: https://libraries.io/go/github.com%2Ftaigrr%2Fbubbleterm

[^6_3]: https://github.com/charmbracelet/bubbletea/issues/987

[^6_4]: https://pkg.go.dev/charm.land/bubbletea/v2

[^6_5]: https://github.com/charmbracelet/bubbletea/discussions/661

[^6_6]: https://github.com/taigrr/bubbleterm

[^6_7]: https://github.com/winder/bubblelayout

[^6_8]: https://libraries.io/go/github.com%2FIgnoramuss%2Fbubbleterm

[^6_9]: https://pkg.go.dev/github.com/taigrr/bubbleterm/emulator

[^6_10]: https://github.com/charmbracelet/bubbletea/discussions/544

[^6_11]: https://www.youtube.com/watch?v=Gl31diSVP8M

[^6_12]: https://github.sundayhk.com/charmbracelet/bubbles/

[^6_13]: https://github.com/charmbracelet/bubbletea/issues/1019

[^6_14]: https://docs.rs/bubbletea-rs/latest/bubbletea_rs/

[^6_15]: https://awesome.ecosyste.ms/projects/github.com%2Fcharmbracelet%2Fbubbles

[^6_16]: https://substack.com/home/post/p-152418733

