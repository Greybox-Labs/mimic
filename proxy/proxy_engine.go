package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"mimic/config"
	"mimic/storage"
)

type ProxyEngine struct {
	proxyConfig *config.ProxyConfig
	database    *storage.Database
	restHandler *RESTHandler
	session     *storage.Session
	client      *http.Client
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

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
			MaxIdleConnsPerHost: 10,
		},
	}

	return &ProxyEngine{
		proxyConfig: &proxyConfig,
		database:    db,
		restHandler: restHandler,
		session:     session,
		client:      client,
		webServer:   webServer,
	}, nil
}



func (p *ProxyEngine) Start() error {
	mux := http.NewServeMux()
	
	// Register web UI routes if webServer is available
	if webServer, ok := p.webServer.(interface{ RegisterRoutes(*http.ServeMux) }); ok {
		webServer.RegisterRoutes(mux)
	}
	
	// All other requests go to proxy handler
	mux.HandleFunc("/", p.handleRequest)

	address := "0.0.0.0:8080" // This method shouldn't be used in multi-proxy mode
	log.Printf("Starting proxy server in %s mode on %s", p.proxyConfig.Mode, address)
	log.Printf("Proxying to %s://%s:%d", p.proxyConfig.Protocol, p.proxyConfig.TargetHost, p.proxyConfig.TargetPort)

	return http.ListenAndServe(address, mux)
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

	targetURL := fmt.Sprintf("%s://%s:%d%s",
		p.proxyConfig.Protocol,
		p.proxyConfig.TargetHost,
		p.proxyConfig.TargetPort,
		r.URL.RequestURI())

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

func (p *ProxyEngine) Stop() error {
	return nil
}
