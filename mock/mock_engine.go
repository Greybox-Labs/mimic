package mock

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"mimic/config"
	"mimic/proxy"
	"mimic/storage"
)

type MockEngine struct {
	config         *config.Config
	database       *storage.Database
	restHandler    *proxy.RESTHandler
	session        *storage.Session
	sequenceState  map[string]int
	sequenceMutex  sync.RWMutex
}

func NewMockEngine(cfg *config.Config, db *storage.Database) (*MockEngine, error) {
	session, err := db.GetOrCreateSession(cfg.Recording.SessionName, "Mock session")
	if err != nil {
		return nil, fmt.Errorf("failed to get or create session: %w", err)
	}

	restHandler := proxy.NewRESTHandler(cfg.Recording.RedactPatterns)

	return &MockEngine{
		config:        cfg,
		database:      db,
		restHandler:   restHandler,
		session:       session,
		sequenceState: make(map[string]int),
	}, nil
}

func (m *MockEngine) Start() error {
	http.HandleFunc("/", m.handleRequest)
	
	address := fmt.Sprintf("%s:%d", m.config.Proxy.ListenHost, m.config.Proxy.ListenPort)
	log.Printf("Starting mock server in %s mode on %s", m.config.Proxy.Mode, address)
	log.Printf("Serving mocked responses for session: %s", m.session.SessionName)
	
	return http.ListenAndServe(address, nil)
}

func (m *MockEngine) handleRequest(w http.ResponseWriter, r *http.Request) {
	log.Printf("[MOCK] %s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
	
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

	var selectedInteraction *storage.Interaction
	
	switch m.config.Mock.SequenceMode {
	case "ordered":
		selectedInteraction = m.selectOrderedInteraction(interactions, r)
	case "random":
		selectedInteraction = m.selectRandomInteraction(interactions, r)
	default:
		selectedInteraction = m.selectOrderedInteraction(interactions, r)
	}

	if selectedInteraction == nil {
		log.Printf("No suitable interaction found for %s %s", r.Method, r.URL.Path)
		m.sendNotFoundResponse(w)
		return
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

func (m *MockEngine) selectOrderedInteraction(interactions []storage.Interaction, r *http.Request) *storage.Interaction {
	key := fmt.Sprintf("%s:%s", r.Method, r.URL.Path)
	
	m.sequenceMutex.Lock()
	defer m.sequenceMutex.Unlock()
	
	currentSequence := m.sequenceState[key]
	
	for _, interaction := range interactions {
		if interaction.SequenceNumber > currentSequence {
			m.sequenceState[key] = interaction.SequenceNumber
			return &interaction
		}
	}
	
	if len(interactions) > 0 {
		m.sequenceState[key] = interactions[0].SequenceNumber
		return &interactions[0]
	}
	
	return nil
}

func (m *MockEngine) selectRandomInteraction(interactions []storage.Interaction, r *http.Request) *storage.Interaction {
	if len(interactions) == 0 {
		return nil
	}
	
	for _, interaction := range interactions {
		if m.restHandler.MatchRequest(r, &interaction, m.config.Mock.MatchingStrategy) {
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
	w.WriteHeader(m.config.Mock.NotFoundResponse.Status)
	
	if err := json.NewEncoder(w).Encode(m.config.Mock.NotFoundResponse.Body); err != nil {
		log.Printf("Error encoding not found response: %v", err)
	}
}

func (m *MockEngine) Stop() error {
	return nil
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