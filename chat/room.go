package chat

import (
	"sync"
)

type room struct {
	mutex   sync.RWMutex
	name    string
	clients map[Connection]struct{}
}

func newRoom(name string) *room {
	return &room{
		name:    name,
		clients: map[Connection]struct{}{},
	}
}

func (r *room) broadcast(from Connection, msg Message) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for conn := range r.clients {
		if conn == from {
			continue
		}
		conn.SendMessage(msg)
	}

	msg.Approve = true

	from.SendMessage(msg)
}

func (r *room) broadcastClientsCount() {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	count := uint(len(r.clients))

	for conn := range r.clients {
		conn.SendClientsCount(count)
	}
}

func (r *room) newClient(conn Connection) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.clients[conn] = struct{}{}
}

func (r *room) removeClient(conn Connection) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	delete(r.clients, conn)
}

func (r *room) clientsCount() uint {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return uint(len(r.clients))
}
