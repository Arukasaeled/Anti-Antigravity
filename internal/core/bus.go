package core

import (
	"sync"
)

type EventType string

const (
	StateChangedEvent EventType = "state_changed"
	PluginStatusEvent EventType = "plugin_status"
	HostExitedEvent   EventType = "host_exited"
	ErrorEvent        EventType = "error"
)

type Event struct {
	Type    EventType
	Payload any
}

type Subscriber = chan Event

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[EventType][]Subscriber
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[EventType][]Subscriber),
	}
}

func (b *EventBus) Subscribe(eventType EventType) Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(Subscriber, 100)
	b.subscribers[eventType] = append(b.subscribers[eventType], ch)
	return ch
}

func (b *EventBus) Unsubscribe(eventType EventType, ch Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subscribers[eventType]
	for i, sub := range subs {
		if sub == ch {
			b.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
}

func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers[event.Type] {
		select {
		case ch <- event:
		default: // non-blocking, drop event if channel is full
		}
	}
}
