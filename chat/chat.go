package chat

import (
	"errors"
	"sync"
	"time"

	"github.com/microcosm-cc/bluemonday"
	blackfriday "gopkg.in/russross/blackfriday.v2"
)

type Chat struct {
	mutex sync.RWMutex
	store Store
	rooms map[string]*room
}

func New(s Store) *Chat {
	return &Chat{
		store: s,
		rooms: map[string]*room{},
	}
}

func (c *Chat) NewClient(room string, conn Connection) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	r, exists := c.rooms[room]
	if !exists {
		r = newRoom(room)
		c.rooms[room] = r
	}

	r.newClient(conn)

	go r.broadcastClientsCount()

	go func() {
		msgs, err := c.store.GetMessages(room)
		if err != nil {
			return
		}
		if len(msgs) > 0 {
			conn.SendMessages(msgs)
		}
	}()
}

var ErrRoomDoesNotExists = errors.New("room does not exists")

func (c *Chat) RemoveClient(room string, conn Connection) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	r, exists := c.rooms[room]
	if !exists {
		return ErrRoomDoesNotExists
	}

	r.removeClient(conn)

	if r.clientsCount() == 0 {
		delete(c.rooms, room)
	} else {
		go r.broadcastClientsCount()
	}

	return nil
}

func (c *Chat) NewMessage(room string, from Connection, text string) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	r, exists := c.rooms[room]
	if !exists {
		return ErrRoomDoesNotExists
	}

	id, err := c.store.GetMessageID(room)
	if err != nil {
		return errors.New("failed to get message ID from store: " + err.Error())
	}

	unsafe := blackfriday.Run([]byte(text))
	html := bluemonday.UGCPolicy().SanitizeBytes(unsafe)

	m := Message{
		ID: id,
		Time: time.Now().UTC().Format(
			"Mon Jan 02 2006 15:04:05 GMT-0700 (MST)"),
		HTML: string(html),
	}

	err = c.store.AddMessage(room, m)
	if err != nil {
		return errors.New("failed to add message to store: " + err.Error())
	}

	go r.broadcast(from, m)

	return nil
}
