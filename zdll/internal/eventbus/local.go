package eventbus

import (
	"sync"

	"zdll/internal/event"
)

const defaultBuffer = 256

// LocalBus is an in-memory implementation of Bus.
type LocalBus struct {
	mu          sync.RWMutex
	subscribers []chan event.Event
	closed      bool
}

func NewLocal() *LocalBus {
	return &LocalBus{}
}

func (b *LocalBus) Publish(e event.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for _, ch := range b.subscribers {
		select {
		case ch <- e:
		default:
			// Drop oldest message if subscriber is slow.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- e:
			default:
			}
		}
	}
}

func (b *LocalBus) Subscribe() <-chan event.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan event.Event, defaultBuffer)
	b.subscribers = append(b.subscribers, ch)
	return ch
}

func (b *LocalBus) Unsubscribe(ch <-chan event.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.subscribers {
		if s == ch {
			close(s)
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			return
		}
	}
}

func (b *LocalBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for _, ch := range b.subscribers {
		close(ch)
	}
	b.subscribers = nil
}
