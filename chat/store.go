package chat

type Store interface {
	GetMessageID(room string) (uint64, error)
	AddMessage(room string, m Message) error
	GetMessages(room string) ([]Message, error)
}
