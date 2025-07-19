package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"mimic/config"
	"mimic/proxy"
	"mimic/storage"
)

// ReplayResult represents the result of replaying a single interaction
type ReplayResult struct {
	Interaction     *storage.Interaction `json:"interaction"`
	Success         bool                 `json:"success"`
	ExpectedStatus  int                  `json:"expected_status"`
	ActualStatus    int                  `json:"actual_status"`
	ExpectedBody    []byte               `json:"expected_body"`
	ActualBody      []byte               `json:"actual_body"`
	ResponseTime    time.Duration        `json:"response_time"`
	Error           error                `json:"error,omitempty"`
	ValidationError string               `json:"validation_error,omitempty"`
}

// ReplaySession represents the overall replay session results
type ReplaySession struct {
	SessionName   string          `json:"session_name"`
	TotalRequests int             `json:"total_requests"`
	SuccessCount  int             `json:"success_count"`
	FailureCount  int             `json:"failure_count"`
	Results       []*ReplayResult `json:"results"`
	StartTime     time.Time       `json:"start_time"`
	EndTime       time.Time       `json:"end_time"`
	Duration      time.Duration   `json:"duration"`
}

// ReplayEngine handles replaying recorded interactions against a target server
type ReplayEngine struct {
	config   *config.ReplayConfig
	database *storage.Database
	session  *storage.Session
	client   *http.Client
	grpcConn *grpc.ClientConn
	results  []*ReplayResult
	mutex    sync.RWMutex
}

// NewReplayEngine creates a new replay engine
func NewReplayEngine(replayConfig *config.ReplayConfig, db *storage.Database) (*ReplayEngine, error) {
	session, err := db.GetSession(replayConfig.SessionName)
	if err != nil {
		return nil, fmt.Errorf("failed to get session '%s': %w", replayConfig.SessionName, err)
	}

	httpClient := &http.Client{
		Timeout: time.Duration(replayConfig.TimeoutSeconds) * time.Second,
	}

	var grpcConn *grpc.ClientConn
	// Check if we have any gRPC interactions in the session or if protocol is gRPC
	hasGRPCInteractions := replayConfig.Protocol == "grpc"
	if !hasGRPCInteractions {
		// Quick check to see if we have gRPC interactions
		interactions, err := db.GetInteractionsBySession(session.ID)
		if err == nil {
			for _, interaction := range interactions {
				if interaction.Protocol == "gRPC" {
					hasGRPCInteractions = true
					break
				}
			}
		}
	}

	if hasGRPCInteractions {
		// Register raw codec for gRPC
		proxy.RegisterRawCodec()

		var creds credentials.TransportCredentials
		if replayConfig.GRPCInsecure {
			creds = insecure.NewCredentials()
		} else {
			// Use TLS credentials for secure connections
			creds = credentials.NewTLS(nil)
		}

		target := fmt.Sprintf("%s:%d", replayConfig.TargetHost, replayConfig.TargetPort)

		// Create dial options with larger limits to handle big gRPC messages
		maxSize := replayConfig.GRPCMaxMessageSize
		if maxSize < 64*1024*1024 {
			maxSize = 64 * 1024 * 1024 // Minimum 64MB
		}

		dialOpts := []grpc.DialOption{
			grpc.WithTransportCredentials(creds),
			grpc.WithDefaultCallOptions(
				grpc.MaxCallRecvMsgSize(maxSize),
				grpc.MaxCallSendMsgSize(maxSize),
			),
			// Set very large HTTP/2 window sizes to handle large frames
			grpc.WithInitialWindowSize(int32(maxSize)),
			grpc.WithInitialConnWindowSize(int32(maxSize)),
			// Add buffer sizes for large messages
			grpc.WithReadBufferSize(maxSize),
			grpc.WithWriteBufferSize(maxSize),
		}

		conn, err := grpc.Dial(target, dialOpts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create gRPC connection: %w", err)
		}
		grpcConn = conn
	}

	return &ReplayEngine{
		config:   replayConfig,
		database: db,
		session:  session,
		client:   httpClient,
		grpcConn: grpcConn,
		results:  make([]*ReplayResult, 0),
	}, nil
}

// Replay replays all interactions from the session against the target server
func (r *ReplayEngine) Replay() (*ReplaySession, error) {
	log.Printf("Starting replay of session '%s' against %s://%s:%d",
		r.config.SessionName, r.config.Protocol, r.config.TargetHost, r.config.TargetPort)

	interactions, err := r.database.GetInteractionsBySession(r.session.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get interactions: %w", err)
	}

	if len(interactions) == 0 {
		return nil, fmt.Errorf("no interactions found in session '%s'", r.config.SessionName)
	}

	// Sort interactions by timestamp to maintain original order
	sort.Slice(interactions, func(i, j int) bool {
		return interactions[i].Timestamp.Before(interactions[j].Timestamp)
	})

	replaySession := &ReplaySession{
		SessionName:   r.config.SessionName,
		TotalRequests: len(interactions),
		Results:       make([]*ReplayResult, 0),
		StartTime:     time.Now(),
	}

	if r.config.MaxConcurrency > 0 {
		err = r.replayConcurrent(interactions, replaySession)
	} else {
		err = r.replaySequential(interactions, replaySession)
	}

	replaySession.EndTime = time.Now()
	replaySession.Duration = replaySession.EndTime.Sub(replaySession.StartTime)
	replaySession.Results = r.results
	replaySession.SuccessCount = r.countSuccesses()
	replaySession.FailureCount = replaySession.TotalRequests - replaySession.SuccessCount

	// Close any open connections
	if r.grpcConn != nil {
		if closeErr := r.grpcConn.Close(); closeErr != nil {
			log.Printf("Warning: failed to close gRPC connection: %v", closeErr)
		}
	}

	if err != nil {
		return replaySession, err
	}

	log.Printf("Replay completed: %d/%d successful, %d failed",
		replaySession.SuccessCount, replaySession.TotalRequests, replaySession.FailureCount)

	return replaySession, nil
}

// replaySequential replays interactions one by one, respecting original timing
func (r *ReplayEngine) replaySequential(interactions []storage.Interaction, replaySession *ReplaySession) error {
	var baseTime *time.Time

	for i, interaction := range interactions {
		// Calculate delay based on original timestamps
		if !r.config.IgnoreTimestamps && baseTime != nil {
			delay := interaction.Timestamp.Sub(*baseTime)
			if delay > 0 {
				log.Printf("Waiting %v before next request (original timing)", delay)
				time.Sleep(delay)
			}
		} else if baseTime == nil {
			baseTime = &interaction.Timestamp
		}

		result := r.replayInteraction(&interaction)
		r.addResult(result)

		if !result.Success && r.config.FailFast {
			return fmt.Errorf("replay failed at interaction %d: %s", i+1, result.ValidationError)
		}

		// Update base time for next iteration
		baseTime = &interaction.Timestamp
	}

	return nil
}

// replayConcurrent replays interactions concurrently with a semaphore for max concurrency
func (r *ReplayEngine) replayConcurrent(interactions []storage.Interaction, replaySession *ReplaySession) error {
	semaphore := make(chan struct{}, r.config.MaxConcurrency)
	var wg sync.WaitGroup
	var firstError error
	var errorMutex sync.Mutex

	for i, interaction := range interactions {
		wg.Add(1)
		go func(idx int, inter storage.Interaction) {
			defer wg.Done()

			semaphore <- struct{}{}        // Acquire semaphore
			defer func() { <-semaphore }() // Release semaphore

			result := r.replayInteraction(&inter)
			r.addResult(result)

			if !result.Success && r.config.FailFast {
				errorMutex.Lock()
				if firstError == nil {
					firstError = fmt.Errorf("replay failed at interaction %d: %s", idx+1, result.ValidationError)
				}
				errorMutex.Unlock()
			}
		}(i, interaction)
	}

	wg.Wait()

	return firstError
}

// replayInteraction replays a single interaction and validates the response
func (r *ReplayEngine) replayInteraction(interaction *storage.Interaction) *ReplayResult {
	result := &ReplayResult{
		Interaction:    interaction,
		ExpectedStatus: interaction.ResponseStatus,
		ExpectedBody:   interaction.ResponseBody,
	}

	startTime := time.Now()

	// Handle gRPC interactions differently from HTTP
	if interaction.Protocol == "gRPC" {
		return r.replayGRPCInteraction(interaction, result, startTime)
	} else {
		return r.replayHTTPInteraction(interaction, result, startTime)
	}
}

// replayHTTPInteraction handles HTTP/HTTPS replay
func (r *ReplayEngine) replayHTTPInteraction(interaction *storage.Interaction, result *ReplayResult, startTime time.Time) *ReplayResult {
	// Construct the request URL
	url := fmt.Sprintf("%s://%s:%d%s", r.config.Protocol, r.config.TargetHost, r.config.TargetPort, interaction.Endpoint)

	// Create the HTTP request
	req, err := http.NewRequest(interaction.Method, url, bytes.NewBuffer(interaction.RequestBody))
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		return result
	}

	// Add headers from the recorded interaction
	if interaction.RequestHeaders != "" {
		headers := make(map[string]string)
		if err := json.Unmarshal([]byte(interaction.RequestHeaders), &headers); err == nil {
			for key, value := range headers {
				req.Header.Set(key, value)
			}
		}
	}

	// Execute the request
	resp, err := r.client.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("request failed: %w", err)
		result.ResponseTime = time.Since(startTime)
		return result
	}
	defer resp.Body.Close()

	result.ResponseTime = time.Since(startTime)
	result.ActualStatus = resp.StatusCode

	// Read response body
	var actualBody bytes.Buffer
	if _, err := actualBody.ReadFrom(resp.Body); err != nil {
		result.Error = fmt.Errorf("failed to read response body: %w", err)
		return result
	}
	result.ActualBody = actualBody.Bytes()

	// Validate the response based on matching strategy
	result.Success, result.ValidationError = r.validateResponse(result)

	return result
}

// replayGRPCInteraction handles gRPC replay
func (r *ReplayEngine) replayGRPCInteraction(interaction *storage.Interaction, result *ReplayResult, startTime time.Time) *ReplayResult {
	if r.grpcConn == nil {
		result.Error = fmt.Errorf("gRPC connection not available")
		result.ResponseTime = time.Since(startTime)
		return result
	}

	// Parse metadata from the recorded interaction
	var metadataMap map[string][]string
	ctx := context.Background()
	if interaction.RequestHeaders != "" {
		if err := json.Unmarshal([]byte(interaction.RequestHeaders), &metadataMap); err == nil {
			md := metadata.New(nil)
			for key, values := range metadataMap {
				md.Set(key, values...)
			}
			ctx = metadata.NewOutgoingContext(ctx, md)
		}
	}

	// Add timeout to context
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.config.TimeoutSeconds)*time.Second)
	defer cancel()

	// Create request message with recorded raw data
	requestMsg := &proxy.RawMessage{
		Data: interaction.RequestBody,
	}

	// Create response message
	responseMsg := &proxy.RawMessage{}

	// Invoke the gRPC method
	err := r.grpcConn.Invoke(ctx, interaction.Method, requestMsg, responseMsg, grpc.ForceCodec(proxy.GetRawCodec()))

	result.ResponseTime = time.Since(startTime)

	if err != nil {
		// Handle gRPC errors
		if st, ok := status.FromError(err); ok {
			result.ActualStatus = int(st.Code())
		} else {
			result.ActualStatus = int(codes.Unknown)
		}

		// For gRPC, some errors might be expected (like validation errors)
		// So we don't always treat errors as failures
		if result.ExpectedStatus != 0 && result.ActualStatus == result.ExpectedStatus {
			// If we expected this error status, it's not a failure
			result.Success, result.ValidationError = r.validateResponse(result)
		} else {
			result.Error = fmt.Errorf("gRPC call failed: %w", err)
		}
		return result
	}

	// Success response
	result.ActualStatus = int(codes.OK)
	result.ActualBody = responseMsg.Data

	// Validate the response based on matching strategy
	result.Success, result.ValidationError = r.validateResponse(result)

	return result
}

// validateResponse validates the actual response against the expected response
func (r *ReplayEngine) validateResponse(result *ReplayResult) (bool, string) {
	switch r.config.MatchingStrategy {
	case "exact":
		return r.exactMatch(result)
	case "fuzzy":
		return r.fuzzyMatch(result)
	case "status_code":
		return r.statusCodeMatch(result)
	default:
		return r.exactMatch(result)
	}
}

// exactMatch validates that the response matches exactly
func (r *ReplayEngine) exactMatch(result *ReplayResult) (bool, string) {
	if result.ActualStatus != result.ExpectedStatus {
		return false, fmt.Sprintf("status mismatch: expected %d, got %d", result.ExpectedStatus, result.ActualStatus)
	}

	if !bytes.Equal(result.ActualBody, result.ExpectedBody) {
		return false, fmt.Sprintf("body mismatch: expected %d bytes, got %d bytes", len(result.ExpectedBody), len(result.ActualBody))
	}

	return true, ""
}

// fuzzyMatch validates with some tolerance for differences
func (r *ReplayEngine) fuzzyMatch(result *ReplayResult) (bool, string) {
	// For fuzzy matching, we only check status code and basic structure
	if result.ActualStatus != result.ExpectedStatus {
		return false, fmt.Sprintf("status mismatch: expected %d, got %d", result.ExpectedStatus, result.ActualStatus)
	}

	// For JSON responses, compare structure rather than exact content
	if r.isJSON(result.ExpectedBody) && r.isJSON(result.ActualBody) {
		var expectedJSON, actualJSON interface{}
		if json.Unmarshal(result.ExpectedBody, &expectedJSON) == nil &&
			json.Unmarshal(result.ActualBody, &actualJSON) == nil {
			// For fuzzy matching, we just verify both are valid JSON with similar structure
			expectedType := fmt.Sprintf("%T", expectedJSON)
			actualType := fmt.Sprintf("%T", actualJSON)
			if expectedType != actualType {
				return false, fmt.Sprintf("JSON structure mismatch: expected %s, got %s", expectedType, actualType)
			}
		}
	}

	return true, ""
}

// statusCodeMatch only validates the status code
func (r *ReplayEngine) statusCodeMatch(result *ReplayResult) (bool, string) {
	if result.ActualStatus != result.ExpectedStatus {
		return false, fmt.Sprintf("status mismatch: expected %d, got %d", result.ExpectedStatus, result.ActualStatus)
	}
	return true, ""
}

// isJSON checks if the given bytes represent valid JSON
func (r *ReplayEngine) isJSON(data []byte) bool {
	var js interface{}
	return json.Unmarshal(data, &js) == nil
}

// addResult adds a result to the engine's results slice (thread-safe)
func (r *ReplayEngine) addResult(result *ReplayResult) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.results = append(r.results, result)
}

// countSuccesses counts the number of successful replays
func (r *ReplayEngine) countSuccesses() int {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	count := 0
	for _, result := range r.results {
		if result.Success {
			count++
		}
	}
	return count
}

// GetResults returns a copy of all replay results
func (r *ReplayEngine) GetResults() []*ReplayResult {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	results := make([]*ReplayResult, len(r.results))
	copy(results, r.results)
	return results
}
