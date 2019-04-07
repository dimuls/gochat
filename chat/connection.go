package chat

type Connection interface {
	SendMessage(msg Message)
	SendMessages(msgs []Message)
	SendClientsCount(count uint)
}
