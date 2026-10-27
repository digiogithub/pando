package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// InstanceInfo is a live Pando instance of the registry.
type InstanceInfo struct {
	InstanceID string `json:"instance_id"`
	Path       string `json:"path"`
	PID        int    `json:"pid"`
	PubPort    int    `json:"pub_port"`
	RPCPort    int    `json:"rpc_port"`
	StartedAt  string `json:"started_at"`
	Mode       string `json:"mode"`
	IsPrimary  bool   `json:"is_primary"`
}

// RemoteSession is a session of another instance.
type RemoteSession struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	UpdatedAt    string `json:"updated_at"`
	MessageCount int    `json:"message_count"`
}

// RemoteSessionPage is one page of remote sessions.
type RemoteSessionPage struct {
	Sessions []RemoteSession `json:"sessions"`
	Total    int             `json:"total"`
	HasMore  bool            `json:"has_more"`
}

// RemoteMessage is a message of a remote session.
type RemoteMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// InstanceEvent is one envelope of an instance stream.
type InstanceEvent struct {
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

// Instances lists the live instances.
func (c *Client) Instances(ctx context.Context) ([]InstanceInfo, error) {
	var out struct {
		Instances []InstanceInfo `json:"instances"`
	}
	err := c.Get(ctx, "/api/v1/instances", &out)
	return out.Instances, err
}

// RemoteSessions lists a page of the sessions of an instance.
func (c *Client) RemoteSessions(ctx context.Context, instanceID string, limit, offset int) (RemoteSessionPage, error) {
	var out RemoteSessionPage
	err := c.Get(ctx, fmt.Sprintf("/api/v1/instances/%s/sessions?limit=%d&offset=%d", url.PathEscape(instanceID), limit, offset), &out)
	if out.Total == 0 {
		out.Total = len(out.Sessions)
	}
	return out, err
}

// RemoteMessages loads the history of a remote session.
func (c *Client) RemoteMessages(ctx context.Context, instanceID, sessionID string) ([]RemoteMessage, error) {
	var out struct {
		Messages []RemoteMessage `json:"messages"`
	}
	err := c.Get(ctx, "/api/v1/instances/"+url.PathEscape(instanceID)+"/sessions/"+url.PathEscape(sessionID)+"/messages", &out)
	return out.Messages, err
}

// SendRemoteMessage sends a prompt to a remote session.
func (c *Client) SendRemoteMessage(ctx context.Context, instanceID, sessionID, content string) error {
	return c.Post(ctx, "/api/v1/instances/"+url.PathEscape(instanceID)+"/sessions/"+url.PathEscape(sessionID)+"/message", map[string]string{"content": content}, nil)
}

// CancelRemote interrupts the run of a remote session.
func (c *Client) CancelRemote(ctx context.Context, instanceID, sessionID string) error {
	return c.Delete(ctx, "/api/v1/instances/"+url.PathEscape(instanceID)+"/sessions/"+url.PathEscape(sessionID)+"/cancel", nil)
}

// FollowRemoteSession reads /instances/{id}/sessions/{sid}/stream until ctx
// ends or the stream closes, calling fn for every envelope (heartbeats
// included: they prove the stream is alive). Frames are unnamed
// `data: {topic,payload}` messages.
func (c *Client) FollowRemoteSession(ctx context.Context, instanceID, sessionID string, fn func(InstanceEvent)) error {
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/sessions/" + url.PathEscape(sessionID) + "/stream"
	return c.OpenStream(ctx, http.MethodGet, path, nil, func(f SSEFrame) error {
		var ev InstanceEvent
		if json.Unmarshal([]byte(f.Data), &ev) == nil {
			fn(ev)
		}
		return nil
	})
}
