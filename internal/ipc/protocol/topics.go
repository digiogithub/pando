// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package protocol

// Session topics — published on the PUB socket for session lifecycle events.
const (
	// TopicSessionList is published when the session list changes; carries a full list.
	TopicSessionList = "session.list"
	// TopicSessionUpdate is published when a single session is created or updated.
	TopicSessionUpdate = "session.update"
	// TopicSessionActivated is published when the active session changes.
	TopicSessionActivated = "session.activated"
	// TopicSessionDeleted is published when a session is deleted.
	TopicSessionDeleted = "session.deleted"
)

// Message topics.
const (
	// TopicMessageAppend is published when a new message is added to a session.
	TopicMessageAppend = "message.append"
)

// LLM topics — published during LLM inference.
const (
	// TopicLLMToken is published for each streaming LLM token.
	TopicLLMToken = "llm.token"
	// TopicLLMStart is published when an LLM call begins.
	TopicLLMStart = "llm.start"
	// TopicLLMEnd is published when an LLM call completes.
	TopicLLMEnd = "llm.end"
)

// Tool topics — published during tool execution.
const (
	// TopicToolStart is published when tool execution begins.
	TopicToolStart = "tool.start"
	// TopicToolEnd is published when tool execution completes.
	TopicToolEnd = "tool.end"
)

// Instance topics — published for instance lifecycle management.
const (
	// TopicInstanceHeartbeat is published every 5 seconds to signal liveness.
	TopicInstanceHeartbeat = "instance.heartbeat"
	// TopicInstanceShutdown is published on graceful shutdown.
	TopicInstanceShutdown = "instance.shutdown"
)
