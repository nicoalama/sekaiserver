package protocol

type MessageType string

const (
	MsgAuth        MessageType = "auth"
	MsgAuthOK      MessageType = "auth_ok"
	MsgRequest     MessageType = "request"
	MsgResponse    MessageType = "response"
	MsgStreamChunk MessageType = "stream_chunk"
	MsgStreamEnd   MessageType = "stream_end"
	MsgHeartbeat   MessageType = "heartbeat"
	MsgError       MessageType = "error"
)

type Message struct {
	Type    MessageType `json:"type"`
	Payload string      `json:"payload,omitempty"`
}

type AuthPayload struct {
	Code string `json:"code"`
	Key  string `json:"key"`
}

type RequestPayload struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body,omitempty"`
	Stream  bool              `json:"stream"`
}

type ResponsePayload struct {
	ID         string            `json:"id"`
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       *string           `json:"body,omitempty"`
}

type StreamChunkPayload struct {
	ID   string `json:"id"`
	Data string `json:"data"`
}

type StreamEndPayload struct {
	ID string `json:"id"`
}
