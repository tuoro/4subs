package server

import (
	"encoding/json"
	"sync"
)

type EventBus struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{
		clients: make(map[chan []byte]struct{}),
	}
}

func (e *EventBus) Subscribe() chan []byte {
	ch := make(chan []byte, 8)
	e.mu.Lock()
	e.clients[ch] = struct{}{}
	e.mu.Unlock()
	return ch
}

func (e *EventBus) Unsubscribe(ch chan []byte) {
	e.mu.Lock()
	if _, ok := e.clients[ch]; ok {
		delete(e.clients, ch)
		close(ch)
	}
	e.mu.Unlock()
}

func (e *EventBus) Publish(event string, payload any) {
	body := map[string]any{
		"event":   event,
		"payload": payload,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return
	}

	e.mu.RLock()
	defer e.mu.RUnlock()
	for ch := range e.clients {
		select {
		case ch <- raw:
		default:
		}
	}
}
