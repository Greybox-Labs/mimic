package proxy

import (
	"fmt"
	"time"

	"google.golang.org/grpc/encoding"
)

// RawMessage is a message that holds raw bytes for gRPC proxying
type RawMessage struct {
	Data []byte
}

// Reset implements proto.Message
func (m *RawMessage) Reset() { m.Data = nil }

// String implements proto.Message
func (m *RawMessage) String() string { return fmt.Sprintf("RawMessage{%d bytes}", len(m.Data)) }

// ProtoMessage implements proto.Message
func (m *RawMessage) ProtoMessage() {}

// XXX_WellKnownType is used by gRPC-Go to determine if this message is well-known
func (m *RawMessage) XXX_WellKnownType() string { return "" }

// Marshal implements proto.Marshal
func (m *RawMessage) Marshal() ([]byte, error) {
	return m.Data, nil
}

// Unmarshal implements proto.Unmarshal
func (m *RawMessage) Unmarshal(data []byte) error {
	m.Data = make([]byte, len(data))
	copy(m.Data, data)
	return nil
}

// Size returns the size of the marshaled message
func (m *RawMessage) Size() int {
	return len(m.Data)
}

// XXX_MessageName returns message name
func (m *RawMessage) XXX_MessageName() string {
	return "mimic.RawMessage"
}

// rawCodec implements encoding.Codec for raw byte handling
type rawCodec struct{}

func (rawCodec) Marshal(v interface{}) ([]byte, error) {
	switch m := v.(type) {
	case *RawMessage:
		return m.Data, nil
	case RawMessage:
		return m.Data, nil
	case []byte:
		return m, nil
	default:
		return nil, fmt.Errorf("unsupported message type: %T", v)
	}
}

func (rawCodec) Unmarshal(data []byte, v interface{}) error {
	switch m := v.(type) {
	case *RawMessage:
		m.Data = make([]byte, len(data))
		copy(m.Data, data)
		return nil
	case *[]byte:
		*m = data
		return nil
	default:
		return fmt.Errorf("unsupported message type: %T", v)
	}
}

func (rawCodec) Name() string { return "raw" }

// GetRawCodec returns a raw codec instance for gRPC
func GetRawCodec() encoding.Codec {
	return rawCodec{}
}

// RegisterRawCodec registers the raw codec for gRPC usage
func RegisterRawCodec() {
	encoding.RegisterCodec(rawCodec{})
}

// GenerateRequestID generates a unique request ID for gRPC interactions
func GenerateRequestID() string {
	return fmt.Sprintf("grpc-%d", time.Now().UnixNano())
}