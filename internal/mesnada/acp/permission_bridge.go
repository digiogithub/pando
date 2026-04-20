package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// ACPPermissionBridge translates Pando tool permission requests into ACP
// requestPermission calls to the connected editor. It is installed as a session
// handler when the session mode is "ask".
type ACPPermissionBridge struct {
	conn      *acpsdk.AgentSideConnection
	sessionID acpsdk.SessionId
	logger    *log.Logger

	// mu serializes permission requests for the same session.
	mu sync.Mutex
}

// NewACPPermissionBridge creates a new bridge for the given session.
func NewACPPermissionBridge(conn *acpsdk.AgentSideConnection, sessionID acpsdk.SessionId, logger *log.Logger) *ACPPermissionBridge {
	if logger == nil {
		logger = log.Default()
	}

	return &ACPPermissionBridge{
		conn:      conn,
		sessionID: sessionID,
		logger:    logger,
	}
}

// Handle processes a permission request by asking the connected editor via ACP.
// Returns true if approved, false if rejected.
// Signature matches what RegisterSessionHandler expects:
// func(req PermissionRequestData) bool
func (b *ACPPermissionBridge) Handle(req PermissionRequestData) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.conn == nil {
		b.logger.Printf("[ACP BRIDGE] No ACP connection available for tool=%s session=%s — denying", req.ToolName, req.SessionID)
		return false
	}

	b.logger.Printf("[ACP BRIDGE] Requesting permission for tool=%s session=%s path=%s", req.ToolName, req.SessionID, req.Path)

	title := req.ToolName
	if req.Description != "" {
		title = req.Description
	}

	toolKind := mapToolKind(req.ToolName)
	status := acpsdk.ToolCallStatusPending

	// Build rawInput from the structured params so the editor can render the
	// tool arguments (including the unified diff for edit/write tools).
	var rawInput any
	if req.Params != nil {
		if data, err := json.Marshal(req.Params); err == nil {
			var decoded any
			if json.Unmarshal(data, &decoded) == nil {
				rawInput = decoded
			}
		}
	}
	if rawInput == nil {
		rawInput = map[string]any{}
	}

	// Build locations from the file path so the editor can highlight the file.
	var locations []acpsdk.ToolCallLocation
	if req.Path != "" {
		locations = []acpsdk.ToolCallLocation{{Path: req.Path}}
	}

	permReq := acpsdk.RequestPermissionRequest{
		SessionId: b.sessionID,
		ToolCall: acpsdk.ToolCallUpdate{
			ToolCallId: acpsdk.ToolCallId(fmt.Sprintf("%s-%s-%d", req.SessionID, req.ToolName, time.Now().UnixNano())),
			Status:     &status,
			Title:      &title,
			Kind:       &toolKind,
			RawInput:   rawInput,
			Locations:  locations,
		},
		Options: []acpsdk.PermissionOption{
			{
				OptionId: acpsdk.PermissionOptionId("once"),
				Kind:     acpsdk.PermissionOptionKindAllowOnce,
				Name:     "Allow once",
			},
			{
				OptionId: acpsdk.PermissionOptionId("always"),
				Kind:     acpsdk.PermissionOptionKindAllowAlways,
				Name:     "Always allow",
			},
			{
				OptionId: acpsdk.PermissionOptionId("reject"),
				Kind:     acpsdk.PermissionOptionKindRejectOnce,
				Name:     "Reject",
			},
		},
	}

	resp, err := b.conn.RequestPermission(context.Background(), permReq)
	if err != nil {
		b.logger.Printf("[ACP BRIDGE] RequestPermission error for tool=%s session=%s: %v — denying", req.ToolName, req.SessionID, err)
		return false
	}

	if resp.Outcome.Selected == nil {
		b.logger.Printf("[ACP BRIDGE] Permission cancelled for tool=%s session=%s", req.ToolName, req.SessionID)
		return false
	}

	selected := string(resp.Outcome.Selected.OptionId)
	b.logger.Printf("[ACP BRIDGE] Permission outcome=%s for tool=%s session=%s", selected, req.ToolName, req.SessionID)

	approved := selected == "once" || selected == "always"

	// If the user approved an edit/write tool, send the preview file content to
	// the editor via WriteTextFile so it can refresh its buffer before the tool
	// executes. This mirrors opencode's pre-execution writeTextFile preview.
	if approved && isEditTool(req.ToolName) && b.conn != nil {
		b.sendEditPreview(req)
	}

	return approved
}

// sendEditPreview reads the current file, applies the unified diff from the
// permission params, and sends the resulting content to the editor via
// WriteTextFile. This gives the editor a preview of the change before the
// tool actually writes the file.
func (b *ACPPermissionBridge) sendEditPreview(req PermissionRequestData) {
	// Extract file_path and diff from the params (edit/write permission params).
	if req.Params == nil {
		return
	}

	// Marshal params back to JSON so we can extract named fields regardless of
	// whether Params is already a map or a typed struct.
	data, err := json.Marshal(req.Params)
	if err != nil {
		return
	}

	var p struct {
		FilePath string `json:"file_path"`
		Diff     string `json:"diff"`
	}
	if err := json.Unmarshal(data, &p); err != nil || p.FilePath == "" || p.Diff == "" {
		return
	}

	// Read the current file content (may not exist yet for new files).
	current := ""
	if raw, readErr := os.ReadFile(p.FilePath); readErr == nil {
		current = string(raw)
	}

	// Apply the unified diff to produce the expected post-edit content.
	newContent, ok := applyUnifiedDiff(current, p.Diff)
	if !ok {
		b.logger.Printf("[ACP BRIDGE] sendEditPreview: could not apply diff for %s — skipping preview", p.FilePath)
		return
	}

	_, writeErr := b.conn.WriteTextFile(context.Background(), acpsdk.WriteTextFileRequest{
		SessionId: b.sessionID,
		Path:      p.FilePath,
		Content:   newContent,
	})
	if writeErr != nil {
		b.logger.Printf("[ACP BRIDGE] sendEditPreview: WriteTextFile failed for %s: %v", p.FilePath, writeErr)
		return
	}

	b.logger.Printf("[ACP BRIDGE] sendEditPreview: sent preview for %s (%d bytes)", p.FilePath, len(newContent))
}

// applyUnifiedDiff applies a standard unified diff patch to the source string
// and returns (newContent, true) on success or ("", false) on failure.
// The diff format is the one produced by go-udiff.Unified (--- a/file, +++ b/file,
// @@ -oldStart,oldLines +newStart,newLines @@ hunks).
func applyUnifiedDiff(src, patch string) (string, bool) {
	if patch == "" {
		return src, true
	}

	type hunkLine struct {
		kind byte // ' ' context, '+' add, '-' remove
		text string
	}
	type hunk struct {
		oldStart int
		lines    []hunkLine
	}

	hunkRe := regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+\d+(?:,\d+)? @@`)
	var hunks []hunk
	var cur *hunk

	for _, l := range strings.Split(patch, "\n") {
		if m := hunkRe.FindStringSubmatch(l); m != nil {
			oldStart, _ := strconv.Atoi(m[1])
			hunks = append(hunks, hunk{oldStart: oldStart})
			cur = &hunks[len(hunks)-1]
			continue
		}
		if cur == nil || len(l) == 0 {
			continue
		}
		switch l[0] {
		case '+', '-', ' ':
			cur.lines = append(cur.lines, hunkLine{l[0], l[1:]})
		}
	}

	if len(hunks) == 0 {
		return src, true
	}

	srcLines := strings.Split(src, "\n")
	result := make([]string, 0, len(srcLines))
	srcIdx := 0 // 0-based index into srcLines

	for _, h := range hunks {
		// Copy unchanged lines that precede this hunk.
		target := h.oldStart - 1 // convert 1-based to 0-based
		for srcIdx < target && srcIdx < len(srcLines) {
			result = append(result, srcLines[srcIdx])
			srcIdx++
		}
		// Apply hunk lines.
		for _, hl := range h.lines {
			switch hl.kind {
			case ' ':
				if srcIdx < len(srcLines) {
					result = append(result, srcLines[srcIdx])
					srcIdx++
				}
			case '-':
				srcIdx++ // consume the removed line
			case '+':
				result = append(result, hl.text)
			}
		}
	}

	// Copy any remaining lines after the last hunk.
	result = append(result, srcLines[srcIdx:]...)
	return strings.Join(result, "\n"), true
}
