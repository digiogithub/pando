// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package api

import (
	"net/http"
	"strconv"
)

const (
	// defaultSessionPageSize is the page size used when a client sends no limit.
	defaultSessionPageSize = 100
	// maxPageSize caps how much a single request can ask for.
	maxPageSize = 500
)

// paginationParams reads ?limit= and ?offset= from the request. A missing or
// invalid limit falls back to def; limit is capped at maxPageSize and offset is
// never negative.
func paginationParams(r *http.Request, def int) (limit, offset int) {
	limit = def
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}

	if raw := r.URL.Query().Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			offset = v
		}
	}
	return limit, offset
}

// paginate returns the [offset, offset+limit) window of items, clamped to the
// slice bounds. It never returns nil so the JSON stays an array.
func paginate[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
