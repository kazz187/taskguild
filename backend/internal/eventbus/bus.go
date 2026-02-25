package eventbus

import (
	"sync"

	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
)

type Bus struct {
	mu          sync.RWMutex
	subscribers map[string]chan *taskguildv1.Event
}

func New() *Bus {
	return &Bus{
		subscribers: make(map[string]chan *taskguildv1.Event),
	}
}

func (b *Bus) Subscribe(bufSize int) (string, <-chan *taskguildv1.Event) {
	id := ulid.Make().String()
	ch := make(chan *taskguildv1.Event, bufSize)
	b.mu.Lock()
	b.subscribers[id] = ch
	b.mu.Unlock()
	return id, ch
}

func (b *Bus) Unsubscribe(id string) {
	b.mu.Lock()
	if ch, ok := b.subscribers[id]; ok {
		close(ch)
		delete(b.subscribers, id)
	}
	b.mu.Unlock()
}

func (b *Bus) Publish(event *taskguildv1.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// buffer full, drop event for this subscriber
		}
	}
}

func (b *Bus) PublishNew(eventType taskguildv1.EventType, resourceID string, payload string, metadata map[string]string) {
	event := &taskguildv1.Event{
		Id:         ulid.Make().String(),
		Type:       eventType,
		ResourceId: resourceID,
		Payload:    payload,
		Metadata:   metadata,
		CreatedAt:  timestamppb.Now(),
	}
	b.Publish(event)
}
