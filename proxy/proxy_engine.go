package proxy

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"mimic/config"
	"mimic/storage"
)

type ProxyEngine struct {
	config      *config.Config
	database    *storage.Database
	restHandler *RESTHandler
	session     *storage.Session
	client      *http.Client
}

func NewProxyEngine(cfg *config.Config, db *storage.Database) (*ProxyEngine, error) {
	session, err := db.GetOrCreateSession(cfg.Recording.SessionName, "Proxy recording session")
	if err != nil {
		return nil, fmt.Errorf("failed to get or create session: %w", err)
	}

	restHandler := NewRESTHandler(cfg.Recording.RedactPatterns)
	
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
		config:      cfg,
		database:    db,
		restHandler: restHandler,
		session:     session,
		client:      client,
	}, nil
}

func (p *ProxyEngine) Start() error {
	http.HandleFunc("/", p.handleRequest)
	
	address := fmt.Sprintf("%s:%d", p.config.Proxy.ListenHost, p.config.Proxy.ListenPort)
	log.Printf("Starting proxy server in %s mode on %s", p.config.Proxy.Mode, address)
	log.Printf("Proxying to %s://%s:%d", p.config.Proxy.Protocol, p.config.Proxy.TargetHost, p.config.Proxy.TargetPort)
	
	return http.ListenAndServe(address, nil)
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
	
	targetURL := fmt.Sprintf("%s://%s:%d%s",
		p.config.Proxy.Protocol,
		p.config.Proxy.TargetHost,
		p.config.Proxy.TargetPort,
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