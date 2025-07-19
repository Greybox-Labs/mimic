package storage

import (
	"time"
)

type Session struct {
	ID          int       `json:"id"`
	SessionName string    `json:"session_name"`
	CreatedAt   time.Time `json:"created_at"`
	Description string    `json:"description"`
}

type Interaction struct {
	ID              int       `json:"id"`
	SessionID       int       `json:"session_id"`
	RequestID       string    `json:"request_id"`
	Protocol        string    `json:"protocol"`
	Method          string    `json:"method"`
	Endpoint        string    `json:"endpoint"`
	RequestHeaders  string    `json:"request_headers"`
	RequestBody     []byte    `json:"request_body"`
	ResponseStatus  int       `json:"response_status"`
	ResponseHeaders string    `json:"response_headers"`
	ResponseBody    []byte    `json:"response_body"`
	Timestamp       time.Time `json:"timestamp"`
	SequenceNumber  int       `json:"sequence_number"`
	Metadata        string    `json:"metadata"`
}

type InteractionRequest struct {
	Headers map[string]string `json:"headers"`
	Body    interface{}       `json:"body"`
}

type InteractionResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    interface{}       `json:"body"`
}

type ExportData struct {
	Version      string              `json:"version"`
	Session      Session             `json:"session"`
	Interactions []ExportInteraction `json:"interactions"`
}

type ExportInteraction struct {
	RequestID      string              `json:"request_id"`
	Protocol       string              `json:"protocol"`
	Method         string              `json:"method"`
	Endpoint       string              `json:"endpoint"`
	Request        InteractionRequest  `json:"request"`
	Response       InteractionResponse `json:"response"`
	Timestamp      time.Time           `json:"timestamp"`
	SequenceNumber int                 `json:"sequence_number"`
}
