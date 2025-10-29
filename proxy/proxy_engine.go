package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"mimic/config"
	"mimic/storage"

	"google.golang.org/grpc"
)

type ProxyEngine struct {
	proxyConfig *config.ProxyConfig
	database    *storage.Database
	restHandler *RESTHandler
	grpcHandler *GRPCHandler
	session     *storage.Session
	client      *http.Client
	grpcServer  *grpc.Server
	webServer   WebBroadcaster
}

type WebBroadcaster interface {
	BroadcastRequest(method, endpoint, sessionName, remoteAddr, requestID string, headers map[string]interface{}, body string)
	BroadcastResponse(method, endpoint, sessionName, remoteAddr, requestID string, status int, headers map[string]interface{}, body string)
}

func NewProxyEngine(proxyConfig config.ProxyConfig, db *storage.Database) (*ProxyEngine, error) {
	return NewProxyEngineWithBroadcaster(proxyConfig, db, nil)
}

func NewProxyEngineWithBroadcaster(proxyConfig config.ProxyConfig, db *storage.Database, webServer WebBroadcaster) (*ProxyEngine, error) {
	session, err := db.GetOrCreateSession(proxyConfig.SessionName, "Proxy recording session")
	if err != nil {
		return nil, fmt.Errorf("failed to get or create session: %w", err)
	}

	restHandler := NewRESTHandler([]string{}) // Use empty redact patterns for now
	grpcHandler := NewGRPCHandler([]string{}) // Use empty redact patterns for now

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
			MaxIdleConnsPerHost: 10,
		},
	}

	var grpcServer *grpc.Server

	if proxyConfig.Protocol == "grpc" {
		// Use raw proxy for better compatibility
		rawProxy := NewRawGRPCProxy(&proxyConfig, "record", db, session, grpcHandler)

		// Set web broadcaster if available
		if webServer != nil {
			rawProxy.SetWebBroadcaster(webServer)
		}

		grpcServer = grpc.NewServer(
			grpc.MaxRecvMsgSize(64*1024*1024),        // 64MB max receive message size
			grpc.MaxSendMsgSize(64*1024*1024),        // 64MB max send message size
			grpc.MaxHeaderListSize(64*1024*1024),     // 64MB max header list size
			grpc.InitialWindowSize(64*1024*1024),     // 64MB initial window
			grpc.InitialConnWindowSize(64*1024*1024), // 64MB connection window
			grpc.UnknownServiceHandler(rawProxy.GetUnknownServiceHandler()),
		)
	}

	return &ProxyEngine{
		proxyConfig: &proxyConfig,
		database:    db,
		restHandler: restHandler,
		grpcHandler: grpcHandler,
		session:     session,
		client:      client,
		grpcServer:  grpcServer,
		webServer:   webServer,
	}, nil
}

func (p *ProxyEngine) Start() error {
	address := "0.0.0.0:8080" // This method shouldn't be used in multi-proxy mode

	if p.proxyConfig.Protocol == "grpc" {
		return p.startGRPCServer(address)
	} else {
		return p.startHTTPServer(address)
	}
}

func (p *ProxyEngine) startHTTPServer(address string) error {
	mux := http.NewServeMux()

	// Register web UI routes if webServer is available
	if webServer, ok := p.webServer.(interface{ RegisterRoutes(*http.ServeMux) }); ok {
		webServer.RegisterRoutes(mux)
	}

	// All other requests go to proxy handler
	mux.HandleFunc("/", p.handleRequest)

	log.Printf("Starting HTTP proxy server on %s", address)
	log.Printf("Proxying to %s://%s:%d", p.proxyConfig.Protocol, p.proxyConfig.TargetHost, p.proxyConfig.TargetPort)

	return http.ListenAndServe(address, mux)
}

func (p *ProxyEngine) startGRPCServer(address string) error {
	if p.grpcServer == nil {
		return fmt.Errorf("gRPC server not initialized")
	}

	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}

	log.Printf("Starting gRPC proxy server on %s", address)
	log.Printf("Proxying to %s://%s:%d", p.proxyConfig.Protocol, p.proxyConfig.TargetHost, p.proxyConfig.TargetPort)

	return p.grpcServer.Serve(lis)
}

// HandleRequest implements the ProxyHandler interface
func (p *ProxyEngine) HandleRequest(w http.ResponseWriter, r *http.Request) {
	p.handleRequest(w, r)
}

func (p *ProxyEngine) handleRequest(w http.ResponseWriter, r *http.Request) {
	log.Printf("[%s] %s %s", r.Method, r.URL.Path, r.RemoteAddr)

	interaction, err := p.restHandler.ExtractRequest(r)
	if err != nil {
		log.Printf("Error extracting request: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	interaction.SessionID = p.session.ID

	// Broadcast request event if web server is available
	if p.webServer != nil {
		var requestHeaders map[string]interface{}
		json.Unmarshal([]byte(interaction.RequestHeaders), &requestHeaders)
		body := string(interaction.RequestBody)
		p.webServer.BroadcastRequest(interaction.Method, interaction.Endpoint, p.session.SessionName, r.RemoteAddr, interaction.RequestID, requestHeaders, body)
	}

	// Build target URL using the (possibly modified) URL path and query string
	targetPath := r.URL.Path
	if r.URL.RawQuery != "" {
		targetPath += "?" + r.URL.RawQuery
	}

	targetURL := fmt.Sprintf("%s://%s:%d%s",
		p.proxyConfig.Protocol,
		p.proxyConfig.TargetHost,
		p.proxyConfig.TargetPort,
		targetPath)

	proxyReq, err := p.restHandler.CopyRequest(r, targetURL)
	if err != nil {
		log.Printf("Error copying request: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	resp, err := p.client.Do(proxyReq)
	if err != nil {
		log.Printf("Error forwarding request: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Check if streaming is enabled for this proxy and response is SSE
	if p.proxyConfig.EnableStreaming && p.restHandler.IsStreamingResponse(resp) {
		log.Printf("Streaming enabled - handling SSE response for %s %s", interaction.Method, interaction.Endpoint)
		p.handleStreamingResponse(w, r, resp, interaction)
		return
	}

	status, headers, body, err := p.restHandler.ExtractResponse(resp)
	if err != nil {
		log.Printf("Error extracting response: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	interaction.ResponseStatus = status
	interaction.ResponseHeaders = headers
	interaction.ResponseBody = body

	// Broadcast response event if web server is available
	if p.webServer != nil {
		var responseHeaders map[string]interface{}
		json.Unmarshal([]byte(interaction.ResponseHeaders), &responseHeaders)
		responseBody := string(interaction.ResponseBody)
		p.webServer.BroadcastResponse(interaction.Method, interaction.Endpoint, p.session.SessionName, r.RemoteAddr, interaction.RequestID, status, responseHeaders, responseBody)
	}

	if err := p.database.RecordInteraction(interaction); err != nil {
		log.Printf("Error recording interaction: %v", err)
	} else {
		log.Printf("Recorded interaction: %s %s -> %d", interaction.Method, interaction.Endpoint, interaction.ResponseStatus)
	}

	if err := p.restHandler.CopyResponse(resp, w); err != nil {
		log.Printf("Error copying response: %v", err)
	}
}

func (p *ProxyEngine) handleStreamingResponse(w http.ResponseWriter, r *http.Request, resp *http.Response, interaction *storage.Interaction) {
	// Extract response headers
	headers := make(map[string]string)
	for key, values := range resp.Header {
		headers[key] = strings.Join(values, ", ")
	}

	headersJSON, err := json.Marshal(headers)
	if err != nil {
		log.Printf("Error marshaling response headers: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	interaction.ResponseStatus = resp.StatusCode
	interaction.ResponseHeaders = string(headersJSON)
	interaction.IsStreaming = true

	// Record the interaction first (without response body for streaming)
	if err := p.database.RecordInteraction(interaction); err != nil {
		log.Printf("Error recording streaming interaction: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	log.Printf("Recorded streaming interaction: %s %s (ID: %d)", interaction.Method, interaction.Endpoint, interaction.ID)

	// Copy and capture the streaming response
	chunks, err := p.restHandler.CopyStreamingResponse(resp, w)
	if err != nil {
		// Check if it's a broken pipe (client disconnected)
		if strings.Contains(err.Error(), "broken pipe") || strings.Contains(err.Error(), "connection reset") {
			log.Printf("Client disconnected during streaming response (captured %d chunks before disconnect)", len(chunks))
		} else {
			log.Printf("Error copying streaming response: %v", err)
		}
		// Continue to save whatever chunks we captured before the error
	}

	log.Printf("Captured %d streaming chunks for %s %s", len(chunks), interaction.Method, interaction.Endpoint)

	// Store all chunks atomically in a single transaction
	streamChunks := make([]*storage.StreamChunk, len(chunks))
	for i, chunk := range chunks {
		streamChunks[i] = &storage.StreamChunk{
			InteractionID: interaction.ID,
			ChunkIndex:    i,
			Data:          chunk.RawData,
			Timestamp:     chunk.Timestamp,
			TimeDelta:     chunk.TimeDelta,
		}
	}

	// Use transactional batch insertion to ensure atomicity
	if err := p.database.RecordStreamChunks(streamChunks); err != nil {
		log.Printf("Error recording stream chunks atomically: %v", err)
		// Mark interaction as failed since no chunks were persisted
		if err := p.database.MarkInteractionAsPartial(interaction.ID, []int{}); err != nil {
			log.Printf("Error marking interaction as partial: %v", err)
		}
	}

	// Broadcast streaming completion if web server is available
	if p.webServer != nil {
		var responseHeaders map[string]interface{}
		json.Unmarshal([]byte(interaction.ResponseHeaders), &responseHeaders)
		responseBody := fmt.Sprintf("[Streaming response with %d chunks]", len(chunks))
		p.webServer.BroadcastResponse(interaction.Method, interaction.Endpoint, p.session.SessionName, r.RemoteAddr, interaction.RequestID, resp.StatusCode, responseHeaders, responseBody)
	}
}

func (p *ProxyEngine) Stop() error {
	if p.grpcServer != nil {
		p.grpcServer.GracefulStop()
	}
	return nil
}

func (p *ProxyEngine) GetGRPCServer() *grpc.Server {
	return p.grpcServer
}
