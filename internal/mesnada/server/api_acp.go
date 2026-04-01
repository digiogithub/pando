package server

import (
	"context"
	"net/http"

	"github.com/digiogithub/pando/internal/mesnada/acp"
	"github.com/gin-gonic/gin"
)

// registerACPAPI registers ACP management endpoints.
func (s *Server) registerACPAPI(api *gin.RouterGroup) {
	if s.acpHandler == nil {
		return
	}

	acp := api.Group("/acp")
	{
		// Transport and session management
		acp.GET("/sessions", s.apiACPListSessions)
		acp.GET("/sessions/:id", s.apiACPGetSession)
		acp.DELETE("/sessions/:id", s.apiACPCancelSession)
		acp.GET("/stats", s.apiACPStats)
		acp.GET("/health", s.apiACPHealthCheck)
	}
}

// apiACPListSessions lists historical sessions from Pando's session store.
func (s *Server) apiACPListSessions(c *gin.Context) {
	agent := s.acpHandler.GetAgent()
	if lister, ok := agent.(interface {
		ListSessions(ctx context.Context) ([]acp.ACPSessionInfo, error)
	}); ok {
		sessions, err := lister.ListSessions(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"sessions": sessions, "count": len(sessions)})
		return
	}

	c.JSON(http.StatusNotImplemented, gin.H{"error": "agent does not support listing historical sessions"})
}

// apiACPGetSession retrieves details about a specific ACP session.
func (s *Server) apiACPGetSession(c *gin.Context) {
	sessionID := c.Param("id")
	transport := s.acpHandler.GetTransport()

	info, err := transport.GetSessionInfo(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "session not found",
		})
		return
	}

	c.JSON(http.StatusOK, info)
}

// apiACPCancelSession cancels/closes a specific ACP session.
func (s *Server) apiACPCancelSession(c *gin.Context) {
	sessionID := c.Param("id")
	transport := s.acpHandler.GetTransport()

	if err := transport.CloseSession(sessionID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cancelled": true,
		"session":   sessionID,
	})
}

// apiACPStats returns comprehensive ACP server statistics.
func (s *Server) apiACPStats(c *gin.Context) {
	transport := s.acpHandler.GetTransport()
	agent := s.acpHandler.GetAgent()

	transportStats := transport.GetStats()
	capabilities := agent.GetCapabilities()

	c.JSON(http.StatusOK, gin.H{
		"status": "operational",
		"agent": gin.H{
			"name":         "pando",
			"version":      agent.GetVersion(),
			"capabilities": capabilities,
		},
		"transport": gin.H{
			"type":                "http+sse",
			"active_sessions":     transportStats.ActiveSessions,
			"total_sessions":      transportStats.TotalSessions,
			"requests_processed":  transportStats.RequestsProcessed,
			"max_sessions":        transportStats.MaxSessions,
			"uptime_seconds":      int(transportStats.Uptime.Seconds()),
			"idle_timeout_seconds": int(transportStats.IdleTimeout.Seconds()),
		},
	})
}

// apiACPHealthCheck provides detailed health information about the ACP server.
func (s *Server) apiACPHealthCheck(c *gin.Context) {
	transport := s.acpHandler.GetTransport()
	agent := s.acpHandler.GetAgent()

	activeSessions := transport.ActiveSessions()
	capabilities := agent.GetCapabilities()

	c.JSON(http.StatusOK, gin.H{
		"status":   "healthy",
		"protocol": "ACP",
		"transport": gin.H{
			"type":            "http+sse",
			"active_sessions": activeSessions,
		},
		"agent": gin.H{
			"name":         "pando",
			"version":      agent.GetVersion(),
			"capabilities": capabilities,
		},
	})
}
