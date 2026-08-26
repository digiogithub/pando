package acp

import (
	"context"
	"errors"

	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/pubsub"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

func (a *PandoACPAgent) startDesignUpdates(acpSession *ACPServerSession) {
	if acpSession == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	acpSession.SetDesignCancel(cancel)
	events := design.Events().Subscribe(ctx)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				a.handleDesignEvent(acpSession, event)
			}
		}
	}()
}

func (a *PandoACPAgent) handleDesignEvent(acpSession *ACPServerSession, event pubsub.Event[design.Event]) {
	if event.Type != pubsub.EventType(design.EventCreated) || event.Payload.Kind != design.EventCreated {
		return
	}
	if acpSession == nil || event.Payload.ArtifactID == "" {
		return
	}
	if event.Payload.SessionID != "" && event.Payload.SessionID != acpSession.PandoSessionID() {
		return
	}
	presentation, err := design.ResolveCreatedArtifactPresentation(context.Background(), event.Payload.ArtifactID)
	if err != nil {
		if !errors.Is(err, design.ErrNoProvider) {
			a.logger.Printf("[ACP AGENT] Failed to resolve preview URL for design artifact %s: %v", event.Payload.ArtifactID, err)
		}
		return
	}
	if err := acpSession.DesignAutoOpener().Open(event.Payload.ArtifactID, presentation.URL); err != nil {
		a.logger.Printf("[ACP AGENT] Failed to auto-open design preview for %s: %v", event.Payload.ArtifactID, err)
	}
	title := event.Payload.Title
	if title == "" {
		title = presentation.Title
	}
	if title == "" {
		title = event.Payload.ArtifactID
	}
	update := acpsdk.UpdateAgentMessage(acpsdk.ResourceLinkBlock(title, presentation.URL))
	if err := a.safeSessionUpdate(acpSession, acpSession.ID, update); err != nil {
		a.logger.Printf("[ACP AGENT] Failed to send design preview link for %s: %v", event.Payload.ArtifactID, err)
	}
}
