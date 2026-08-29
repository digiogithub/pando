//go:build windows

package windows

import ole "github.com/go-ole/go-ole"

// CLSIDs / IIDs from UIAutomationClient.idl / uiautomation.h. CUIAutomation
// (the original, single-threaded-COM-safe automation object) is tried
// first; CUIAutomation8 (introduced in Windows 8, supports
// TreeTraversalOptions and a couple of newer patterns) is used when
// available. Both implement IUIAutomation.
var (
	clsidCUIAutomation  = ole.NewGUID("{FF48DBA4-60EF-4201-AA87-54103EEF594E}")
	clsidCUIAutomation8 = ole.NewGUID("{E22AD333-B25F-460C-83D0-0581107395C9}")
	iidIUIAutomation    = ole.NewGUID("{30CBE57D-D9D0-452A-AB13-7AC5AC4825EE}")
	iidIUIAutomation2   = ole.NewGUID("{34723AFF-0C9D-49D0-9896-7AB52DF8CD8A}")

	iidIUIAutomationElement           = ole.NewGUID("{D22108AA-8AC5-49A5-837B-37BBB3D7591E}")
	iidIUIAutomationCondition         = ole.NewGUID("{352FFBA8-0973-437C-A61F-F64CAFD81DF9}")
	iidIUIAutomationCacheRequest      = ole.NewGUID("{B32A92B5-BC25-4078-9C08-D7EE95C48E03}")
	iidIUIAutomationElementArray      = ole.NewGUID("{14314595-B4BC-4055-95F2-58F2E42C9855}")
	iidIUIAutomationTreeWalker        = ole.NewGUID("{4042C624-389C-4AFC-A630-9DF854A541FC}")
	iidIUIAutomationInvokePattern     = ole.NewGUID("{FB377FBE-8EA6-46D5-9C73-6499642D3059}")
	iidIUIAutomationValuePattern      = ole.NewGUID("{A94CD8B1-0844-4CD6-9D2D-640537AB39E9}")
	iidIUIAutomationTogglePattern     = ole.NewGUID("{94CF8058-9B8D-4AB9-8BFD-4CD0A33C8C70}")
	iidIUIAutomationSelectionItem     = ole.NewGUID("{A8EFA66A-0FDA-421A-9194-38021F3578EA}")
	iidIUIAutomationExpandCollapse    = ole.NewGUID("{619BE086-1F4E-4EE4-BAFA-210128738730}")
	iidIUIAutomationScrollItemPattern = ole.NewGUID("{B488300F-D015-4F19-9C29-BB595E3645EF}")
	iidIUIAutomationLegacyIAccessible = ole.NewGUID("{828055AD-355B-4435-86D5-3B51C14A9B1B}")
)

// UIA_*PatternId constants (UIAutomationClient.h).
const (
	patternInvoke            int32 = 10000
	patternSelection         int32 = 10001
	patternValue             int32 = 10002
	patternRangeValue        int32 = 10003
	patternScroll            int32 = 10004
	patternExpandCollapse    int32 = 10005
	patternGrid              int32 = 10006
	patternGridItem          int32 = 10007
	patternMultipleView      int32 = 10008
	patternWindow            int32 = 10009
	patternSelectionItem     int32 = 10010
	patternDock              int32 = 10011
	patternTable             int32 = 10012
	patternTableItem         int32 = 10013
	patternText              int32 = 10014
	patternToggle            int32 = 10015
	patternTransform         int32 = 10016
	patternScrollItem        int32 = 10017
	patternLegacyIAccessible int32 = 10018
)

// UIA_*PropertyId constants (UIAutomationClient.h) for the fixed set of
// properties this backend pre-caches on every element.
const (
	propertyRuntimeId         int32 = 30000
	propertyBoundingRectangle int32 = 30001
	propertyProcessId         int32 = 30002
	propertyControlType       int32 = 30003
	propertyIsEnabled         int32 = 30010
	propertyAutomationId      int32 = 30011
	propertyClassName         int32 = 30012
	propertyName              int32 = 30005
	propertyHasKeyboardFocus  int32 = 30008
	propertyIsOffscreen       int32 = 30022
)

// TreeScope_* constants (UIAutomationClient.h), a bit mask.
const (
	treeScopeElement     int32 = 1
	treeScopeChildren    int32 = 2
	treeScopeDescendants int32 = 8
	treeScopeSubtree     int32 = treeScopeElement | treeScopeChildren | treeScopeDescendants
)

// PropertyConditionFlags_* (used with CreatePropertyConditionEx); this
// backend only ever uses the default (case-sensitive, 0) flag.
const propertyConditionFlagsNone int32 = 0
