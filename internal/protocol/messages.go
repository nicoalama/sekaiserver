package protocol

type PendingRequest struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body"`
}

type PendingResponse struct {
	Requests []PendingRequest `json:"requests"`
}

type SubmitResponse struct {
	RequestID  string            `json:"requestId"`
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       *string           `json:"body"`
}

type HeartbeatBody struct {
	Code string `json:"code"`
}
