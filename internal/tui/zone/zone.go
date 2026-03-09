package zone

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	bubblezone "github.com/lrstanley/bubblezone"
)

const (
	FileTreeItemPrefix   = "filetree-item-"
	TabPrefix            = "tab-"
	SidebarItemPrefix    = "sidebar-item-"
	DialogButtonPrefix   = "dialog-button-"
	SessionItemPrefix    = "session-item-"
	ModelItemPrefix      = "model-item-"
	StatusModel          = "status-model"
	StatusSession        = "status-session"
	StatusHelp           = "status-help"
	StatusDiagnostics    = "status-diagnostics"
	StatusBreadcrumbPrefix = "status-breadcrumb-"
	ViewerViewport       = "viewer-viewport"
	EditorViewport       = "editor-viewport"
	DiffViewport         = "diff-viewport"
	ChatViewport         = "chat-viewport"
	PermissionAllow      = "permission-allow"
	PermissionSession    = "permission-session"
	PermissionDeny       = "permission-deny"
)

var Manager = bubblezone.New()

func FileTreeItemID(path string) string {
	return FileTreeItemPrefix + hash(path)
}

func TabID(index int) string {
	return fmt.Sprintf("%s%d", TabPrefix, index)
}

func SidebarItemID(id string) string {
	return SidebarItemPrefix + hash(id)
}

func DialogButtonID(id string) string {
	return DialogButtonPrefix + hash(id)
}

func SessionItemID(id string) string {
	return SessionItemPrefix + hash(id)
}

func ModelItemID(id string) string {
	return ModelItemPrefix + hash(id)
}

func MarkFileTreeItem(path, content string) string {
	return Manager.Mark(FileTreeItemID(path), content)
}

func MarkTab(index int, content string) string {
	return Manager.Mark(TabID(index), content)
}

func MarkSidebarItem(id, content string) string {
	return Manager.Mark(SidebarItemID(id), content)
}

func MarkStatusModel(content string) string {
	return Manager.Mark(StatusModel, content)
}

func MarkStatusSession(content string) string {
	return Manager.Mark(StatusSession, content)
}

func MarkDialogButton(id, content string) string {
	return Manager.Mark(DialogButtonID(id), content)
}

func MarkSessionItem(id, content string) string {
	return Manager.Mark(SessionItemID(id), content)
}

func MarkModelItem(id, content string) string {
	return Manager.Mark(ModelItemID(id), content)
}

func MarkViewerViewport(content string) string {
	return Manager.Mark(ViewerViewport, content)
}

func MarkEditorViewport(content string) string {
	return Manager.Mark(EditorViewport, content)
}

func MarkDiffViewport(content string) string {
	return Manager.Mark(DiffViewport, content)
}

func MarkChatViewport(content string) string {
	return Manager.Mark(ChatViewport, content)
}

func MarkStatusHelp(content string) string {
	return Manager.Mark(StatusHelp, content)
}

func MarkStatusDiagnostics(content string) string {
	return Manager.Mark(StatusDiagnostics, content)
}

func StatusBreadcrumbID(index int) string {
	return fmt.Sprintf("%s%d", StatusBreadcrumbPrefix, index)
}

func MarkStatusBreadcrumb(index int, content string) string {
	return Manager.Mark(StatusBreadcrumbID(index), content)
}

func InBounds(id string, msg tea.MouseMsg) bool {
	info := Manager.Get(id)
	return info != nil && info.InBounds(msg)
}

func hash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:6])
}
