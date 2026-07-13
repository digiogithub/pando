package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/rag/kb"
)

const kbRelatedDocumentsToolName = "kb_related_documents"

// kbWikiLinkSyntax is the one description of the syntax, shared by the tools that
// need to teach it.
const kbWikiLinkSyntax = "Wiki links: writing [[concept]] or [[concept|label]] in a document's content links it to another document. " +
	"A target may be a full path ([[pando/plans/foo.md]]), a bare name ([[foo]]) or a declared alias; occurrences inside code fences are ignored. " +
	"A target no document defines yet stays unresolved on purpose: it records a concept worth documenting later."

// kbLinkView is an outgoing link as reported by the tools.
type kbLinkView struct {
	// Target is the link as written, e.g. "pando/plans/foo.md" or "Code Graph".
	Target string `json:"target"`
	Slug   string `json:"slug"`
	Label  string `json:"label,omitempty"`
	// FilePath is the document the link resolves to, absent when unresolved.
	FilePath string `json:"file_path,omitempty"`
	Resolved bool   `json:"resolved"`
}

// kbBacklinkView is an incoming link as reported by the tools.
type kbBacklinkView struct {
	FilePath string `json:"file_path"`
	Slug     string `json:"slug"`
	Label    string `json:"label,omitempty"`
}

// kbRelatedView is a scored neighbour in the document graph.
type kbRelatedView struct {
	FilePath string   `json:"file_path"`
	Score    float64  `json:"score"`
	Reasons  []string `json:"reasons,omitempty"`
}

func kbLinkViews(targets []kb.LinkTarget) []kbLinkView {
	if len(targets) == 0 {
		return nil
	}
	out := make([]kbLinkView, len(targets))
	for i, t := range targets {
		out[i] = kbLinkView{
			Target:   t.Raw,
			Slug:     t.Slug,
			Label:    t.Label,
			FilePath: t.FilePath,
			Resolved: t.Resolved,
		}
	}
	return out
}

func kbBacklinkViews(backlinks []kb.Backlink) []kbBacklinkView {
	if len(backlinks) == 0 {
		return nil
	}
	out := make([]kbBacklinkView, len(backlinks))
	for i, b := range backlinks {
		out[i] = kbBacklinkView{FilePath: b.FilePath, Slug: b.Slug, Label: b.Label}
	}
	return out
}

func kbRelatedViews(related []kb.RelatedDocument) []kbRelatedView {
	if len(related) == 0 {
		return nil
	}
	out := make([]kbRelatedView, len(related))
	for i, r := range related {
		out[i] = kbRelatedView{FilePath: r.FilePath, Score: r.Score, Reasons: r.Reasons}
	}
	return out
}

// unresolvedTargets lists the links of a document that point at no document, in
// the spelling their author used.
func unresolvedTargets(targets []kb.LinkTarget) []string {
	var out []string
	for _, t := range targets {
		if !t.Resolved {
			out = append(out, t.Raw)
		}
	}
	return out
}

// attachDocumentLinks adds the link sections of a document to a tool response.
// It does nothing at all when the knowledge base holds no wiki link, so a
// knowledge base that does not use the syntax — every one written before the
// graph existed — produces exactly the response it produced before.
func attachDocumentLinks(ctx context.Context, store *kb.KBStore, out map[string]any, filePath string) error {
	has, err := store.HasLinks(ctx)
	if err != nil || !has {
		return err
	}

	links, err := store.OutgoingLinks(ctx, filePath)
	if err != nil {
		return err
	}
	backlinks, err := store.Backlinks(ctx, filePath)
	if err != nil {
		return err
	}

	if v := kbLinkViews(links); v != nil {
		out["links"] = v
	}
	if v := kbBacklinkViews(backlinks); v != nil {
		out["backlinks"] = v
	}
	return nil
}

// linkFeedback summarizes the wiki links a just-stored document declares, so the
// author sees they were understood and which targets nothing documents yet. It is
// empty when the document declares none, leaving the plain confirmation message
// untouched.
func linkFeedback(ctx context.Context, store *kb.KBStore, filePath string) string {
	// Advisory only: the document is already stored, so a graph read that fails
	// must not turn a successful write into an error.
	targets, err := store.OutgoingLinks(ctx, filePath)
	if err != nil || len(targets) == 0 {
		return ""
	}

	msg := fmt.Sprintf(" — %d wiki link(s) indexed", len(targets))
	if unresolved := unresolvedTargets(targets); len(unresolved) > 0 {
		quoted := make([]string, len(unresolved))
		for i, raw := range unresolved {
			quoted[i] = "[[" + raw + "]]"
		}
		msg += fmt.Sprintf("; no document defines %s yet", strings.Join(quoted, ", "))
	}
	return msg + "."
}

// KBRelatedDocumentsTool navigates the wiki link graph of the knowledge base.
type KBRelatedDocumentsTool struct {
	store *kb.KBStore
}

// NewKBRelatedDocumentsTool creates a new KBRelatedDocumentsTool.
func NewKBRelatedDocumentsTool(store *kb.KBStore) BaseTool {
	return &KBRelatedDocumentsTool{store: store}
}

func (t *KBRelatedDocumentsTool) Info() ToolInfo {
	return ToolInfo{
		Name: kbRelatedDocumentsToolName,
		Description: "Navigates the knowledge base as a wiki: given a document, returns the documents it links to, the documents that link back to it, and the neighbours it is most connected to (outgoing links rank highest, then backlinks, then shared tags). " +
			"Use it after kb_search_documents or kb_get_document to pull in the surrounding context instead of searching again.\n\n" +
			"Called with no file_path, it instead lists the \"wanted concepts\": link targets that no document defines yet, most requested first — the best candidates for the next document to write.\n\n" +
			kbWikiLinkSyntax,
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The document to explore (as stored, e.g. 'pando/plans/foo.md'). Omit to list the unresolved link targets of the whole knowledge base instead.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of related documents (or wanted concepts) to return. Default 10.",
			},
		},
	}
}

func (t *KBRelatedDocumentsTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		FilePath string `json:"file_path"`
		Limit    int    `json:"limit"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}

	if req.FilePath == "" {
		return t.runWanted(ctx, req.Limit)
	}

	doc, err := t.store.GetDocument(ctx, req.FilePath)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb error: %v", err)), nil
	}
	if doc == nil {
		return NewTextResponse(fmt.Sprintf("Document not found: %s", req.FilePath)), nil
	}

	links, err := t.store.OutgoingLinks(ctx, req.FilePath)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb links error: %v", err)), nil
	}
	backlinks, err := t.store.Backlinks(ctx, req.FilePath)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb backlinks error: %v", err)), nil
	}
	related, err := t.store.RelatedDocuments(ctx, req.FilePath, req.Limit)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb related error: %v", err)), nil
	}

	if len(links) == 0 && len(backlinks) == 0 && len(related) == 0 {
		return NewTextResponse(fmt.Sprintf(
			"%s is not connected to any other document. %s",
			req.FilePath, kbWikiLinkSyntax,
		)), nil
	}

	out := map[string]any{"file_path": req.FilePath}
	if v := kbLinkViews(links); v != nil {
		out["links"] = v
	}
	if v := kbBacklinkViews(backlinks); v != nil {
		out["backlinks"] = v
	}
	if v := kbRelatedViews(related); v != nil {
		out["related"] = v
	}
	return NewStructuredResponse(out), nil
}

// runWanted answers the no-file_path form: what the knowledge base refers to but
// never explains.
func (t *KBRelatedDocumentsTool) runWanted(ctx context.Context, limit int) (ToolResponse, error) {
	if limit <= 0 {
		limit = 10
	}

	wanted, err := t.store.WantedConcepts(ctx, limit)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb wanted concepts error: %v", err)), nil
	}
	if len(wanted) == 0 {
		return NewTextResponse(fmt.Sprintf(
			"Every wiki link in the knowledge base resolves to an existing document; there is no undocumented concept. %s",
			kbWikiLinkSyntax,
		)), nil
	}

	type wantedItem struct {
		Concept string   `json:"concept"`
		Slug    string   `json:"slug"`
		Count   int      `json:"linked_from_count"`
		Sources []string `json:"linked_from"`
	}
	items := make([]wantedItem, len(wanted))
	for i, w := range wanted {
		items[i] = wantedItem{Concept: w.Raw, Slug: w.Slug, Count: w.Count, Sources: w.Sources}
	}

	return NewStructuredResponse(map[string]any{
		"count":  len(items),
		"wanted": items,
	}), nil
}
