package mock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"mimic/config"
	"mimic/proxy"
	"mimic/storage"
)

// mockRawMessage for handling raw gRPC message data in mock responses  
type mockRawMessage struct {
	Data []byte
}

// Reset implements proto.Message
func (m *mockRawMessage) Reset() {
	m.Data = nil
}

// String implements proto.Message
func (m *mockRawMessage) String() string {
	return fmt.Sprintf("mockRawMessage{%d bytes}", len(m.Data))
}

// ProtoMessage implements proto.Message
func (m *mockRawMessage) ProtoMessage() {}

// Marshal implements protobuf marshaling
func (m *mockRawMessage) Marshal() ([]byte, error) {
	return m.Data, nil
}

// Unmarshal implements protobuf unmarshaling  
func (m *mockRawMessage) Unmarshal(data []byte) error {
	m.Data = make([]byte, len(data))
	copy(m.Data, data)
	return nil
}

// Size returns the size of the message
func (m *mockRawMessage) Size() int {
	return len(m.Data)
}

type MockEngine struct {
	proxyConfig   *config.ProxyConfig
	database      *storage.Database
	restHandler   *proxy.RESTHandler
	grpcHandler   *proxy.GRPCHandler
	grpcServer    *grpc.Server
	session       *storage.Session
	sequenceState map[string]int
	sequenceMutex sync.RWMutex
	webServer     WebBroadcaster
}

type WebBroadcaster interface {
	BroadcastRequest(method, endpoint, sessionName, remoteAddr, requestID string, headers map[string]interface{}, body string)
	BroadcastResponse(method, endpoint, sessionName, remoteAddr, requestID string, status int, headers map[string]interface{}, body string)
}

func NewMockEngine(proxyConfig config.ProxyConfig, db *storage.Database) (*MockEngine, error) {
	return NewMockEngineWithBroadcaster(proxyConfig, db, nil)
}

func NewMockEngineWithBroadcaster(proxyConfig config.ProxyConfig, db *storage.Database, webServer WebBroadcaster) (*MockEngine, error) {
	session, err := db.GetOrCreateSession(proxyConfig.SessionName, "Mock session")
	if err != nil {
		return nil, fmt.Errorf("failed to get or create session: %w", err)
	}

	restHandler := proxy.NewRESTHandler([]string{}) // Use empty redact patterns for now
	grpcHandler := proxy.NewGRPCHandler([]string{}) // Use empty redact patterns for now

	var grpcServer *grpc.Server
	if proxyConfig.Protocol == "grpc" {
		grpcServer = grpc.NewServer(
			grpc.MaxRecvMsgSize(64*1024*1024),       // 64MB max receive message size
			grpc.MaxSendMsgSize(64*1024*1024),       // 64MB max send message size
			grpc.MaxHeaderListSize(64*1024*1024),    // 64MB max header list size
			grpc.InitialWindowSize(64*1024*1024),    // 64MB initial window
			grpc.InitialConnWindowSize(64*1024*1024), // 64MB connection window
			grpc.UnknownServiceHandler(func(srv interface{}, stream grpc.ServerStream) error {
				return handleGRPCMockRequest(stream, db, session, grpcHandler)
			}),
		)
	}

	return &MockEngine{
		proxyConfig:   &proxyConfig,
		database:      db,
		restHandler:   restHandler,
		grpcHandler:   grpcHandler,
		grpcServer:    grpcServer,
		session:       session,
		sequenceState: make(map[string]int),
		webServer:     webServer,
	}, nil
}

func (m *MockEngine) Start() error {
	address := "0.0.0.0:8080" // This method shouldn't be used in multi-proxy mode

	if m.proxyConfig.Protocol == "grpc" {
		return m.startGRPCMockServer(address)
	} else {
		return m.startHTTPMockServer(address)
	}
}

func (m *MockEngine) startHTTPMockServer(address string) error {
	mux := http.NewServeMux()

	// Register web UI routes if webServer is available
	if webServer, ok := m.webServer.(interface{ RegisterRoutes(*http.ServeMux) }); ok {
		webServer.RegisterRoutes(mux)
	}

	// All other requests go to mock handler
	mux.HandleFunc("/", m.handleRequest)

	log.Printf("Starting HTTP mock server in %s mode on %s", m.proxyConfig.Mode, address)
	log.Printf("Serving mocked responses for session: %s", m.session.SessionName)

	return http.ListenAndServe(address, mux)
}

func (m *MockEngine) startGRPCMockServer(address string) error {
	if m.grpcServer == nil {
		return fmt.Errorf("gRPC mock server not initialized")
	}

	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}

	log.Printf("Starting gRPC mock server in %s mode on %s", m.proxyConfig.Mode, address)
	log.Printf("Serving mocked responses for session: %s", m.session.SessionName)

	return m.grpcServer.Serve(lis)
}

// HandleRequest implements the ProxyHandler interface
func (m *MockEngine) HandleRequest(w http.ResponseWriter, r *http.Request) {
	m.handleRequest(w, r)
}

func (m *MockEngine) handleRequest(w http.ResponseWriter, r *http.Request) {
	log.Printf("[MOCK] %s %s %s", r.Method, r.URL.Path, r.RemoteAddr)

	// Broadcast request event if web server is available
	if m.webServer != nil {
		requestHeaders := make(map[string]interface{})
		for key, values := range r.Header {
			requestHeaders[key] = strings.Join(values, ", ")
		}

		var requestBody string
		if r.Body != nil {
			bodyBytes, err := io.ReadAll(r.Body)
			if err == nil {
				requestBody = string(bodyBytes)
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
		}

		m.webServer.BroadcastRequest(r.Method, r.URL.Path, m.session.SessionName, r.RemoteAddr, "", requestHeaders, requestBody)
	}

	interactions, err := m.database.FindMatchingInteractions(m.session.ID, r.Method, r.URL.Path)
	if err != nil {
		log.Printf("Error finding matching interactions: %v", err)
		m.sendNotFoundResponse(w)
		return
	}

	if len(interactions) == 0 {
		log.Printf("No matching interactions found for %s %s", r.Method, r.URL.Path)
		m.sendNotFoundResponse(w)
		return
	}

	// Filter interactions based on headers and body matching
	matchingInteractions := m.filterMatchingInteractions(interactions, r)

	if len(matchingInteractions) == 0 {
		log.Printf("No interactions match request headers/body for %s %s", r.Method, r.URL.Path)
		m.sendNotFoundResponse(w)
		return
	}

	// Select interaction based on sequence order (default behavior)
	selectedInteraction := m.selectSequentialInteraction(matchingInteractions, r)

	if selectedInteraction == nil {
		log.Printf("No suitable interaction found for %s %s", r.Method, r.URL.Path)
		m.sendNotFoundResponse(w)
		return
	}

	// Broadcast response event if web server is available
	if m.webServer != nil {
		var responseHeaders map[string]interface{}
		json.Unmarshal([]byte(selectedInteraction.ResponseHeaders), &responseHeaders)
		responseBody := string(selectedInteraction.ResponseBody)
		m.webServer.BroadcastResponse(selectedInteraction.Method, selectedInteraction.Endpoint, m.session.SessionName, r.RemoteAddr, selectedInteraction.RequestID, selectedInteraction.ResponseStatus, responseHeaders, responseBody)
	}

	if err := m.sendMockResponse(w, selectedInteraction); err != nil {
		log.Printf("Error sending mock response: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	log.Printf("Served mock response: %s %s -> %d (sequence: %d)",
		selectedInteraction.Method, selectedInteraction.Endpoint,
		selectedInteraction.ResponseStatus, selectedInteraction.SequenceNumber)
}

func (m *MockEngine) filterMatchingInteractions(interactions []storage.Interaction, r *http.Request) []storage.Interaction {
	var matches []storage.Interaction

	for _, interaction := range interactions {
		if m.matchesRequestContent(interaction, r) {
			matches = append(matches, interaction)
		}
	}

	return matches
}

func (m *MockEngine) matchesRequestContent(interaction storage.Interaction, r *http.Request) bool {
	// Compare headers (ignoring redacted fields)
	if !m.matchesHeaders(interaction.RequestHeaders, r.Header) {
		return false
	}

	// Compare body
	if !m.matchesBody(interaction.RequestBody, r) {
		return false
	}

	return true
}

func (m *MockEngine) matchesHeaders(recordedHeaders string, requestHeaders http.Header) bool {
	// Parse recorded headers
	var recorded map[string]string
	if recordedHeaders != "" {
		if err := json.Unmarshal([]byte(recordedHeaders), &recorded); err != nil {
			return false
		}
	} else {
		recorded = make(map[string]string)
	}

	// Convert request headers to same format
	current := make(map[string]string)
	for key, values := range requestHeaders {
		current[key] = strings.Join(values, ", ")
	}

	// Apply redaction to both for comparison
	recordedJSON, _ := json.Marshal(recorded)
	currentJSON, _ := json.Marshal(current)

	recordedRedacted := m.redactSensitiveData(string(recordedJSON))
	currentRedacted := m.redactSensitiveData(string(currentJSON))

	return recordedRedacted == currentRedacted
}

func (m *MockEngine) matchesBody(recordedBody []byte, r *http.Request) bool {
	// Read current request body
	var currentBody []byte
	if r.Body != nil {
		var err error
		currentBody, err = io.ReadAll(r.Body)
		if err != nil {
			return false
		}
		// Restore body for further processing
		r.Body = io.NopCloser(bytes.NewBuffer(currentBody))
	}

	// Compare bodies
	return bytes.Equal(recordedBody, currentBody)
}

func (m *MockEngine) redactSensitiveData(data string) string {
	result := data
	// Use the same redaction patterns as the REST handler
	for _, pattern := range m.restHandler.GetRedactPatterns() {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}

func (m *MockEngine) getRequestSignature(r *http.Request) (string, error) {
	// Extract and normalize headers (with redaction)
	headers := make(map[string]string)
	for key, values := range r.Header {
		headers[key] = strings.Join(values, ", ")
	}

	headersJSON, err := json.Marshal(headers)
	if err != nil {
		return "", err
	}

	headersStr := m.redactSensitiveData(string(headersJSON))

	// Read body
	var body []byte
	if r.Body != nil {
		body, err = io.ReadAll(r.Body)
		if err != nil {
			return "", err
		}
		// Restore body for further processing
		r.Body = io.NopCloser(bytes.NewBuffer(body))
	}

	// Create signature
	signature := fmt.Sprintf("%s:%s:%s:%s", r.Method, r.URL.Path, headersStr, string(body))
	return signature, nil
}

func (m *MockEngine) selectSequentialInteraction(interactions []storage.Interaction, r *http.Request) *storage.Interaction {
	if len(interactions) == 0 {
		return nil
	}

	// Get request signature for sequence tracking
	signature, err := m.getRequestSignature(r)
	if err != nil {
		log.Printf("Error generating request signature: %v", err)
		// Fallback to basic signature
		signature = fmt.Sprintf("%s:%s", r.Method, r.URL.Path)
	}

	m.sequenceMutex.Lock()
	defer m.sequenceMutex.Unlock()

	currentSequence := m.sequenceState[signature]

	// Find the next interaction in sequence
	for _, interaction := range interactions {
		if interaction.SequenceNumber > currentSequence {
			m.sequenceState[signature] = interaction.SequenceNumber
			return &interaction
		}
	}

	// If we've reached the end, cycle back to the beginning
	if len(interactions) > 0 {
		m.sequenceState[signature] = interactions[0].SequenceNumber
		return &interactions[0]
	}

	return nil
}

func (m *MockEngine) selectOrderedInteraction(interactions []storage.Interaction, r *http.Request) *storage.Interaction {
	// Delegate to the new sequential interaction method for backward compatibility
	return m.selectSequentialInteraction(interactions, r)
}

func (m *MockEngine) selectRandomInteraction(interactions []storage.Interaction, r *http.Request) *storage.Interaction {
	if len(interactions) == 0 {
		return nil
	}

	for _, interaction := range interactions {
		if m.restHandler.MatchRequest(r, &interaction, "exact") { // Default to exact matching
			return &interaction
		}
	}

	return &interactions[0]
}

func (m *MockEngine) sendMockResponse(w http.ResponseWriter, interaction *storage.Interaction) error {
	var headers map[string]string
	if interaction.ResponseHeaders != "" {
		if err := json.Unmarshal([]byte(interaction.ResponseHeaders), &headers); err != nil {
			return fmt.Errorf("failed to unmarshal response headers: %w", err)
		}
	}

	for key, value := range headers {
		w.Header().Set(key, value)
	}

	w.WriteHeader(interaction.ResponseStatus)

	if len(interaction.ResponseBody) > 0 {
		_, err := w.Write(interaction.ResponseBody)
		if err != nil {
			return fmt.Errorf("failed to write response body: %w", err)
		}
	}

	return nil
}

func (m *MockEngine) sendNotFoundResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(404) // Default not found status

	notFoundBody := map[string]interface{}{
		"error": "Recording not found",
	}
	if err := json.NewEncoder(w).Encode(notFoundBody); err != nil {
		log.Printf("Error encoding not found response: %v", err)
	}
}

func (m *MockEngine) Stop() error {
	if m.grpcServer != nil {
		m.grpcServer.GracefulStop()
	}
	return nil
}

func (m *MockEngine) GetGRPCServer() *grpc.Server {
	return m.grpcServer
}

func (m *MockEngine) ResetSequenceState() {
	m.sequenceMutex.Lock()
	defer m.sequenceMutex.Unlock()

	m.sequenceState = make(map[string]int)
	log.Printf("Reset sequence state for mock engine")
}

func (m *MockEngine) GetSequenceState() map[string]int {
	m.sequenceMutex.RLock()
	defer m.sequenceMutex.RUnlock()

	state := make(map[string]int)
	for key, value := range m.sequenceState {
		state[key] = value
	}

	return state
}

// handleGRPCMockRequest handles gRPC mock requests
func handleGRPCMockRequest(stream grpc.ServerStream, db *storage.Database, session *storage.Session, grpcHandler *proxy.GRPCHandler) error {
	fullMethodName, ok := grpc.MethodFromServerStream(stream)
	if !ok {
		return status.Errorf(codes.Internal, "failed to get method from stream")
	}

	log.Printf("[GRPC MOCK] %s", fullMethodName)

	// Find matching gRPC interactions
	interactions, err := db.FindMatchingInteractions(session.ID, fullMethodName, fullMethodName)
	if err != nil {
		log.Printf("Error finding matching gRPC interactions: %v", err)
		return status.Errorf(codes.Internal, "failed to find matching interactions")
	}

	if len(interactions) == 0 {
		log.Printf("No matching gRPC interactions found for %s", fullMethodName)
		return status.Errorf(codes.NotFound, "no recorded interaction found for method %s", fullMethodName)
	}

	// For simplicity, use the first matching interaction
	// In a more sophisticated implementation, we could add sequence support for gRPC
	selectedInteraction := &interactions[0]

	// Create a mock gRPC response
	// Note: This is a simplified implementation
	// In practice, we would need to handle protobuf message types dynamically

	// Send response headers/metadata if present
	if selectedInteraction.ResponseHeaders != "" {
		var metadataMap map[string][]string
		if err := json.Unmarshal([]byte(selectedInteraction.ResponseHeaders), &metadataMap); err == nil {
			md := metadata.New(nil)
			for key, values := range metadataMap {
				md.Set(key, values...)
			}
			if err := stream.SendHeader(md); err != nil {
				return err
			}
		}
	}

	// First, receive the request message from the client (required for unary calls)
	var requestMsg mockRawMessage
	if err := stream.RecvMsg(&requestMsg); err != nil {
		log.Printf("Error receiving request message: %v", err)
		return status.Errorf(codes.Internal, "failed to receive request: %v", err)
	}

	// Send the recorded response body if available
	if len(selectedInteraction.ResponseBody) > 0 {
		// Create a raw message with the recorded response data
		responseMsg := mockRawMessage{Data: selectedInteraction.ResponseBody}
		if err := stream.SendMsg(&responseMsg); err != nil {
			return status.Errorf(codes.Internal, "failed to send response: %v", err)
		}
		log.Printf("Served gRPC mock response: %s -> %d (%d bytes)", fullMethodName, selectedInteraction.ResponseStatus, len(selectedInteraction.ResponseBody))
	} else {
		log.Printf("Served gRPC mock response: %s -> %d (empty response)", fullMethodName, selectedInteraction.ResponseStatus)
	}

	return nil
}
