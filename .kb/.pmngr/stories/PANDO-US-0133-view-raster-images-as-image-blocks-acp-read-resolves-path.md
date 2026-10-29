---
id: PANDO-US-0133
type: story
title: "view: raster images as image blocks; ACP read resolves path before recording"
status: done
parent: PANDO-EP-0023
author: mcp
labels: [tools, view]
created: 2026-11-03T12:02:14Z
updated: 2026-11-03T12:02:22Z
closed: 2026-11-03T12:02:22Z
---

## Description
Already implemented in this report: `internal/llm/tools/view_image.go` (new), the image branch of `view.go`, `runWithACP` resolving the path with `resolveToolPath`, and tests in `view_image_test.go`.

## Acceptance Criteria
- PNG/JPEG returned as `ToolResponseTypeImage`; SVG stays text; non-image content with an image extension rejected.
- Tests in tools/agent/api pass.
