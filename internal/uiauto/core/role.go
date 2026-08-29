package core

import "strings"

// Role is the normalized, platform-independent semantic role of an
// accessibility element.
type Role string

// Canonical role vocabulary. Backends map their platform-specific roles
// onto this set via NormalizeRole.
const (
	RoleApplication Role = "application"
	RoleWindow      Role = "window"
	RoleDialog      Role = "dialog"
	RoleButton      Role = "button"
	RoleCheckbox    Role = "checkbox"
	RoleRadio       Role = "radio"
	RoleTextField   Role = "textfield"
	RoleTextArea    Role = "textarea"
	RoleComboBox    Role = "combobox"
	RoleList        Role = "list"
	RoleListItem    Role = "listitem"
	RoleMenu        Role = "menu"
	RoleMenuItem    Role = "menuitem"
	RoleMenuBar     Role = "menubar"
	RoleTab         Role = "tab"
	RoleTabList     Role = "tablist"
	RoleTree        Role = "tree"
	RoleTreeItem    Role = "treeitem"
	RoleTable       Role = "table"
	RoleRow         Role = "row"
	RoleCell        Role = "cell"
	RoleLink        Role = "link"
	RoleImage       Role = "image"
	RoleHeading     Role = "heading"
	RoleLabel       Role = "label"
	RoleText        Role = "text"
	RoleGroup       Role = "group"
	RoleToolbar     Role = "toolbar"
	RoleScrollbar   Role = "scrollbar"
	RoleSlider      Role = "slider"
	RoleProgressBar Role = "progressbar"
	RoleStatusBar   Role = "statusbar"
	RolePanel       Role = "panel"
	RoleSeparator   Role = "separator"
	RoleUnknown     Role = "unknown"
)

// roleAliases maps a selector-facing alias to the canonical role it should
// match against, used by Role.Matches.
var roleAliases = map[string]Role{
	"textbox":     RoleTextField,
	"edit":        RoleTextField,
	"input":       RoleTextField,
	"push":        RoleButton,
	"pushbutton":  RoleButton,
	"radiobutton": RoleRadio,
	"combo":       RoleComboBox,
	"select":      RoleComboBox,
	"img":         RoleImage,
	"anchor":      RoleLink,
	"grp":         RoleGroup,
	"hr":          RoleSeparator,
}

// atspiRoleMap maps AT-SPI2 role names (Accessible2.Role / role name
// strings as reported over D-Bus, lowercased) to the canonical vocabulary.
var atspiRoleMap = map[string]Role{
	"application":   RoleApplication,
	"frame":         RoleWindow,
	"window":        RoleWindow,
	"dialog":        RoleDialog,
	"push button":   RoleButton,
	"toggle button": RoleButton,
	"check box":     RoleCheckbox,
	"radio button":  RoleRadio,
	"text":          RoleTextField,
	"entry":         RoleTextField,
	"password text": RoleTextField,
	"paragraph":     RoleTextArea,
	"combo box":     RoleComboBox,
	"list":          RoleList,
	"list item":     RoleListItem,
	"menu":          RoleMenu,
	"menu item":     RoleMenuItem,
	"menu bar":      RoleMenuBar,
	"page tab":      RoleTab,
	"page tab list": RoleTabList,
	"tree":          RoleTree,
	"tree item":     RoleTreeItem,
	"table":         RoleTable,
	"table row":     RoleRow,
	"table cell":    RoleCell,
	"link":          RoleLink,
	"image":         RoleImage,
	"icon":          RoleImage,
	"heading":       RoleHeading,
	"label":         RoleLabel,
	"static":        RoleText,
	"panel":         RolePanel,
	"filler":        RoleGroup,
	"grouping":      RoleGroup,
	"tool bar":      RoleToolbar,
	"scroll bar":    RoleScrollbar,
	"slider":        RoleSlider,
	"progress bar":  RoleProgressBar,
	"statusbar":     RoleStatusBar,
	"status bar":    RoleStatusBar,
	"separator":     RoleSeparator,
	// Additive entries for common AT-SPI2 roles not covered above (Phase 2,
	// Linux AT-SPI2 backend): spin buttons and check/radio menu items map to
	// their closest canonical counterpart, and notifications behave like
	// dialogs for selector/render purposes.
	"spin button":       RoleTextField,
	"check menu item":   RoleMenuItem,
	"radio menu item":   RoleMenuItem,
	"tearoff menu item": RoleMenuItem,
	"notification":      RoleDialog,
}

// uiaRoleMap maps Windows UI Automation ControlType names (as returned by
// GetCachedPropertyValue(UIA_ControlTypePropertyId) friendly names,
// lowercased) to the canonical vocabulary.
var uiaRoleMap = map[string]Role{
	"application":  RoleApplication,
	"window":       RoleWindow,
	"pane":         RolePanel,
	"button":       RoleButton,
	"split button": RoleButton,
	"check box":    RoleCheckbox,
	"radio button": RoleRadio,
	"edit":         RoleTextField,
	"document":     RoleTextArea,
	"combo box":    RoleComboBox,
	"list":         RoleList,
	"list item":    RoleListItem,
	"menu":         RoleMenu,
	"menu item":    RoleMenuItem,
	"menu bar":     RoleMenuBar,
	"tab item":     RoleTab,
	"tab":          RoleTabList,
	"tree":         RoleTree,
	"tree item":    RoleTreeItem,
	"table":        RoleTable,
	"data grid":    RoleTable,
	"data item":    RoleRow,
	"custom":       RoleGroup,
	"hyperlink":    RoleLink,
	"image":        RoleImage,
	"header":       RoleHeading,
	"text":         RoleText,
	"group":        RoleGroup,
	"tool bar":     RoleToolbar,
	"scroll bar":   RoleScrollbar,
	"slider":       RoleSlider,
	"progress bar": RoleProgressBar,
	"status bar":   RoleStatusBar,
	"separator":    RoleSeparator,
	// Additive entries for UIA ControlType ids not covered above (Phase 4,
	// Windows UI Automation backend): these have no exact canonical
	// counterpart, so each maps to its closest semantic match.
	"calendar":      RoleGroup,
	"spinner":       RoleTextField,
	"tool tip":      RoleText,
	"thumb":         RoleSlider,
	"title bar":     RoleGroup,
	"semantic zoom": RoleGroup,
	"header item":   RoleCell,
}

// axRoleMap maps macOS AXRole values (e.g. "AXButton", lowercased and
// without the "AX" prefix) to the canonical vocabulary.
var axRoleMap = map[string]Role{
	"application":       RoleApplication,
	"window":            RoleWindow,
	"sheet":             RoleDialog,
	"button":            RoleButton,
	"checkbox":          RoleCheckbox,
	"radiobutton":       RoleRadio,
	"textfield":         RoleTextField,
	"securetextfield":   RoleTextField,
	"textarea":          RoleTextArea,
	"popupbutton":       RoleComboBox,
	"combobox":          RoleComboBox,
	"list":              RoleList,
	"row":               RoleRow,
	"menu":              RoleMenu,
	"menuitem":          RoleMenuItem,
	"menubar":           RoleMenuBar,
	"tabgroup":          RoleTabList,
	"radiogroup":        RoleGroup,
	"outline":           RoleTree,
	"table":             RoleTable,
	"cell":              RoleCell,
	"link":              RoleLink,
	"image":             RoleImage,
	"heading":           RoleHeading,
	"statictext":        RoleText,
	"group":             RoleGroup,
	"toolbar":           RoleToolbar,
	"scrollbar":         RoleScrollbar,
	"slider":            RoleSlider,
	"progressindicator": RoleProgressBar,
	"splitter":          RoleSeparator,
}

// cdpRoleMap maps Chrome DevTools Protocol Accessibility.AXNode roles
// (lowercased) to the canonical vocabulary.
var cdpRoleMap = map[string]Role{
	"webarea":      RoleApplication,
	"window":       RoleWindow,
	"dialog":       RoleDialog,
	"button":       RoleButton,
	"checkbox":     RoleCheckbox,
	"radio":        RoleRadio,
	"textbox":      RoleTextField,
	"searchbox":    RoleTextField,
	"textfield":    RoleTextField,
	"combobox":     RoleComboBox,
	"listbox":      RoleList,
	"list":         RoleList,
	"listitem":     RoleListItem,
	"menu":         RoleMenu,
	"menuitem":     RoleMenuItem,
	"menubar":      RoleMenuBar,
	"tab":          RoleTab,
	"tablist":      RoleTabList,
	"tree":         RoleTree,
	"treeitem":     RoleTreeItem,
	"table":        RoleTable,
	"row":          RoleRow,
	"cell":         RoleCell,
	"columnheader": RoleCell,
	"link":         RoleLink,
	"image":        RoleImage,
	"heading":      RoleHeading,
	"labeltext":    RoleLabel,
	"statictext":   RoleText,
	"text":         RoleText,
	"group":        RoleGroup,
	"toolbar":      RoleToolbar,
	"scrollbar":    RoleScrollbar,
	"slider":       RoleSlider,
	"progressbar":  RoleProgressBar,
	"status":       RoleStatusBar,
	"separator":    RoleSeparator,
}

// platformRoleMaps indexes the per-platform maps by backend name.
var platformRoleMaps = map[string]map[string]Role{
	"atspi": atspiRoleMap,
	"uia":   uiaRoleMap,
	"ax":    axRoleMap,
	"cdp":   cdpRoleMap,
}

// NormalizeRole maps a raw, platform-specific role string to the canonical
// Role vocabulary. platform selects the per-backend table ("atspi", "uia",
// "ax", "cdp"); any other value, or a raw string not present in the table,
// falls back to lowercasing raw and matching it directly against the
// canonical vocabulary, and finally to RoleUnknown.
func NormalizeRole(platform, raw string) Role {
	norm := strings.ToLower(strings.TrimSpace(raw))
	if norm == "" {
		return RoleUnknown
	}
	if m, ok := platformRoleMaps[strings.ToLower(platform)]; ok {
		if r, ok := m[norm]; ok {
			return r
		}
	}
	// Fall back: the raw value may already be (or resemble) a canonical
	// role name, e.g. "button".
	candidate := Role(strings.TrimPrefix(norm, "ax"))
	if isCanonicalRole(candidate) {
		return candidate
	}
	if isCanonicalRole(Role(norm)) {
		return Role(norm)
	}
	return RoleUnknown
}

var canonicalRoles = map[Role]bool{
	RoleApplication: true, RoleWindow: true, RoleDialog: true, RoleButton: true,
	RoleCheckbox: true, RoleRadio: true, RoleTextField: true, RoleTextArea: true,
	RoleComboBox: true, RoleList: true, RoleListItem: true, RoleMenu: true,
	RoleMenuItem: true, RoleMenuBar: true, RoleTab: true, RoleTabList: true,
	RoleTree: true, RoleTreeItem: true, RoleTable: true, RoleRow: true,
	RoleCell: true, RoleLink: true, RoleImage: true, RoleHeading: true,
	RoleLabel: true, RoleText: true, RoleGroup: true, RoleToolbar: true,
	RoleScrollbar: true, RoleSlider: true, RoleProgressBar: true,
	RoleStatusBar: true, RolePanel: true, RoleSeparator: true, RoleUnknown: true,
}

func isCanonicalRole(r Role) bool {
	return canonicalRoles[r]
}

// Matches reports whether the receiver role matches the given selector role
// token, case-insensitively and allowing a small set of common aliases
// (e.g. "textbox" -> textfield, "edit" -> textfield). A selectorRole of "*"
// always matches.
func (r Role) Matches(selectorRole string) bool {
	sel := strings.ToLower(strings.TrimSpace(selectorRole))
	if sel == "" || sel == "*" {
		return true
	}
	if string(r) == sel {
		return true
	}
	if alias, ok := roleAliases[sel]; ok {
		return r == alias
	}
	return false
}
