package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"mimic/config"
	"mimic/replay"
	"mimic/storage"
	"mimic/web"
)

// ReplayHandler handles HTTP requests for replay functionality
type ReplayHandler struct {
	config    *config.ReplayConfig
	database  *storage.Database
	webServer *web.Server
}

// NewReplayHandler creates a new replay handler
func NewReplayHandler(replayConfig *config.ReplayConfig, db *storage.Database, webServer *web.Server) (*ReplayHandler, error) {
	return &ReplayHandler{
		config:    replayConfig,
		database:  db,
		webServer: webServer,
	}, nil
}

// HandleRequest implements the ProxyHandler interface for replay functionality
func (h *ReplayHandler) HandleRequest(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		h.handleStatus(w, r)
	case "POST":
		h.handleReplay(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleStatus returns replay configuration and available sessions
func (h *ReplayHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.database.ListSessions()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list sessions: %v", err), http.StatusInternalServerError)
		return
	}

	status := map[string]interface{}{
		"mode":               "replay",
		"target_host":        h.config.TargetHost,
		"target_port":        h.config.TargetPort,
		"protocol":           h.config.Protocol,
		"session_name":       h.config.SessionName,
		"matching_strategy":  h.config.MatchingStrategy,
		"fail_fast":          h.config.FailFast,
		"timeout_seconds":    h.config.TimeoutSeconds,
		"max_concurrency":    h.config.MaxConcurrency,
		"ignore_timestamps":  h.config.IgnoreTimestamps,
		"available_sessions": sessions,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleReplay executes a replay based on request parameters
func (h *ReplayHandler) handleReplay(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters to override config
	replayConfig := *h.config // Copy the config

	if sessionName := r.URL.Query().Get("session"); sessionName != "" {
		replayConfig.SessionName = sessionName
	}

	if targetHost := r.URL.Query().Get("target_host"); targetHost != "" {
		replayConfig.TargetHost = targetHost
	}

	if targetPortStr := r.URL.Query().Get("target_port"); targetPortStr != "" {
		if targetPort, err := strconv.Atoi(targetPortStr); err == nil {
			replayConfig.TargetPort = targetPort
		}
	}

	if protocol := r.URL.Query().Get("protocol"); protocol != "" {
		replayConfig.Protocol = protocol
	}

	if matchingStrategy := r.URL.Query().Get("matching_strategy"); matchingStrategy != "" {
		replayConfig.MatchingStrategy = matchingStrategy
	}

	if failFastStr := r.URL.Query().Get("fail_fast"); failFastStr != "" {
		if failFast, err := strconv.ParseBool(failFastStr); err == nil {
			replayConfig.FailFast = failFast
		}
	}

	if ignoreTimestampsStr := r.URL.Query().Get("ignore_timestamps"); ignoreTimestampsStr != "" {
		if ignoreTimestamps, err := strconv.ParseBool(ignoreTimestampsStr); err == nil {
			replayConfig.IgnoreTimestamps = ignoreTimestamps
		}
	}

	// Create replay engine and execute replay
	engine, err := replay.NewReplayEngine(&replayConfig, h.database)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create replay engine: %v", err), http.StatusBadRequest)
		return
	}

	log.Printf("Starting replay of session '%s' against %s://%s:%d",
		replayConfig.SessionName, replayConfig.Protocol, replayConfig.TargetHost, replayConfig.TargetPort)

	// Execute the replay
	replaySession, err := engine.Replay()
	if err != nil {
		// Even if there were failures, we want to return the results
		log.Printf("Replay completed with errors: %v", err)
	}

	// Broadcast replay results to web clients if available
	if h.webServer != nil {
		h.webServer.BroadcastEvent("replay_completed", replaySession)
	}

	// Return the replay results
	w.Header().Set("Content-Type", "application/json")
	if replaySession.FailureCount > 0 {
		w.WriteHeader(http.StatusExpectationFailed) // 417 to indicate test failures
	}
	json.NewEncoder(w).Encode(replaySession)

	log.Printf("Replay completed: %d/%d successful, %d failed, duration: %v",
		replaySession.SuccessCount, replaySession.TotalRequests, replaySession.FailureCount, replaySession.Duration)
}
