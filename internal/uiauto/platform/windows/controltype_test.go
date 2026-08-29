package windows

import (
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

func TestControlTypeName(t *testing.T) {
	if got := ControlTypeName(ControlTypeButton); got != "button" {
		t.Fatalf("ControlTypeName(button) = %q, want \"button\"", got)
	}
	if got := ControlTypeName(999999); got != "" {
		t.Fatalf("ControlTypeName(unknown) = %q, want \"\"", got)
	}
}

func TestRoleForControlType(t *testing.T) {
	cases := []struct {
		id   int32
		want core.Role
	}{
		{ControlTypeButton, core.RoleButton},
		{ControlTypeEdit, core.RoleTextField},
		{ControlTypeCheckBox, core.RoleCheckbox},
		{ControlTypeComboBox, core.RoleComboBox},
		{ControlTypeHyperlink, core.RoleLink},
		{ControlTypeImage, core.RoleImage},
		{ControlTypeListItem, core.RoleListItem},
		{ControlTypeList, core.RoleList},
		{ControlTypeMenu, core.RoleMenu},
		{ControlTypeMenuBar, core.RoleMenuBar},
		{ControlTypeMenuItem, core.RoleMenuItem},
		{ControlTypeProgressBar, core.RoleProgressBar},
		{ControlTypeRadioButton, core.RoleRadio},
		{ControlTypeScrollBar, core.RoleScrollbar},
		{ControlTypeSlider, core.RoleSlider},
		{ControlTypeSpinner, core.RoleTextField},
		{ControlTypeStatusBar, core.RoleStatusBar},
		{ControlTypeTab, core.RoleTabList},
		{ControlTypeTabItem, core.RoleTab},
		{ControlTypeText, core.RoleText},
		{ControlTypeToolBar, core.RoleToolbar},
		{ControlTypeTree, core.RoleTree},
		{ControlTypeTreeItem, core.RoleTreeItem},
		{ControlTypeCustom, core.RoleGroup},
		{ControlTypeGroup, core.RoleGroup},
		{ControlTypeWindow, core.RoleWindow},
		{ControlTypePane, core.RolePanel},
		{ControlTypeHeader, core.RoleHeading},
		{ControlTypeTable, core.RoleTable},
		{ControlTypeDataGrid, core.RoleTable},
		{ControlTypeDataItem, core.RoleRow},
		{ControlTypeSeparator, core.RoleSeparator},
		{ControlTypeSplitButton, core.RoleButton},
		{99999, core.RoleUnknown},
	}
	for _, c := range cases {
		if got := RoleForControlType(c.id); got != c.want {
			t.Errorf("RoleForControlType(%d) = %q, want %q", c.id, got, c.want)
		}
	}
}
