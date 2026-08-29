//go:build windows

package windows

import "github.com/digiogithub/pando/internal/uiauto/core"

// IUIAutomationCacheRequest vtable slot indices (UIAutomationClient.idl).
// Slots 0-2 are IUnknown.
const (
	idxCacheRequestAddProperty  = 3
	idxCacheRequestAddPattern   = 4
	idxCacheRequestPutTreeScope = 9
)

// cacheRequest wraps an IUIAutomationCacheRequest, configured once by
// automation.createCacheRequest to pre-fetch this backend's fixed property
// set (see doc.go) over TreeScope_Subtree, then reused for every
// FindAll(TreeScope_Children, TrueCondition, cacheRequest) call the
// traversal makes — this is what turns "one round trip per property per
// node" into "one round trip per tree level".
type cacheRequest struct {
	obj *comObject
}

// addProperty calls IUIAutomationCacheRequest::AddProperty(propertyId).
func (c *cacheRequest) addProperty(propertyID int32) error {
	hr, _ := c.obj.call(idxCacheRequestAddProperty, uintptr(propertyID))
	if hresultOf(hr) != hrOK {
		return mapHRESULT("IUIAutomationCacheRequest.AddProperty", hresultOf(hr), core.ErrPlatformNotSupported)
	}
	return nil
}

// setTreeScope calls IUIAutomationCacheRequest::put_TreeScope(scope).
func (c *cacheRequest) setTreeScope(scope int32) error {
	hr, _ := c.obj.call(idxCacheRequestPutTreeScope, uintptr(scope))
	if hresultOf(hr) != hrOK {
		return mapHRESULT("IUIAutomationCacheRequest.put_TreeScope", hresultOf(hr), core.ErrPlatformNotSupported)
	}
	return nil
}

func (c *cacheRequest) release() {
	if c == nil || c.obj == nil {
		return
	}
	c.obj.Release()
}
