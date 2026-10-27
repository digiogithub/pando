package client

import (
	"context"
	"net/url"
	"strconv"
)

// LogEntry is a server log line (GET /api/v1/logs).
type LogEntry struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	Details   string `json:"details,omitempty"`
}

// LogQuery filters ListLogs. Zero values mean "no filter" (limit 200).
type LogQuery struct {
	Level  string
	Search string
	Since  string
	Limit  int
	Offset int
}

// LogPage is one page of logs.
type LogPage struct {
	Logs  []LogEntry `json:"logs"`
	Total int        `json:"total"`
}

// ListLogs reads the server's in-memory log buffer.
func (c *Client) ListLogs(ctx context.Context, q LogQuery) (LogPage, error) {
	v := url.Values{}
	if q.Level != "" && q.Level != "all" {
		v.Set("level", q.Level)
	}
	if q.Search != "" {
		v.Set("search", q.Search)
	}
	if q.Since != "" {
		v.Set("since", q.Since)
	}
	if q.Limit <= 0 {
		q.Limit = 200
	}
	v.Set("limit", strconv.Itoa(q.Limit))
	if q.Offset > 0 {
		v.Set("offset", strconv.Itoa(q.Offset))
	}
	var out LogPage
	err := c.Get(ctx, "/api/v1/logs?"+v.Encode(), &out)
	return out, err
}
