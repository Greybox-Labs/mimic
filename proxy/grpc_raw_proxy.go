package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"mimic/config"
	"mimic/storage"
)

// RawGRPCProxy implements raw byte-level gRPC proxying
type RawGRPCProxy struct {
	config    *config.ProxyConfig
	mode      string // Global mode: "record" or "mock"
	database  *storage.Database
	session   *storage.Session
	handler   *GRPCHandler
	webServer WebBroadcaster
}



func NewRawGRPCProxy(proxyConfig *config.ProxyConfig, mode string, db *storage.Database, session *storage.Session, grpcHandler *GRPCHandler) *RawGRPCProxy {
	return &RawGRPCProxy{
		config:    proxyConfig,
		mode:      mode,
		database:  db,
		session:   session,
		handler:   grpcHandler,
		webServer: nil, // Will be set by proxy engine
	}
}

func (p *RawGRPCProxy) SetWebBroadcaster(wb WebBroadcaster) {
	p.webServer = wb
}



// GetUnknownServiceHandler returns a handler that can proxy any gRPC service using raw bytes
func (p *RawGRPCProxy) GetUnknownServiceHandler() grpc.StreamHandler {
	// Register our raw codec
	RegisterRawCodec()

	return func(srv interface{}, stream grpc.ServerStream) error {
		fullMethodName, ok := grpc.MethodFromServerStream(stream)
		if !ok {
			return status.Errorf(codes.Internal, "failed to get method from stream")
		}

		log.Printf("Raw proxy handling: %s", fullMethodName)

		// Create connection to target
		targetAddr := fmt.Sprintf("%s:%d", p.config.TargetHost, p.config.TargetPort)
		ctx := stream.Context()

		// Determine if we should use TLS based on port
		var creds credentials.TransportCredentials
		if p.config.TargetPort == 443 || p.config.Protocol == "https" {
			creds = credentials.NewTLS(nil) // Use system root CAs
		} else {
			creds = insecure.NewCredentials()
		}

		conn, err := grpc.DialContext(ctx, targetAddr,
			grpc.WithTransportCredentials(creds),
			grpc.WithInitialWindowSize(64*1024*1024),     // 64MB initial window
			grpc.WithInitialConnWindowSize(64*1024*1024), // 64MB connection window
			grpc.WithReadBufferSize(1024*1024),           // 1MB read buffer
			grpc.WithWriteBufferSize(1024*1024),          // 1MB write buffer
			grpc.WithDefaultCallOptions(
				grpc.MaxCallRecvMsgSize(64*1024*1024),
				grpc.MaxCallSendMsgSize(64*1024*1024),
			),
		)
		if err != nil {
			return status.Errorf(codes.Unavailable, "failed to connect to backend %s: %v", targetAddr, err)
		}
		defer conn.Close()

		// Determine if this is a unary vs streaming call
		if p.isLikelyUnaryCall(fullMethodName) {
			return p.handleUnaryCall(ctx, conn, stream, fullMethodName)
		}
		// Create client stream using raw codec
		clientStream, err := conn.NewStream(
			ctx,
			&grpc.StreamDesc{
				StreamName:    fullMethodName,
				ServerStreams: true,
				ClientStreams: true,
			},
			fullMethodName,
			grpc.ForceCodec(GetRawCodec()),
		)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to create client stream for %s: %v", fullMethodName, err)
		}

		return p.proxyRawStream(stream, clientStream, fullMethodName)
	}
}

// proxyRawStream proxies using raw message handling
func (p *RawGRPCProxy) proxyRawStream(serverStream grpc.ServerStream, clientStream grpc.ClientStream, method string) error {
	errCh := make(chan error, 2)

	// Proxy client->server (requests)
	go func() {
		defer func() {
			clientStream.CloseSend()
		}()

		for {
			var msg RawMessage
			if err := serverStream.RecvMsg(&msg); err != nil {
				if err == io.EOF {
					errCh <- nil
					return
				}
				errCh <- fmt.Errorf("server recv error: %w", err)
				return
			}

			log.Printf("→ %s: %d bytes", method, len(msg.Data))

			if err := clientStream.SendMsg(msg); err != nil {
				errCh <- fmt.Errorf("client send error: %w", err)
				return
			}
		}
	}()

	// Proxy server->client (responses)
	go func() {
		for {
			var msg RawMessage
			if err := clientStream.RecvMsg(&msg); err != nil {
				if err == io.EOF {
					errCh <- nil
					return
				}
				errCh <- fmt.Errorf("client recv error: %w", err)
				return
			}

			log.Printf("← %s: %d bytes", method, len(msg.Data))

			if err := serverStream.SendMsg(msg); err != nil {
				errCh <- fmt.Errorf("server send error: %w", err)
				return
			}
		}
	}()

	return <-errCh
}



func (p *RawGRPCProxy) metadataToJSON(md metadata.MD) string {
	metadataMap := make(map[string][]string)
	for key, values := range md {
		metadataMap[key] = values
	}
	jsonBytes, err := json.Marshal(metadataMap)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func (p *RawGRPCProxy) metadataToMap(md metadata.MD) map[string]interface{} {
	result := make(map[string]interface{})
	for key, values := range md {
		if len(values) == 1 {
			result[key] = values[0]
		} else {
			result[key] = values
		}
	}
	return result
}

// isLikelyUnaryCall heuristically determines if a method is likely a unary call
func (p *RawGRPCProxy) isLikelyUnaryCall(method string) bool {
	// Methods with streaming patterns are definitely streaming
	streamingPatterns := []string{
		"Stream", "Watch", "Subscribe", "Listen", "Monitor", "Observe",
	}
	
	for _, pattern := range streamingPatterns {
		if strings.Contains(method, pattern) {
			return false
		}
	}
	
	// Common patterns for unary calls
	unaryPatterns := []string{
		"Get", "Create", "Update", "Delete", "Check", "Validate", 
		"Info", "Status", "Health", "Ping", "Version", "List",
	}
	
	for _, pattern := range unaryPatterns {
		if strings.Contains(method, pattern) {
			return true
		}
	}
	
	// Default to unary for unknown patterns
	return true
}

// handleUnaryCall handles unary gRPC calls
func (p *RawGRPCProxy) handleUnaryCall(ctx context.Context, conn *grpc.ClientConn, stream grpc.ServerStream, method string) error {
	// Receive the request from client
	var requestMsg RawMessage
	if err := stream.RecvMsg(&requestMsg); err != nil {
		return status.Errorf(codes.Internal, "failed to receive request: %v", err)
	}

	if p.mode == "record" {
		log.Printf("→ %s: %d bytes (unary)", method, len(requestMsg.Data))
	}

	// Extract and forward metadata
	md, _ := metadata.FromIncomingContext(stream.Context())
	outCtx := metadata.NewOutgoingContext(ctx, md)
	
	// Create interaction record for database storage
	var interaction *storage.Interaction
	if p.mode == "record" {
		interaction = &storage.Interaction{
			RequestID:      GenerateRequestID(),
			SessionID:      p.session.ID,
			Protocol:       "gRPC",
			Method:         method,
			Endpoint:       method,
			RequestHeaders: p.metadataToJSON(md),
			RequestBody:    requestMsg.Data,
			Timestamp:      time.Now(),
		}

		// Broadcast request event to web UI
		if p.webServer != nil {
			log.Printf("[DEBUG] Broadcasting gRPC request to web UI: %s", method)
			headers := p.metadataToMap(md)
			body := fmt.Sprintf("gRPC raw message (%d bytes)", len(requestMsg.Data))
			p.webServer.BroadcastRequest(method, method, p.session.SessionName, "grpc-client", interaction.RequestID, headers, body)
		} else {
			log.Printf("[DEBUG] No webServer available for broadcasting gRPC request")
		}
	}
	
	// Forward the unary call to target server
	var responseMsg RawMessage
	err := conn.Invoke(outCtx, method, &requestMsg, &responseMsg, grpc.ForceCodec(GetRawCodec()))
	
	// Handle recording and response
	if p.mode == "record" {
		statusCode := 0
		if err != nil {
			if st, ok := status.FromError(err); ok {
				statusCode = int(st.Code())
			} else {
				statusCode = int(codes.Unknown)
			}
		} else {
			statusCode = int(codes.OK)
		}

		log.Printf("← %s: %d bytes (unary)", method, len(responseMsg.Data))

		// Complete the interaction record
		interaction.ResponseStatus = statusCode
		interaction.ResponseHeaders = "{}" // Empty metadata for now
		interaction.ResponseBody = responseMsg.Data

		// Save to database
		if recordErr := p.database.RecordInteraction(interaction); recordErr != nil {
			log.Printf("Error recording gRPC interaction: %v", recordErr)
		} else {
			log.Printf("Recorded gRPC interaction: %s -> %d", method, statusCode)
		}

		// Broadcast response event to web UI
		if p.webServer != nil {
			log.Printf("[DEBUG] Broadcasting gRPC response to web UI: %s", method)
			responseHeaders := make(map[string]interface{})
			responseBody := fmt.Sprintf("gRPC raw message (%d bytes)", len(responseMsg.Data))
			p.webServer.BroadcastResponse(method, method, p.session.SessionName, "grpc-client", interaction.RequestID, statusCode, responseHeaders, responseBody)
		} else {
			log.Printf("[DEBUG] No webServer available for broadcasting gRPC response")
		}
	}

	if err != nil {
		return err
	}

	// Send response back to client
	if err := stream.SendMsg(&responseMsg); err != nil {
		return status.Errorf(codes.Internal, "failed to send response: %v", err)
	}

	return nil
}