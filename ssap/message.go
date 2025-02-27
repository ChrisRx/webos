package ssap

type MessageType string

const (
	RegisterMessageType   MessageType = "register"
	RegisteredMessageType MessageType = "registered"
	RequestMessageType    MessageType = "request"
	ResponseMessageType   MessageType = "response"
	ErrorMessageType      MessageType = "error"
)

type Message struct {
	Type    MessageType    `json:"type,omitempty"`
	ID      string         `json:"id,omitempty"`
	URI     Command        `json:"uri,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
	Error   string         `json:"error,omitempty"`
}
