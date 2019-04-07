package main

import (
	"encoding/json"

	"github.com/dimuls/gochat/chat"
	"github.com/go-redis/redis"
)

type redisChatStore struct {
	redis              *redis.Client
	maxMessagesPerRoom uint
}

func newRedisChatStore(opt redis.Options, maxMessagesPerRoom uint) *redisChatStore {
	client := redis.NewClient(&opt)
	return &redisChatStore{
		redis:              client,
		maxMessagesPerRoom: maxMessagesPerRoom,
	}
}

func (s *redisChatStore) GetMessageID(room string) (uint64, error) {
	id, err := s.redis.Incr(room + "/message_id").Result()
	return uint64(id), err
}

func (s *redisChatStore) AddMessage(room string, m chat.Message) error {
	key := room + "/messages"

	mJSON, err := json.Marshal(m)
	if err != nil {
		return err
	}

	pipe := s.redis.TxPipeline()

	pipe.LPush(key, string(mJSON))
	pipe.LTrim(key, 0, int64(s.maxMessagesPerRoom-1))

	_, err = pipe.Exec()

	return err
}

func (s *redisChatStore) GetMessages(room string) ([]chat.Message, error) {
	res, err := s.redis.LRange(room+"/messages", 0,
		int64(s.maxMessagesPerRoom-1)).Result()
	if err != nil {
		return nil, err
	}

	var msgs []chat.Message

	for i := len(res) - 1; i >= 0; i-- {
		var msg chat.Message

		err := json.Unmarshal([]byte(res[i]), &msg)
		if err != nil {
			return nil, err
		}

		msgs = append(msgs, msg)
	}

	return msgs, nil
}
