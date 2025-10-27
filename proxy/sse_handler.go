package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"
)

// SSEEvent represents a single Server-Sent Event
type SSEEvent struct {
	Event string
	Data  string
	ID    string
	Retry int
}

// SSEChunk represents a captured SSE chunk with timing information
type SSEChunk struct {
	RawData   []byte
	Event     *SSEEvent
	Timestamp time.Time
	TimeDelta int64 // Milliseconds since previous chunk
}

// IsSSEResponse checks if a response is an SSE stream based on content-type
func IsSSEResponse(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

// ParseSSEEvent parses a raw SSE event data into an SSEEvent struct
func ParseSSEEvent(data []byte) (*SSEEvent, error) {
	event := &SSEEvent{}
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip comments
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Parse field
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}

		field := parts[0]
		value := strings.TrimSpace(parts[1])

		switch field {
		case "event":
			event.Event = value
		case "data":
			if event.Data != "" {
				event.Data += "\n"
			}
			event.Data += value
		case "id":
			event.ID = value
		case "retry":
			fmt.Sscanf(value, "%d", &event.Retry)
		}
	}

	return event, nil
}

// FormatSSEEvent formats an SSEEvent back into raw SSE format
func FormatSSEEvent(event *SSEEvent) []byte {
	var buf bytes.Buffer

	if event.Event != "" {
		buf.WriteString(fmt.Sprintf("event: %s\n", event.Event))
	}

	if event.ID != "" {
		buf.WriteString(fmt.Sprintf("id: %s\n", event.ID))
	}

	if event.Retry > 0 {
		buf.WriteString(fmt.Sprintf("retry: %d\n", event.Retry))
	}

	// Write data lines
	dataLines := strings.Split(event.Data, "\n")
	for _, line := range dataLines {
		buf.WriteString(fmt.Sprintf("data: %s\n", line))
	}

	// SSE events end with double newline
	buf.WriteString("\n")

	return buf.Bytes()
}

// SSEStreamReader reads and parses SSE events from an io.Reader
type SSEStreamReader struct {
	reader    *bufio.Reader
	buffer    bytes.Buffer
	startTime time.Time
	lastTime  time.Time
}

// NewSSEStreamReader creates a new SSE stream reader
func NewSSEStreamReader(r io.Reader) *SSEStreamReader {
	now := time.Now()
	return &SSEStreamReader{
		reader:    bufio.NewReader(r),
		startTime: now,
		lastTime:  now,
	}
}

// ReadChunk reads the next SSE chunk from the stream
func (r *SSEStreamReader) ReadChunk() (*SSEChunk, error) {
	r.buffer.Reset()

	for {
		line, err := r.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF && r.buffer.Len() > 0 {
				// Return any buffered data
				now := time.Now()
				timeDelta := now.Sub(r.lastTime).Milliseconds()
				r.lastTime = now

				rawData := make([]byte, r.buffer.Len())
				copy(rawData, r.buffer.Bytes())

				event, err := ParseSSEEvent(rawData)
				if err != nil {
					return nil, err
				}

				return &SSEChunk{
					RawData:   rawData,
					Event:     event,
					Timestamp: now,
					TimeDelta: timeDelta,
				}, io.EOF
			}
			return nil, err
		}

		r.buffer.Write(line)

		// SSE events are terminated by a blank line (double newline)
		if len(bytes.TrimSpace(line)) == 0 && r.buffer.Len() > 1 {
			now := time.Now()
			timeDelta := now.Sub(r.lastTime).Milliseconds()
			r.lastTime = now

			rawData := make([]byte, r.buffer.Len())
			copy(rawData, r.buffer.Bytes())

			event, err := ParseSSEEvent(rawData)
			if err != nil {
				return nil, err
			}

			return &SSEChunk{
				RawData:   rawData,
				Event:     event,
				Timestamp: now,
				TimeDelta: timeDelta,
			}, nil
		}
	}
}

// ReadAllChunks reads all chunks from the stream until EOF
func (r *SSEStreamReader) ReadAllChunks() ([]*SSEChunk, error) {
	var chunks []*SSEChunk

	for {
		chunk, err := r.ReadChunk()
		if err == io.EOF {
			if chunk != nil {
				chunks = append(chunks, chunk)
			}
			break
		}
		if err != nil {
			return chunks, err
		}
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// SSEStreamWriter writes SSE events to an http.ResponseWriter
type SSEStreamWriter struct {
	writer  io.Writer
	flusher Flusher
}

// Flusher interface for flushing buffered data
type Flusher interface {
	Flush()
}

// NewSSEStreamWriter creates a new SSE stream writer
func NewSSEStreamWriter(w io.Writer, f Flusher) *SSEStreamWriter {
	return &SSEStreamWriter{
		writer:  w,
		flusher: f,
	}
}

// WriteChunk writes a single SSE chunk
func (w *SSEStreamWriter) WriteChunk(chunk *SSEChunk) error {
	_, err := w.writer.Write(chunk.RawData)
	if err != nil {
		return err
	}

	if w.flusher != nil {
		w.flusher.Flush()
	}

	return nil
}

// WriteEvent writes a formatted SSE event
func (w *SSEStreamWriter) WriteEvent(event *SSEEvent) error {
	data := FormatSSEEvent(event)
	_, err := w.writer.Write(data)
	if err != nil {
		return err
	}

	if w.flusher != nil {
		w.flusher.Flush()
	}

	return nil
}
