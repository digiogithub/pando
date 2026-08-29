package windows

import "github.com/digiogithub/pando/internal/uiauto/core"

// UIA_*ControlTypeId constants, from UIAutomationClient.h / UIAutomationCore.idl.
// These are the raw numeric ids the CacheRequest/GetCachedPropertyValue
// call for UIA_ControlTypePropertyId returns.
const (
	ControlTypeButton       int32 = 50000
	ControlTypeCalendar     int32 = 50001
	ControlTypeCheckBox     int32 = 50002
	ControlTypeComboBox     int32 = 50003
	ControlTypeEdit         int32 = 50004
	ControlTypeHyperlink    int32 = 50005
	ControlTypeImage        int32 = 50006
	ControlTypeListItem     int32 = 50007
	ControlTypeList         int32 = 50008
	ControlTypeMenu         int32 = 50009
	ControlTypeMenuBar      int32 = 50010
	ControlTypeMenuItem     int32 = 50011
	ControlTypeProgressBar  int32 = 50012
	ControlTypeRadioButton  int32 = 50013
	ControlTypeScrollBar    int32 = 50014
	ControlTypeSlider       int32 = 50015
	ControlTypeSpinner      int32 = 50016
	ControlTypeStatusBar    int32 = 50017
	ControlTypeTab          int32 = 50018
	ControlTypeTabItem      int32 = 50019
	ControlTypeText         int32 = 50020
	ControlTypeToolBar      int32 = 50021
	ControlTypeToolTip      int32 = 50022
	ControlTypeTree         int32 = 50023
	ControlTypeTreeItem     int32 = 50024
	ControlTypeCustom       int32 = 50025
	ControlTypeGroup        int32 = 50026
	ControlTypeThumb        int32 = 50027
	ControlTypeDataGrid     int32 = 50028
	ControlTypeDataItem     int32 = 50029
	ControlTypeDocument     int32 = 50030
	ControlTypeSplitButton  int32 = 50031
	ControlTypeWindow       int32 = 50032
	ControlTypePane         int32 = 50033
	ControlTypeHeader       int32 = 50034
	ControlTypeHeaderItem   int32 = 50035
	ControlTypeTable        int32 = 50036
	ControlTypeTitleBar     int32 = 50037
	ControlTypeSeparator    int32 = 50038
	ControlTypeSemanticZoom int32 = 50039
)

// controlTypeNames maps a raw UIA ControlType id to the friendly name UIA
// itself reports for it (e.g. via GetControlTypeName / the LocalizedControlType
// property, lowercased), which is also the key space core.NormalizeRole's
// "uia" role table (internal/uiauto/core/role.go) expects.
var controlTypeNames = map[int32]string{
	ControlTypeButton:       "button",
	ControlTypeCalendar:     "calendar",
	ControlTypeCheckBox:     "check box",
	ControlTypeComboBox:     "combo box",
	ControlTypeEdit:         "edit",
	ControlTypeHyperlink:    "hyperlink",
	ControlTypeImage:        "image",
	ControlTypeListItem:     "list item",
	ControlTypeList:         "list",
	ControlTypeMenu:         "menu",
	ControlTypeMenuBar:      "menu bar",
	ControlTypeMenuItem:     "menu item",
	ControlTypeProgressBar:  "progress bar",
	ControlTypeRadioButton:  "radio button",
	ControlTypeScrollBar:    "scroll bar",
	ControlTypeSlider:       "slider",
	ControlTypeSpinner:      "spinner",
	ControlTypeStatusBar:    "status bar",
	ControlTypeTab:          "tab",
	ControlTypeTabItem:      "tab item",
	ControlTypeText:         "text",
	ControlTypeToolBar:      "tool bar",
	ControlTypeToolTip:      "tool tip",
	ControlTypeTree:         "tree",
	ControlTypeTreeItem:     "tree item",
	ControlTypeCustom:       "custom",
	ControlTypeGroup:        "group",
	ControlTypeThumb:        "thumb",
	ControlTypeDataGrid:     "data grid",
	ControlTypeDataItem:     "data item",
	ControlTypeDocument:     "document",
	ControlTypeSplitButton:  "split button",
	ControlTypeWindow:       "window",
	ControlTypePane:         "pane",
	ControlTypeHeader:       "header",
	ControlTypeHeaderItem:   "header item",
	ControlTypeTable:        "table",
	ControlTypeTitleBar:     "title bar",
	ControlTypeSeparator:    "separator",
	ControlTypeSemanticZoom: "semantic zoom",
}

// ControlTypeName returns the friendly, lowercased UIA ControlType name for
// raw id, or "" when id is not one of the known UIA_*ControlTypeId values.
func ControlTypeName(id int32) string {
	return controlTypeNames[id]
}

// RoleForControlType normalizes a raw UIA ControlType id to the canonical
// core.Role vocabulary via core.NormalizeRole("uia", ...), so it stays in
// sync with the shared per-platform role table in internal/uiauto/core.
func RoleForControlType(id int32) core.Role {
	return core.NormalizeRole("uia", ControlTypeName(id))
}
