// Package vision implements the Pando Desktop Controller's vision-fallback
// path: when a region of the screen exposes no usable accessibility
// semantics (a canvas app, a remote desktop window, a game, an app with a
// broken accessibility implementation), the agent still needs a way to
// act. The model itself is the vision engine -- this package never
// inspects pixels or does any image analysis. Its job is to make the
// screenshot -> model -> coordinates -> physical input loop safe and
// explicit:
//
//   - DrawGrid overlays a light coordinate grid on a screenshot so a
//     vision-capable model can estimate pixel coordinates more accurately
//     (internal/llm/tools/desktop_screenshot.go's optional "grid" param).
//   - ValidateCoordinates checks a proposed (x,y) against real display
//     bounds before any physical input is sent (internal/uiauto.Manager.
//     ClickAt).
//
// Every action performed through this path must be marked source="vision"
// in the tool's structured response (see
// internal/llm/tools/desktop_click_at.go), so the agent and the user can
// always tell a semantic (accessibility-tree) action from a blind,
// guessed-coordinate one. Guardrails (permission prompt,
// Options.AllowPhysicalInput gating) live in internal/uiauto.Manager,
// which is the only caller of this package's coordinate helpers.
package vision
