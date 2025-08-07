package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/google/uuid"
	"mimic/storage"
)

type GRPCHandler struct {
	redactPatterns []*regexp.Regexp
}

func NewGRPCHandler(redactPatterns []string) *GRPCHandler {
	patterns := make([]*regexp.Regexp, len(redactPatterns))
	for i, pattern := range redactPatterns {
		if compiled, err := regexp.Compile(pattern); err == nil {
			patterns[i] = compiled
		}
	}
	return &GRPCHandler{redactPatterns: patterns}
}

type GRPCRequest struct {
	Method   string
	Metadata metadata.MD
	Message  proto.Message
}

type GRPCResponse struct {
	Status   *status.Status
	Metadata metadata.MD
	Message  proto.Message
}

func (h *GRPCHandler) ExtractGRPCRequest(method string, md metadata.MD, req proto.Message) (*storage.Interaction, error) {
	requestID := uuid.New().String()

	// Convert metadata to JSON
	metadataMap := make(map[string][]string)
	for key, values := range md {
		metadataMap[key] = values
	}

	headersJSON, err := json.Marshal(metadataMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gRPC metadata: %w", err)
	}

	headersStr := string(headersJSON)
	headersStr = h.redactSensitiveData(headersStr)

	// Convert protobuf message to JSON for storage
	var body []byte
	if req != nil {
		jsonBytes, err := protojson.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal protobuf message to JSON: %w", err)
		}
		body = jsonBytes
	}

	return &storage.Interaction{
		RequestID:      requestID,
		Protocol:       "gRPC",
		Method:         method,
		Endpoint:       method, // For gRPC, method is the endpoint
		RequestHeaders: headersStr,
		RequestBody:    body,
		Timestamp:      time.Now(),
	}, nil
}

func (h *GRPCHandler) ExtractGRPCResponse(st *status.Status, md metadata.MD, resp proto.Message) (int, string, []byte, error) {
	// Convert status to HTTP-like status code for storage
	statusCode := int(st.Code())

	// Convert metadata to JSON
	metadataMap := make(map[string][]string)
	for key, values := range md {
		metadataMap[key] = values
	}

	headersJSON, err := json.Marshal(metadataMap)
	if err != nil {
		return 0, "", nil, fmt.Errorf("failed to marshal gRPC response metadata: %w", err)
	}

	headersStr := string(headersJSON)
	headersStr = h.redactSensitiveData(headersStr)

	// Convert protobuf message to JSON for storage
	var body []byte
	if resp != nil {
		jsonBytes, err := protojson.Marshal(resp)
		if err != nil {
			return 0, "", nil, fmt.Errorf("failed to marshal protobuf response to JSON: %w", err)
		}
		body = jsonBytes
	}

	return statusCode, headersStr, body, nil
}

func (h *GRPCHandler) CreateGRPCResponse(interaction *storage.Interaction, messageType protoreflect.MessageType) (*GRPCResponse, error) {
	// Parse stored metadata
	var metadataMap map[string][]string
	if interaction.ResponseHeaders != "" {
		if err := json.Unmarshal([]byte(interaction.ResponseHeaders), &metadataMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response metadata: %w", err)
		}
	}

	md := metadata.New(nil)
	for key, values := range metadataMap {
		md.Set(key, values...)
	}

	// Create status from stored status code
	st := status.New(codes.Code(interaction.ResponseStatus), "")

	// Parse stored message
	var message proto.Message
	if len(interaction.ResponseBody) > 0 && messageType != nil {
		message = dynamicpb.NewMessage(messageType.Descriptor())
		if err := protojson.Unmarshal(interaction.ResponseBody, message); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response message: %w", err)
		}
	}

	return &GRPCResponse{
		Status:   st,
		Metadata: md,
		Message:  message,
	}, nil
}

func (h *GRPCHandler) MatchGRPCRequest(method string, md metadata.MD, interaction *storage.Interaction, strategy string) bool {
	switch strategy {
	case "exact":
		return h.exactGRPCMatch(method, md, interaction)
	case "pattern":
		return h.patternGRPCMatch(method, interaction)
	case "fuzzy":
		return h.fuzzyGRPCMatch(method, interaction)
	default:
		return h.exactGRPCMatch(method, md, interaction)
	}
}

func (h *GRPCHandler) exactGRPCMatch(method string, md metadata.MD, interaction *storage.Interaction) bool {
	return method == interaction.Method
}

func (h *GRPCHandler) patternGRPCMatch(method string, interaction *storage.Interaction) bool {
	pattern, err := regexp.Compile(interaction.Method)
	if err != nil {
		return false
	}
	return pattern.MatchString(method)
}

func (h *GRPCHandler) fuzzyGRPCMatch(method string, interaction *storage.Interaction) bool {
	// Simple fuzzy matching for gRPC methods
	// This could be enhanced with more sophisticated matching logic
	return method == interaction.Method
}

func (h *GRPCHandler) redactSensitiveData(data string) string {
	result := data
	for _, pattern := range h.redactPatterns {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}

func (h *GRPCHandler) GetRedactPatterns() []*regexp.Regexp {
	return h.redactPatterns
}

// GRPCInterceptor creates a grpc.UnaryServerInterceptor for recording
func (h *GRPCHandler) GRPCInterceptor(db *storage.Database, session *storage.Session) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, _ := metadata.FromIncomingContext(ctx)

		// Extract request
		protoReq, ok := req.(proto.Message)
		if !ok {
			return handler(ctx, req)
		}

		interaction, err := h.ExtractGRPCRequest(info.FullMethod, md, protoReq)
		if err != nil {
			return handler(ctx, req)
		}

		interaction.SessionID = session.ID

		// Call the actual handler
		resp, err := handler(ctx, req)

		// Extract response
		var protoResp proto.Message
		if resp != nil {
			if pr, ok := resp.(proto.Message); ok {
				protoResp = pr
			}
		}

		st, _ := status.FromError(err)
		outMd, _ := metadata.FromOutgoingContext(ctx)

		statusCode, headers, body, extractErr := h.ExtractGRPCResponse(st, outMd, protoResp)
		if extractErr == nil {
			interaction.ResponseStatus = statusCode
			interaction.ResponseHeaders = headers
			interaction.ResponseBody = body

			if recordErr := db.RecordInteraction(interaction); recordErr != nil {
				// Log error but don't fail the request
			}
		}

		return resp, err
	}
}
