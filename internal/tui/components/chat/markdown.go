package chat

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/components/editor"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

type markdownSegmentKind int

const (
	markdownTextSegment markdownSegmentKind = iota
	markdownCodeBlockSegment
)

type markdownSegment struct {
	kind     markdownSegmentKind
	content  string
	language string
}

var markdownURLPattern = regexp.MustCompile(`https?://[^\s<>()]+`)

func renderMarkdown(content string, width int) string {
	if content == "" {
		return ""
	}

	renderer := styles.GetMarkdownRenderer(width)
	highlighter := editor.New(theme.CurrentTheme())
	segments := splitMarkdownSegments(content)
	if len(segments) == 0 {
		return ""
	}

	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		switch segment.kind {
		case markdownCodeBlockSegment:
			parts = append(parts, renderMarkdownCodeBlock(highlighter, segment.content, segment.language))
		default:
			parts = append(parts, renderMarkdownText(renderer, segment.content))
		}
	}

	return strings.TrimRight(strings.Join(parts, "\n"), "\n")
}

func renderMarkdownText(renderer *glamour.TermRenderer, content string) string {
	if content == "" {
		return ""
	}

	rendered, err := renderer.Render(content)
	if err != nil {
		rendered = content
	}

	rendered = strings.TrimRight(rendered, "\n")
	rendered = styles.ForceReplaceBackgroundWithLipgloss(rendered, theme.CurrentTheme().Background())
	return hyperlinkMarkdownURLs(rendered)
}

func renderMarkdownCodeBlock(highlighter *editor.Highlighter, source, language string) string {
	t := theme.CurrentTheme()

	rendered, err := highlighter.HighlightSnippet(source, language)
	if err != nil {
		rendered = source
	}

	rendered = strings.TrimRight(rendered, "\n")
	rendered = styles.ForceReplaceBackgroundWithLipgloss(rendered, t.BackgroundDarker())

	codeBlock := styles.BaseStyle().
		Background(t.BackgroundDarker()).
		Foreground(t.MarkdownCodeBlock()).
		Padding(0, 1).
		Render(rendered)

	if language == "" {
		return codeBlock
	}

	label := styles.BaseStyle().
		Background(t.BackgroundSecondary()).
		Foreground(t.MarkdownCode()).
		Bold(true).
		Padding(0, 1).
		Render(strings.ToLower(language))

	return lipgloss.JoinVertical(lipgloss.Left, label, codeBlock)
}

func splitMarkdownSegments(content string) []markdownSegment {
	lines := strings.Split(content, "\n")
	segments := make([]markdownSegment, 0)

	var textBuilder strings.Builder
	var codeBuilder strings.Builder
	openFence := ""
	codeLanguage := ""

	flushText := func() {
		if textBuilder.Len() == 0 {
			return
		}
		segments = append(segments, markdownSegment{
			kind:    markdownTextSegment,
			content: textBuilder.String(),
		})
		textBuilder.Reset()
	}

	flushCode := func() {
		segments = append(segments, markdownSegment{
			kind:     markdownCodeBlockSegment,
			content:  codeBuilder.String(),
			language: codeLanguage,
		})
		codeBuilder.Reset()
		codeLanguage = ""
	}

	for _, line := range lines {
		if openFence == "" {
			if fence, language, ok := parseFenceStart(line); ok {
				flushText()
				openFence = fence
				codeLanguage = language
				continue
			}

			if textBuilder.Len() > 0 {
				textBuilder.WriteByte('\n')
			}
			textBuilder.WriteString(line)
			continue
		}

		if isFenceEnd(line, openFence) {
			flushCode()
			openFence = ""
			continue
		}

		if codeBuilder.Len() > 0 {
			codeBuilder.WriteByte('\n')
		}
		codeBuilder.WriteString(line)
	}

	if openFence != "" {
		flushCode()
	} else {
		flushText()
	}

	return segments
}

func parseFenceStart(line string) (string, string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) < 3 {
		return "", "", false
	}

	fenceChar := trimmed[0]
	if fenceChar != '`' && fenceChar != '~' {
		return "", "", false
	}

	fenceLen := 0
	for fenceLen < len(trimmed) && trimmed[fenceLen] == fenceChar {
		fenceLen++
	}
	if fenceLen < 3 {
		return "", "", false
	}

	info := strings.TrimSpace(trimmed[fenceLen:])
	return trimmed[:fenceLen], fenceLanguage(info), true
}

func isFenceEnd(line, openFence string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < len(openFence) {
		return false
	}

	fenceChar := openFence[0]
	fenceLen := 0
	for fenceLen < len(trimmed) && trimmed[fenceLen] == fenceChar {
		fenceLen++
	}
	if fenceLen < len(openFence) {
		return false
	}

	return strings.TrimSpace(trimmed[fenceLen:]) == ""
}

func fenceLanguage(info string) string {
	if info == "" {
		return ""
	}

	fields := strings.Fields(info)
	if len(fields) == 0 {
		return ""
	}

	return strings.ToLower(fields[0])
}

func hyperlinkMarkdownURLs(content string) string {
	return markdownURLPattern.ReplaceAllStringFunc(content, func(match string) string {
		url, suffix := trimHyperlinkSuffix(match)
		if url == "" {
			return match
		}
		return terminalHyperlink(url, url) + suffix
	})
}

func trimHyperlinkSuffix(url string) (string, string) {
	suffix := ""
	for len(url) > 0 {
		switch url[len(url)-1] {
		case '.', ',', ';', ':', '!', '?', ')', ']', '}':
			suffix = string(url[len(url)-1]) + suffix
			url = url[:len(url)-1]
		default:
			return url, suffix
		}
	}

	return url, suffix
}

func terminalHyperlink(url, text string) string {
	return "\x1b]8;;" + url + "\x07" + text + "\x1b]8;;\x07"
}
