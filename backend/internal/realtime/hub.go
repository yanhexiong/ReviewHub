package realtime

import (
	"sync"
	"time"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan domain.ProjectEvent]struct{}
	revisions   map[string]int64
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string]map[chan domain.ProjectEvent]struct{}),
		revisions:   make(map[string]int64),
	}
}

func (hub *Hub) Subscribe(projectID string) (<-chan domain.ProjectEvent, func(), int64) {
	channel := make(chan domain.ProjectEvent, 32)
	hub.mu.Lock()
	if hub.subscribers[projectID] == nil {
		hub.subscribers[projectID] = make(map[chan domain.ProjectEvent]struct{})
	}
	hub.subscribers[projectID][channel] = struct{}{}
	revision := hub.revisions[projectID]
	hub.mu.Unlock()

	return channel, func() {
		hub.mu.Lock()
		if subscribers := hub.subscribers[projectID]; subscribers != nil {
			delete(subscribers, channel)
			if len(subscribers) == 0 {
				delete(hub.subscribers, projectID)
			}
		}
		hub.mu.Unlock()
	}, revision
}

func (hub *Hub) Publish(event domain.ProjectEvent) domain.ProjectEvent {
	hub.mu.Lock()
	event.Revision = hub.revisions[event.ProjectID] + 1
	hub.revisions[event.ProjectID] = event.Revision
	if event.ChangedAt == 0 {
		event.ChangedAt = time.Now().UnixMilli()
	}
	for subscriber := range hub.subscribers[event.ProjectID] {
		select {
		case subscriber <- event:
		default:
			// A slow browser must not block comment writes for other reviewers.
		}
	}
	hub.mu.Unlock()
	return event
}
