package monitor

import "sync"

type Broker struct {
	mu       sync.RWMutex
	clients  map[chan Event]struct{}
	eventLog []Event
}

func NewBroker() *Broker {
	return &Broker{
		clients: make(map[chan Event]struct{}),
	}
}

func (b *Broker) Subscribe() chan Event {
	ch := make(chan Event, 64)
	b.mu.Lock()
	for _, e := range b.eventLog {
		select {
		case ch <- e:
		default:
		}
	}
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broker) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

func (b *Broker) Broadcast(e Event) {
	b.mu.Lock()
	b.eventLog = append(b.eventLog, e)
	for ch := range b.clients {
		select {
		case ch <- e:
		default:
		}
	}
	b.mu.Unlock()
}
