package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"mimic/config"
	"mimic/storage"
)

type Server struct {
	config      *config.Config
	database    *storage.Database
	upgrader    websocket.Upgrader
	clients     map[*websocket.Conn]bool
	clientsMux  sync.RWMutex
	broadcast   chan []byte
}

type Message struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

type RequestResponseEvent struct {
	Type         string                 `json:"type"` // "request" or "response"
	Method       string                 `json:"method"`
	Endpoint     string                 `json:"endpoint"`
	Headers      map[string]interface{} `json:"headers"`
	Body         string                 `json:"body"`
	Status       int                    `json:"status,omitempty"`
	SessionName  string                 `json:"session_name"`
	RemoteAddr   string                 `json:"remote_addr"`
	RequestID    string                 `json:"request_id"`
}

func NewServer(cfg *config.Config, db *storage.Database) *Server {
	return &Server{
		config:   cfg,
		database: db,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for development
			},
		},
		clients:   make(map[*websocket.Conn]bool),
		broadcast: make(chan []byte),
	}
}

func (s *Server) Start() error {
	// Start the broadcast handler
	go s.handleBroadcast()

	// Create a new HTTP multiplexer for the web server
	mux := http.NewServeMux()
	
	// Static files
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static/"))))
	
	// Main page
	mux.HandleFunc("/", s.handleHome)
	
	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)
	
	// API endpoints
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionDetail)
	mux.HandleFunc("/api/interactions/", s.handleInteractions)
	mux.HandleFunc("/api/clear", s.handleClear)
	
	address := fmt.Sprintf("%s:%d", s.config.Server.ListenHost, s.config.Server.ListenPort) // Use same port as server
	log.Printf("Starting web UI on http://%s", address)
	return http.ListenAndServe(address, mux)
}

// RegisterRoutes adds the web UI routes to an existing mux at top level
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Start the broadcast handler
	go s.handleBroadcast()
	
	// Static files at /static/
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static/"))))
	
	// Main page at /
	mux.HandleFunc("/", s.handleHome)
	
	// WebSocket endpoint at /ws
	mux.HandleFunc("/ws", s.handleWebSocket)
	
	// API endpoints at /api/
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionDetail)
	mux.HandleFunc("/api/interactions/", s.handleInteractions)
	mux.HandleFunc("/api/clear", s.handleClear)
	
	log.Printf("Web UI registered at top level")
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Mimic - API Record & Replay</title>
    <link rel="stylesheet" href="/static/style.css">
</head>
<body>
    <div class="container">
        <header>
            <h1>🎭 Mimic</h1>
            <p>API Record & Replay Tool</p>
            <div class="status">
                <span id="connection-status" class="status-disconnected">Disconnected</span>
            </div>
        </header>

        <div class="main-content">
            <div class="sidebar">
                <div class="section">
                    <h3>Sessions</h3>
                    <div id="sessions-list" class="sessions-list">
                        Loading...
                    </div>
                    <button id="refresh-sessions" class="btn">Refresh</button>
                    <button id="clear-all" class="btn btn-danger">Clear All</button>
                </div>
                
                <div class="section">
                    <h3>Controls</h3>
                    <button id="clear-events" class="btn">Clear Events</button>
                    <label>
                        <input type="checkbox" id="auto-scroll" checked> Auto-scroll
                    </label>
                </div>
            </div>

            <div class="content">
                <div class="tabs">
                    <button class="tab-btn active" data-tab="events">Live Events</button>
                    <button class="tab-btn" data-tab="interactions">Interactions</button>
                </div>

                <div id="events-tab" class="tab-content active">
                    <div class="events-header">
                        <h3>Real-time Requests & Responses</h3>
                        <div class="event-count">Events: <span id="event-count">0</span></div>
                    </div>
                    <div id="events-list" class="events-list">
                        <div class="no-events">No events yet. Start making requests to see them here.</div>
                    </div>
                </div>

                <div id="interactions-tab" class="tab-content">
                    <div class="interactions-header">
                        <h3>Stored Interactions</h3>
                        <select id="session-filter">
                            <option value="">All Sessions</option>
                        </select>
                    </div>
                    <div id="interactions-list" class="interactions-list">
                        Loading...
                    </div>
                </div>
            </div>
        </div>
    </div>

    <div id="interaction-modal" class="modal">
        <div class="modal-content">
            <span class="close">&times;</span>
            <div id="interaction-detail"></div>
        </div>
    </div>

    <script src="/static/app.js"></script>
</body>
</html>`
	
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	s.clientsMux.Lock()
	s.clients[conn] = true
	s.clientsMux.Unlock()

	log.Printf("WebSocket client connected from %s", r.RemoteAddr)

	defer func() {
		s.clientsMux.Lock()
		delete(s.clients, conn)
		s.clientsMux.Unlock()
		log.Printf("WebSocket client disconnected")
	}()

	// Keep connection alive
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (s *Server) handleBroadcast() {
	for {
		message := <-s.broadcast
		s.clientsMux.RLock()
		for client := range s.clients {
			err := client.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				log.Printf("WebSocket write error: %v", err)
				client.Close()
				delete(s.clients, client)
			}
		}
		s.clientsMux.RUnlock()
	}
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.database.GetAllSessions()
	if err != nil {
		http.Error(w, "Failed to get sessions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Path[len("/api/sessions/"):]
	id, err := strconv.Atoi(sessionID)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	interactions, err := s.database.GetInteractionsBySession(id)
	if err != nil {
		http.Error(w, "Failed to get interactions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(interactions)
}

func (s *Server) handleInteractions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.database.GetAllSessions()
	if err != nil {
		http.Error(w, "Failed to get sessions", http.StatusInternalServerError)
		return
	}

	var allInteractions []storage.Interaction
	for _, session := range sessions {
		interactions, err := s.database.GetInteractionsBySession(session.ID)
		if err != nil {
			continue
		}
		allInteractions = append(allInteractions, interactions...)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allInteractions)
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := s.database.ClearAllSessions()
	if err != nil {
		http.Error(w, "Failed to clear sessions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// BroadcastEvent sends an event to all connected WebSocket clients
func (s *Server) BroadcastEvent(eventType string, data interface{}) {
	message := Message{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("Failed to marshal broadcast message: %v", err)
		return
	}

	select {
	case s.broadcast <- messageBytes:
	default:
		// Channel is full, skip this message
	}
}

// BroadcastRequest broadcasts a request event
func (s *Server) BroadcastRequest(method, endpoint, sessionName, remoteAddr, requestID string, headers map[string]interface{}, body string) {
	event := RequestResponseEvent{
		Type:        "request",
		Method:      method,
		Endpoint:    endpoint,
		Headers:     headers,
		Body:        body,
		SessionName: sessionName,
		RemoteAddr:  remoteAddr,
		RequestID:   requestID,
	}
	s.BroadcastEvent("request", event)
}

// BroadcastResponse broadcasts a response event
func (s *Server) BroadcastResponse(method, endpoint, sessionName, remoteAddr, requestID string, status int, headers map[string]interface{}, body string) {
	event := RequestResponseEvent{
		Type:        "response",
		Method:      method,
		Endpoint:    endpoint,
		Headers:     headers,
		Body:        body,
		Status:      status,
		SessionName: sessionName,
		RemoteAddr:  remoteAddr,
		RequestID:   requestID,
	}
	s.BroadcastEvent("response", event)
}