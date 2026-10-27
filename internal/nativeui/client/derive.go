package client

import (
	"net/url"
	"strings"
)

// Derive returns a client for another API root reachable through this
// server, sharing its transport, credentials and token. It is how a project
// workspace talks to a child instance through the parent proxy:
//
//	child := parent.Derive("/api/v1/projects/" + id + "/web")
//
// The proxy swaps the parent's token for the child's, and answers the child's
// own /api/v1/token with a placeholder, so the derived client always takes
// the token from the parent.
func (c *Client) Derive(pathPrefix string) *Client {
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.Trim(pathPrefix, "/")
	u.RawPath = ""
	return &Client{
		base:    &u,
		http:    c.http,
		timeout: c.timeout,
		user:    c.user,
		pass:    c.pass,
		parent:  c,
	}
}

// ProjectWebPrefix is the proxy prefix of a project's child instance.
func ProjectWebPrefix(projectID string) string {
	return "/api/v1/projects/" + url.PathEscape(projectID) + "/web"
}
