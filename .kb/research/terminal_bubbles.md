<img src="https://r2cdn.perplexity.ai/pplx-full-logo-primary-dark%402x.png" style="height:64px;margin-right:32px"/>

# I'm looking for a library to integrate a terminal view into a TUI app made with charmbracelet bubbles

Yes: the most direct option I've found for **integrating a terminal view** with a Bubble Tea/Bubbles-based app is `github.com/BigJk/crt`, because it's designed exactly as a "minimal terminal emulator for Bubbletea" and can be attached to a `tea.Program` via its Bubble Tea adapter. [^1_1]

## Main option

`BigJk/crt` is described as a library that provides a simple terminal emulator that can be attached to `tea.Program`, with support for TrueColor, mouse and keyboard, and interpretation of CSI sequences used by Bubble Tea. [^1_1] Additionally, its README shows an example usage with `bubbleadapter.Window(...)`, where you pass your `tea.Model` and it renders it in its own window. [^1_1]

This fits well if what you're looking for isn't just a text panel, but a "real" embedded terminal for a TUI/terminal-like app. [^1_1]

## Important nuance

If by "terminal view" you mean a scrollable area to display ANSI output or logs, `bubbles/viewport` exists and serves to render a viewport within Bubble Tea, with programmatic vertical and horizontal scrolling. [^1_2][^1_3] But `viewport` is not a terminal emulator nor does it run PTY/shell; it's a display component within Bubble Tea. [^1_3]

On the other hand, `crt` does target the case of terminal emulation/rendering associated with Bubble Tea, although it uses `ebitengine` for drawing and its own README warns of limitations: not all CSI sequences are implemented and key mapping can still be somewhat irregular. [^1_1]

## Practical recommendation

I would say:

- Use `BigJk/crt` if you want a truly embedded terminal within the Bubble Tea ecosystem. [^1_1]
- Use `bubbles/viewport` if you only need a panel with scrollable content, like logs, already-processed command output, or limited ANSI text. [^1_2][^1_3]


## Integration with Bubbles

Bubble Tea is the base framework, and Bubbles provides reusable components like `viewport`, lists, inputs, and tables, so your natural architecture would be to continue with Bubble Tea/Bubbles for the UI and add `crt` only in the area where you need the embedded terminal. [^1_1][^1_3][^1_1]

If you want, I can now propose a concrete architecture for **Bubble Tea + Bubbles + real PTY** with a Go example to embed a shell inside your TUI.
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

# Any other library to embed a terminal in a Go TUI?

Yes: besides `crt`, a pretty clear alternative today is `github.com/taigrr/bubbleterm`, which defines itself as a headless and embeddable terminal emulator in Go, with output compatible with Bubble Tea. [^2_1]

## Useful libraries

`bubbleterm` seems more aligned with your case than `crt` if you want to stay within a traditional TUI, because it doesn't render with `ebitengine`, but instead exposes a `Model` for Bubble Tea and is designed to run commands inside a "terminal bubble". [^2_1] Its README indicates support for ANSI parsing, screen state, 256 colors and true color, scrollback, keyboard, mouse, resize, and compatibility with `xterm-256color`. [^2_1]

`crt`, by contrast, also serves to couple a terminal to Bubble Tea, but its approach is more of a window rendered with `ebitengine`, not so much a native TUI component rendered as a string within Bubble Tea's view tree. [^2_2]

## Key differences

| Library | Approach | Integration with Bubble Tea |
| :-- | :-- | :-- |
| `taigrr/bubbleterm` | Embeddable headless emulator; Bubble component; designed for PTY + TUI. [^2_1] | Very good if you want a terminal panel inside your Bubble Tea layout. [^2_1] |
| `BigJk/crt` | Simple emulator coupled to `tea.Program`, rendered with `ebitengine`. [^2_2] | Useful if you accept that different graphics/render backend. [^2_2] |
| `creack/pty` / `danielgatis/go-pty` | PTY/process management, not complete terminal emulation by itself. [^2_3][^2_1] | Serves as a base, but you need an emulation/rendering layer on top. [^2_3][^2_1] |

## My recommendation

If your app is already built with Bubbles and you want a clean integration, I would try `bubbleterm` first. [^2_1] In fact, its documentation includes `NewWithCommand`, `Update`, `View`, `Resize`, `Focus`, `SendInput`, and an explicit example of integrating `htop` within Bubble Tea, which is exactly the type of API that usually fits well in a layout with panes. [^2_1]

If what you want is to build the stack yourself, then the most flexible combination would be `creack/pty` or `go-pty` for the process/PTY and an emulation library like `bubbleterm` on top. [^2_3][^2_1]

## Limitations

In `bubbleterm`, current limitations are documented: there's no complete damage tracking, it may redraw the entire screen on each frame, and character erasing may fail in some cases; they even mention that running `tmux` inside the emulator mitigates some of those issues. [^2_1] This suggests it's very promising, but it's still worth validating with real cases like `bash`, `zsh`, `vim`, `less`, `htop`, or `lazygit`. [^2_1]

If you want, I can prepare a more technical comparison between `bubbleterm`, `crt`, and building `pty + vt10x/tcell` to choose based on your case.
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

# Make me a comparison table

Yes. Here's a table designed to choose among the most reasonable options for embedding a terminal in a Go TUI, especially if you're already in Bubble Tea. [^3_1][^3_2][^3_3]

## Table

| Option | What it is | Integration with Bubble Tea | Pros | Cons | When to use it |
| :-- | :-- | :-- | :-- | :-- | :-- |
| `taigrr/bubbleterm` | Headless and embeddable terminal emulator, with Bubble Tea-compatible output. [^3_1][^3_2] | Very high: exposes `Init`, `Update`, `View`, `Resize`, `SendInput`, `StartCommand` and direct Bubble Tea example. [^3_1] | Designed for this case; supports ANSI, UTF-8, 256/true color, scrollback, keyboard, mouse and resize. [^3_1][^3_4] | Young project; the docs themselves mention redrawing/damage tracking limitations depending on the published implementation state. [^3_5][^3_4] | If you want a terminal panel inside your Bubble Tea layout without building the emulation layer yourself. [^3_1] |
| `BigJk/crt` | Minimal terminal emulator oriented toward Bubble Tea. [^3_6][^3_7] | Medium-high: couples to `tea.Program`, but its implementation uses `ebiten`, so the render model isn't the typical string-only Bubble Tea component. [^3_8][^3_5] | Direct approach, specifically designed for Bubble Tea; useful if you accept its graphics backend. [^3_6][^3_5] | Less "TUI native" in the classic sense; depends on `ebiten` rather than pure textual rendering. [^3_8] | If you want something functional quickly and don't mind deviating a bit from the traditional Bubbles pattern. [^3_6][^3_8] |
| `creack/pty` | PTY library to launch and control processes with pseudo-terminal on Unix. [^3_9][^3_3] | Low on its own: doesn't render terminal or interpret ANSI; only manages the PTY/process. [^3_9] | Very widely used and mature; good base for shells, `vim`, `htop`, `less`, etc. [^3_3] | You need to add emulation, ANSI parser, screen, input mapping and rendering on top. [^3_9][^3_1] | If you want to build your own embedded terminal or combine it with another emulation library. [^3_9] |

## Quick read

If you're looking for the most natural option for an app already built with Bubbles, `bubbleterm` is the clearest candidate right now. [^3_1][^3_2] If you prioritize a robust PTY base to build your own solution, `creack/pty` is the right infrastructure piece, but it doesn't solve the terminal view by itself. [^3_9][^3_3]

`crt` falls in between: it works for the Bubble Tea case, but its technical approach differs from the usual textual component pattern of Bubbles. [^3_8][^3_5]

## Recommendation

For a new project, I would make this choice:

- `bubbleterm` if you want integration speed and a Bubble Tea-style API. [^3_1]
- `creack/pty` if you need maximum control and don't mind implementing more layers. [^3_9][^3_3]
- `crt` only if its `ebiten` render model fits well from the start. [^3_8]

If you want, I can make the next table comparing with more engineering criteria: **maintenance, maturity, integration complexity, compatibility with interactive apps (`vim`, `tmux`, `lazygit`), and technical risk**.
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

# I integrated bubbluterm but the app hangs when I try to load a terminal, then I made changes so it wouldn't hang but the shell doesn't appear in the view where I load the terminal

What you describe sounds like two different failures: first, the terminal gets blocked because the Bubble Tea loop isn't receiving or propagating the `bubbleterm` updates properly; then, after avoiding the blockage, the process may start but isn't rendering in the correct view or doesn't have a valid size/focus to draw itself. [^4_1][^4_2][^4_3]

## Possible causes

`bubbleterm` documents a very specific pattern: create the terminal with `NewWithCommand(width, height, cmd)`, return `m.terminal.Init()` in `Init()`, forward messages to `m.terminal.Update(msg)` within `Update()`, and return `m.terminal.View()` in `View()`. [^4_1][^4_2] If any of those pieces is missing, the process may exist but the screen doesn't update, or it stays waiting for events that never arrive. [^4_1]

Additionally, the library exposes `SetAutoPoll(false)` and `UpdateTerminal()` for manual polling, which means if you disabled auto-poll and don't periodically call `UpdateTerminal()`, the shell may be running but not generate visible repaints. [^4_1][^4_2] This fits quite well with "it no longer hangs, but nothing appears". [^4_1]

## What I would check

- Make sure the terminal is created with actual width and height, not with `0x0` or with dimensions not yet initialized. [^4_1][^4_3]
- Forward `tea.WindowSizeMsg` to the terminal and call `Resize(width, height)` when the layout changes, because Bubble Tea bases much of its rendering on that message and there are documented cases where the size doesn't refresh properly if you don't re-inject it into the loop. [^4_1][^4_3]
- If you use focus between panes, confirm that the terminal panel is in `Focus()` when you expect it to receive input. [^4_1]
- If you switched to manual polling, run `UpdateTerminal()` with a ticker; otherwise, leave `SetAutoPoll(true)` or don't touch it. [^4_1][^4_2]
- Verify that in your main `View()` you're actually concatenating or routing `m.terminal.View()` within the visible pane, because if the active view returns something else, the shell may be alive but outside the render tree. [^4_1]


## Minimum pattern

The `bubbleterm` documentation proposes something conceptually like: create the command, build the terminal, use the terminal's `Init()`, forward all `msg` to the terminal in `Update()`, and render with `View()`. [^4_1][^4_2] It also exposes `StartCommand`, `Resize`, `SendInput`, and `UpdateTerminal`, so any integration that replaces that base flow needs to keep feeding those entry points. [^4_1]

A very typical failure in Bubbles layouts is creating the terminal before knowing the final pane size, so it initializes small or empty, and then not calling `Resize()` when `tea.WindowSizeMsg` arrives. [^4_3][^4_4] In Bubble Tea, the correct size is usually set upon receiving `tea.WindowSizeMsg`, not before. [^4_3][^4_4]

## Quick diagnosis

Do these checks in order:

1. The command exists and starts, for example `/bin/bash`, `zsh` or `sh`, not some weird interactive one that fails due to the environment. [^4_1]
2. The terminal receives `Init()` and then `Update()` on every program message. [^4_1]
3. The pane has a size greater than zero and you call `Resize()` when you know that size. [^4_1][^4_3]
4. You haven't disabled auto-poll without replacing it with ticks that call `UpdateTerminal()`. [^4_1]
5. The visible pane's `View()` actually returns `m.terminal.View()`. [^4_1]

## Most likely

Based on how you describe it, I would bet more on one of these two:

- You disabled the blocking, but left the terminal without polling or without `Init()`, so the process doesn't refresh the screen. [^4_1][^4_2]
- The shell does start, but the component has size `0`, doesn't receive `Resize()`, or its `View()` isn't being inserted into the active pane. [^4_1][^4_3]

Paste me the `model`, `Init`, `Update` and `View` from that screen, plus where you create `bubbleterm`, and I'll tell you exactly where the problem is.
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

# Complete code example to integrate bubbleterm in Bubble Tea

Yes. This is a **complete and minimal** example for integrating `bubbleterm` within a Bubble Tea app, following the API documented by the library: create the terminal with `NewWithCommand`, forward messages with `Update`, initialize with `Init`, resize with `Resize`, and render with `View`. [^5_1][^5_2]

## Complete example

The `bubbleterm` documentation shows that the correct flow is `NewWithCommand(width, height, cmd)`, `m.terminal.Init()`, `m.terminal.Update(msg)` and `m.terminal.View()`. [^5_1] It also exposes `Resize`, `Focus`, `Blur`, `Close`, `StartCommand` and `UpdateTerminal`, which serve for a more robust integration. [^5_1]

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
	// We wait for WindowSizeMsg to create the terminal with real size.
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

			// Init is part of the flow documented by bubbleterm.
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
		return fmt.Sprintf("error creating terminal: %v\n", m.err)
	}

	if !m.ready || m.terminal == nil {
		return "initializing terminal..."
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


## Why it works

This example delays terminal creation until receiving `tea.WindowSizeMsg`, which in Bubble Tea is the reliable moment to know the real screen or pane size. [^5_3][^5_1] This avoids one of the most common problems: creating the terminal with size `0x0` and then seeing nothing on screen. [^5_1]

Additionally, it forwards **all** messages to `m.terminal.Update(msg)` after handling global ones like resize or quit, which is exactly the pattern that the documentation teaches for the terminal to process input, repaints and internal events. [^5_1][^5_2]

## Layout version

If you want to put the terminal in a pane and not full screen, you need to create and resize `bubbleterm` with the pane size, not the full terminal size. [^5_1] In that case, your main model's `View()` must insert `m.terminal.View()` within the visible layout, because if you return a different view, the shell may be running but not drawing where you expect. [^5_1]

A typical pattern would be to calculate `leftWidth`, `rightWidth` and `contentHeight`, and then do `m.terminal.Resize(rightWidth, contentHeight)` when `WindowSizeMsg` arrives. [^5_1][^5_3]

## Common errors

- Creating `bubbleterm` before having real size. [^5_1]
- Not calling `m.terminal.Init()`. [^5_1]
- Not forwarding `msg` to `m.terminal.Update(msg)`. [^5_1]
- Not calling `Resize()` when size changes. [^5_1]
- Fixing the hang by disabling polling, but forgetting `UpdateTerminal()` if you used `SetAutoPoll(false)`. [^5_1]


## Recommended variant

If you want something more stable to start with, use `sh` or simple `bash` before trying `zsh`, `vim`, `htop` or `lazygit`, because that reduces variables during diagnosis. [^5_1] The documentation itself uses `htop` as an integration example, which confirms that the library's intention is to support interactive processes within Bubble Tea. [^5_2][^5_4]

I can now give you a second, more realistic version with:

- header + footer,
- terminal in center panel,
- focus between panes,
- correct resize,
- and manual ticker in case you want to use `SetAutoPoll(false)`.
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

# Advanced example with dynamic resize support

Yes. This example is more **advanced**: it uses `bubbleterm` in a layout with header, footer and center panel; supports dynamic resize; creates the terminal only when it knows the usable size; and also uses manual polling with `SetAutoPoll(false)` + `UpdateTerminal()` to avoid blocking and have more control. [^6_1][^6_2]

## Complete code

The API documented by `bubbleterm` includes `NewWithCommand`, `Init`, `Update`, `View`, `Resize`, `Focus`, `Blur`, `SetAutoPoll(false)` and `UpdateTerminal()`, so this example relies exactly on those pieces. [^6_1][^6_2]

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
	line1 := "q/ctrl+c exit • ctrl+l clear • ctrl+r restart shell"
	line2 := "dynamic resize active"
	if m.ready && m.terminal != nil && m.terminal.Focused() {
		line2 = "dynamic resize active • terminal focused"
	}
	return trimWidth(padRight(line1, m.width), m.width) + "\n" +
		trimWidth(padRight(line2, m.width), m.width)
}

func (m model) bodyView() string {
	if m.err != nil {
		return padBlock(fmt.Sprintf("error: %v", m.err), m.termW, m.termH)
	}

	if !m.ready || m.terminal == nil {
		return padBlock("initializing terminal...", m.termW, m.termH)
	}

	return padBlock(m.terminal.View(), m.termW, m.termH)
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "waiting for terminal size..."
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


## What it does well

This pattern avoids creating the shell before knowing the usable size of the center area, which is one of the most common problems in Bubble Tea when a component depends on `tea.WindowSizeMsg`. [^6_3][^6_1] Additionally, each resize recalculates the layout and calls `terminal.Resize(...)`, which is exactly the operation that `bubbleterm` exposes to adapt the emulator dimensions. [^6_1]

It also uses manual polling because the `bubbleterm` documentation indicates that if you disable auto-polling with `SetAutoPoll(false)`, you must call `UpdateTerminal()` from an external ticker. [^6_1][^6_2] This gives you more control in complex integrations and helps avoid states where the process exists but the view doesn't repaint. [^6_1]

## Critical points

In Bubble Tea, the correct size arrives via `tea.WindowSizeMsg`, so it's not advisable to initialize the terminal in `Init()` with dummy sizes. [^6_3][^6_4] In cases where the external terminal changes size and it doesn't propagate well, the Bubble Tea community has also used a ticker that re-injects resize events to force coherent layout updates. [^6_5]

If the shell doesn't appear, check four things: that the main `View()` actually includes `m.terminal.View()`, that the panel has width and height greater than zero, that the terminal receives `Update(msg)` for all messages, and that manual polling is active if you called `SetAutoPoll(false)`. [^6_1][^6_2]

## Useful adjustments

You can change `exec.Command("bash")` to `exec.Command("zsh")` or to an app like `htop`, because `bubbleterm` is designed to run interactive processes within a bubble. [^6_2][^6_6] If you want to integrate it into a more serious layout, a complementary option is to use a layout manager like `bubblelayout`, which translates `tea.WindowSizeMsg` to exact per-panel sizes. [^6_7]

Send me your current code and I'll return it adapted to this pattern, with your panes and navigation.
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
