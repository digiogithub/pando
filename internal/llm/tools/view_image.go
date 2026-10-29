package tools

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/digiogithub/pando/internal/imageopt"
	"github.com/digiogithub/pando/internal/runtime"
)

// MaxViewImageBytes caps how large an image file the view tool will load into a
// tool response. Larger files are rejected with a text error instead of being
// base64-encoded into the request.
const MaxViewImageBytes = 20 * 1024 * 1024

// readImageForView loads an image file and returns it as an image tool response.
// The agent routes ToolResponseTypeImage results to the model as real image
// blocks (agent.buildImageToolResult), so the model can actually see the file.
// The MIME type is taken from the file content, not the extension; files whose
// content is not png/jpeg/webp/gif are rejected.
func readImageForView(ctx context.Context, workspaceFS runtime.WorkspaceFS, filePath, imageType string) ToolResponse {
	data, err := workspaceFS.ReadFile(ctx, filePath)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("failed to read image %s: %s", filePath, err))
	}
	if len(data) > MaxViewImageBytes {
		return NewTextErrorResponse(fmt.Sprintf("image %s is too large: %d bytes (max %d)", filePath, len(data), MaxViewImageBytes))
	}
	if imageopt.DetectMIME(data) == "" {
		return NewTextErrorResponse(fmt.Sprintf("file %s has a %s extension but its content is not a supported image (png/jpeg/webp/gif)", filePath, imageType))
	}
	return ToolResponse{
		Type:    ToolResponseTypeImage,
		Content: base64.StdEncoding.EncodeToString(data),
	}
}
