package realtime

import (
	"testing"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

func TestPublishAssignsRevisionAndDoesNotBlockOnSlowSubscriber(t *testing.T) {
	hub := NewHub()
	channel, cancel, revision := hub.Subscribe("project-1")
	defer cancel()
	if revision != 0 {
		t.Fatalf("initial revision = %d", revision)
	}
	event := hub.Publish(domain.ProjectEvent{Type: "comment.created", ProjectID: "project-1", CommentID: "comment-1"})
	if event.Revision != 1 || event.ChangedAt == 0 {
		t.Fatalf("published event = %#v", event)
	}
	if received := <-channel; received.Revision != event.Revision {
		t.Fatalf("received event = %#v", received)
	}
	for index := 0; index < 40; index++ {
		hub.Publish(domain.ProjectEvent{Type: "comment.updated", ProjectID: "project-1"})
	}
}
