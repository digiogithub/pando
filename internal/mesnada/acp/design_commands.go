package acp

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	acpsdk "github.com/madeindigio/acp-go-sdk"

	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/imageopt"
)

// The ACP design commands are the Zed/VS Code/JetBrains surface of the Design
// Studio. An ACP client has no Pando UI to embed, so what it can offer is what
// the protocol already carries: a resource_link the editor turns into a
// clickable preview, and an ImageBlock it renders inline.
//
// These are control commands, not agent turns: they answer from the design
// store and end the turn. Authoring stays in the chat, through the design_*
// tools.

// designScreenshotCaps keeps an inline preview small enough to be pleasant in a
// side panel. An artifact screenshot is a full-page render; sent raw it is
// megabytes of base64 in a JSON-RPC frame.
var designScreenshotCaps = imageopt.Options{
	AutoResize:     true,
	MaxLongSidePx:  1280,
	MaxBase64Bytes: 1 << 20,
	Quality:        82,
}

func (a *PandoACPAgent) designService(acpSession *ACPServerSession) (*design.Service, error) {
	return design.ServiceFor(acpSession.PandoSessionID())
}

// processDesignCommand handles `/design [artifact]`. With no argument it lists
// what the project contains; with one it shows that artifact and links it.
func (a *PandoACPAgent) processDesignCommand(ctx context.Context, acpSession *ACPServerSession, ref string) (acpsdk.StopReason, error) {
	svc, err := a.designService(acpSession)
	if err != nil {
		return a.designFailure(acpSession, err)
	}
	if strings.TrimSpace(ref) != "" {
		return a.processDesignOpenCommand(ctx, acpSession, ref)
	}

	artifacts, err := svc.List(ctx, false)
	if err != nil {
		return a.designFailure(acpSession, err)
	}
	if len(artifacts) == 0 {
		return a.designReply(acpSession, "This project has no design artifacts yet. Ask for one in the chat — for example \"design a landing page for X\" — and it will appear here.")
	}

	lines := []string{fmt.Sprintf("%d design artifact(s):", len(artifacts)), ""}
	for _, artifact := range artifacts {
		lines = append(lines, fmt.Sprintf("- **%s** (%s, v%d) — `%s`", artifact.Title, artifact.Kind, artifact.CurrentVersion, artifact.Dir))
	}
	lines = append(lines, "", "Use `/design-open <slug>` to preview one, `/design-versions <slug>` for its history.")
	return a.designReply(acpSession, strings.Join(lines, "\n"))
}

// processDesignOpenCommand handles `/design-open [artifact] [slide]`. It sends
// the live preview as a resource_link and the current render as an image, so a
// client that supports neither still gets the URL as text.
func (a *PandoACPAgent) processDesignOpenCommand(ctx context.Context, acpSession *ACPServerSession, args string) (acpsdk.StopReason, error) {
	svc, err := a.designService(acpSession)
	if err != nil {
		return a.designFailure(acpSession, err)
	}
	ref, slide := splitDesignSlide(args)
	artifact, err := svc.Resolve(ctx, ref)
	if err != nil {
		return a.designFailure(acpSession, err)
	}
	presentation, err := svc.LiveURL(ctx, artifact.ID, slide)
	if err != nil {
		return a.designFailure(acpSession, err)
	}

	header := fmt.Sprintf("**%s** — %s, version %d\n%s", presentation.Title, presentation.Kind, presentation.Version, presentation.URL)
	if presentation.Slides > 0 {
		header += fmt.Sprintf("\n%d slide(s)", presentation.Slides)
	}
	if err := a.designText(acpSession, header); err != nil {
		return "", err
	}

	link := acpsdk.ResourceLinkBlock(presentation.Title, presentation.URL)
	if err := acpSession.SendUpdate(acpsdk.UpdateAgentMessage(link)); err != nil {
		return "", err
	}
	// A screenshot needs a browser. Its absence must not turn a working preview
	// link into a failed command, so the error is reported and swallowed.
	if block, shotErr := a.designScreenshotBlock(ctx, svc, artifact, slide); shotErr == nil {
		if err := acpSession.SendUpdate(acpsdk.UpdateAgentMessage(block)); err != nil {
			return "", err
		}
	} else if err := a.designText(acpSession, fmt.Sprintf("_(no inline preview: %v)_", shotErr)); err != nil {
		return "", err
	}
	return acpsdk.StopReasonEndTurn, nil
}

// processDesignVersionsCommand handles `/design-versions [artifact]`.
//
// The plan asked for an image per version. A version is a directory-scoped
// snapshot, not a stored document, so rendering one would mean checking it out
// over the user's working tree first — a destructive act for a read-only
// command. The history is therefore text, and the single image is the current
// version, which is the one that exists on disk.
func (a *PandoACPAgent) processDesignVersionsCommand(ctx context.Context, acpSession *ACPServerSession, ref string) (acpsdk.StopReason, error) {
	svc, err := a.designService(acpSession)
	if err != nil {
		return a.designFailure(acpSession, err)
	}
	artifact, err := svc.Resolve(ctx, ref)
	if err != nil {
		return a.designFailure(acpSession, err)
	}
	versions, err := svc.Versions(ctx, artifact.ID)
	if err != nil {
		return a.designFailure(acpSession, err)
	}

	lines := []string{fmt.Sprintf("**%s** — %d version(s), current v%d", artifact.Title, len(versions), artifact.CurrentVersion), ""}
	for _, v := range versions {
		marker := ""
		if v.Number == artifact.CurrentVersion {
			marker = " ← current"
		}
		summary := v.Summary
		if summary == "" {
			summary = "_(no summary)_"
		}
		line := fmt.Sprintf("- **v%d** %s%s", v.Number, summary, marker)
		if v.Critique != nil {
			line += fmt.Sprintf(" · score %.1f", v.Critique.Score)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "Ask in the chat to check out a version; `/design-open` previews the current one.")
	if err := a.designText(acpSession, strings.Join(lines, "\n")); err != nil {
		return "", err
	}

	if presentation, err := svc.LiveURL(ctx, artifact.ID, 0); err == nil {
		link := acpsdk.ResourceLinkBlock(fmt.Sprintf("%s v%d", artifact.Title, artifact.CurrentVersion), presentation.URL)
		if err := acpSession.SendUpdate(acpsdk.UpdateAgentMessage(link)); err != nil {
			return "", err
		}
	}
	return acpsdk.StopReasonEndTurn, nil
}

// designScreenshotBlock renders the artifact and returns it as an ImageBlock,
// downscaled to something a chat panel can hold.
func (a *PandoACPAgent) designScreenshotBlock(ctx context.Context, svc *design.Service, artifact design.Artifact, slide int) (acpsdk.ContentBlock, error) {
	renderer := svc.Renderer()
	if renderer == nil {
		return acpsdk.ContentBlock{}, design.ErrNoBrowser
	}
	png, err := renderer.Screenshot(ctx, artifact, design.ScreenshotOptions{Slide: slide})
	if err != nil {
		return acpsdk.ContentBlock{}, err
	}
	data, mime, _, err := imageopt.Normalize(png, imageopt.MIMEPNG, designScreenshotCaps)
	if err != nil {
		data, mime = png, imageopt.MIMEPNG
	}
	return acpsdk.ImageBlock(base64.StdEncoding.EncodeToString(data), mime), nil
}

func (a *PandoACPAgent) designText(acpSession *ACPServerSession, text string) error {
	return a.sendAgentText(acpSession, text)
}

// designReply is designText for the terminal case: send the text and end the
// turn.
func (a *PandoACPAgent) designReply(acpSession *ACPServerSession, text string) (acpsdk.StopReason, error) {
	if err := a.sendAgentText(acpSession, text); err != nil {
		return "", err
	}
	return acpsdk.StopReasonEndTurn, nil
}

// designFailure reports a design error to the user and ends the turn normally:
// a mistyped slug is a conversation, not a protocol error.
func (a *PandoACPAgent) designFailure(acpSession *ACPServerSession, err error) (acpsdk.StopReason, error) {
	message := err.Error()
	if strings.Contains(message, design.ErrNoProvider.Error()) {
		message = "The design subsystem is not available in this process."
	}
	if sendErr := a.sendAgentText(acpSession, message); sendErr != nil {
		return "", sendErr
	}
	return acpsdk.StopReasonEndTurn, nil
}

// splitDesignSlide accepts "<ref> <slide>", "<ref> #3" or a bare slide number,
// because "/design-open 3" on a deck the user is already looking at is the
// shortest thing they will type.
func splitDesignSlide(args string) (string, int) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return "", 0
	}
	last := strings.TrimPrefix(fields[len(fields)-1], "#")
	if n, err := strconv.Atoi(last); err == nil && n > 0 {
		return strings.Join(fields[:len(fields)-1], " "), n
	}
	return strings.Join(fields, " "), 0
}

// processDesignSystemCommand handles `/design-system`.
//
// It is read-only on purpose. Changing a design system rewrites the look of
// every artifact in the project at once, and a slash command typed into an
// editor is the wrong place for that: the `design_system` tool asks for
// permission first, and the CLI shows what an extraction found before writing.
func (a *PandoACPAgent) processDesignSystemCommand(acpSession *ACPServerSession) (acpsdk.StopReason, error) {
	svc, err := a.designService(acpSession)
	if err != nil {
		return a.designFailure(acpSession, err)
	}
	ds, exists, err := svc.LoadSystem()
	if err != nil {
		return a.designFailure(acpSession, err)
	}
	lines := []string{fmt.Sprintf("**Design system — %s**", ds.Name), ""}
	if !exists {
		lines = append(lines,
			"_This project has not committed a design system yet; these are the defaults._",
			"")
	}
	lines = append(lines,
		fmt.Sprintf("- tokens: `%s`", svc.SystemRelPath(design.SystemTokensFile)),
		fmt.Sprintf("- stylesheet: `%s`", svc.SystemRelPath(design.SystemStylesheet)),
		fmt.Sprintf("- contract: `%s`", svc.SystemRelPath(design.SystemContractFile)),
		"")
	for _, group := range design.SortedTokenGroups(ds.Tokens) {
		for _, name := range design.SortedTokenNames(ds.Tokens[group]) {
			lines = append(lines, fmt.Sprintf("- `--%s-%s`: `%s`", group, name, ds.Tokens[group][name]))
		}
	}
	if names := design.ExampleSystemNames(); len(names) > 0 {
		lines = append(lines, "",
			fmt.Sprintf("Bundled style guides: %s. Ask me to extract one with the `design_system` tool.",
				strings.Join(names, ", ")))
	}
	return a.designReply(acpSession, strings.Join(lines, "\n"))
}

// processDesignTemplatesCommand handles `/design-templates`.
//
// Like `/design-system` it is read-only: a template is only half the input, and
// the other half is the brief. Listing them here lets the user pick a name and
// then say what they want in the same conversation.
func (a *PandoACPAgent) processDesignTemplatesCommand(acpSession *ACPServerSession) (acpsdk.StopReason, error) {
	templates, err := design.BundledTemplates()
	if err != nil {
		return a.designFailure(acpSession, err)
	}
	if len(templates) == 0 {
		return a.designReply(acpSession, "No design templates are bundled with this build.")
	}

	lines := []string{"**Design templates**", ""}
	for _, tpl := range templates {
		if !tpl.Startable {
			continue
		}
		lines = append(lines, fmt.Sprintf("- **%s** (`%s`) — %s", tpl.Name, tpl.Kind, tpl.Description))
		if tpl.RequiresSystem {
			lines = append(lines, "  - needs a committed design system")
		}
		if tpl.ExamplePrompt != "" {
			lines = append(lines, fmt.Sprintf("  - try: _%s_", tpl.ExamplePrompt))
		}
	}
	workflows := []string{}
	for _, tpl := range templates {
		if !tpl.Startable {
			workflows = append(workflows, fmt.Sprintf("`%s`", tpl.Name))
		}
	}
	if len(workflows) > 0 {
		lines = append(lines, "", "Workflows: "+strings.Join(workflows, ", "))
	}
	if craft := design.CraftReferenceNames(); len(craft) > 0 {
		lines = append(lines, fmt.Sprintf("Craft references: %s.", strings.Join(craft, ", ")))
	}
	lines = append(lines, "", "Name one and tell me what to build.")
	return a.designReply(acpSession, strings.Join(lines, "\n"))
}
