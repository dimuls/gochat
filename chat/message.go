package chat

type Message struct {
	ID      uint64 `json:"id"`
	Time    string `json:"time"`
	HTML    string `json:"html"`
	Approve bool   `json:"approve"`
}
