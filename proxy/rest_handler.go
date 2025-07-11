package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"mimic/storage"
	"github.com/google/uuid"
)

type RESTHandler struct {
	redactPatterns []*regexp.Regexp
}

func NewRESTHandler(redactPatterns []string) *RESTHandler {
	patterns := make([]*regexp.Regexp, len(redactPatterns))
	for i, pattern := range redactPatterns {
		if compiled, err := regexp.Compile(pattern); err == nil {
			patterns[i] = compiled
		}
	}
	return &RESTHandler{redactPatterns: patterns}
}

func (h *RESTHandler) ExtractRequest(req *http.Request) (*storage.Interaction, error) {
	requestID := uuid.New().String()
	
	headers := make(map[string]string)
	for key, values := range req.Header {
		headers[key] = strings.Join(values, ", ")
	}
	
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal headers: %w", err)
	}
	
	headersStr := string(headersJSON)
	headersStr = h.redactSensitiveData(headersStr)
	
	var body []byte
	if req.Body != nil {
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		req.Body = io.NopCloser(bytes.NewBuffer(body))
	}
	
	return &storage.Interaction{
		RequestID:      requestID,
		Protocol:       "REST",
		Method:         req.Method,
		Endpoint:       req.URL.Path,
		RequestHeaders: headersStr,
		RequestBody:    body,
		Timestamp:      time.Now(),
	}, nil
}

func (h *RESTHandler) ExtractResponse(resp *http.Response) (int, string, []byte, error) {
	headers := make(map[string]string)
	for key, values := range resp.Header {
		headers[key] = strings.Join(values, ", ")
	}
	
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		return 0, "", nil, fmt.Errorf("failed to marshal response headers: %w", err)
	}
	
	headersStr := string(headersJSON)
	headersStr = h.redactSensitiveData(headersStr)
	
	var body []byte
	if resp.Body != nil {
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return 0, "", nil, fmt.Errorf("failed to read response body: %w", err)
		}
		resp.Body = io.NopCloser(bytes.NewBuffer(body))
	}
	
	return resp.StatusCode, headersStr, body, nil
}

func (h *RESTHandler) CreateResponse(interaction *storage.Interaction) *http.Response {
	body := io.NopCloser(bytes.NewBuffer(interaction.ResponseBody))
	
	var headers map[string]string
	if interaction.ResponseHeaders != "" {
		json.Unmarshal([]byte(interaction.ResponseHeaders), &headers)
	}
	
	resp := &http.Response{
		StatusCode: interaction.ResponseStatus,
		Header:     make(http.Header),
		Body:       body,
		Close:      true,
	}
	
	for key, value := range headers {
		resp.Header.Set(key, value)
	}
	
	return resp
}

func (h *RESTHandler) MatchRequest(req *http.Request, interaction *storage.Interaction, strategy string) bool {
	switch strategy {
	case "exact":
		return h.exactMatch(req, interaction)
	case "pattern":
		return h.patternMatch(req, interaction)
	case "fuzzy":
		return h.fuzzyMatch(req, interaction)
	default:
		return h.exactMatch(req, interaction)
	}
}

func (h *RESTHandler) exactMatch(req *http.Request, interaction *storage.Interaction) bool {
	if req.Method != interaction.Method {
		return false
	}
	
	if req.URL.Path != interaction.Endpoint {
		return false
	}
	
	return true
}

func (h *RESTHandler) patternMatch(req *http.Request, interaction *storage.Interaction) bool {
	if req.Method != interaction.Method {
		return false
	}
	
	pattern, err := regexp.Compile(interaction.Endpoint)
	if err != nil {
		return false
	}
	
	return pattern.MatchString(req.URL.Path)
}

func (h *RESTHandler) fuzzyMatch(req *http.Request, interaction *storage.Interaction) bool {
	if req.Method != interaction.Method {
		return false
	}
	
	reqPath := strings.TrimPrefix(req.URL.Path, "/")
	interactionPath := strings.TrimPrefix(interaction.Endpoint, "/")
	
	reqParts := strings.Split(reqPath, "/")
	interactionParts := strings.Split(interactionPath, "/")
	
	if len(reqParts) != len(interactionParts) {
		return false
	}
	
	for i := range reqParts {
		if reqParts[i] != interactionParts[i] {
			if !h.isNumericOrUUID(reqParts[i]) || !h.isNumericOrUUID(interactionParts[i]) {
				return false
			}
		}
	}
	
	return true
}

func (h *RESTHandler) isNumericOrUUID(s string) bool {
	if len(s) == 36 && strings.Count(s, "-") == 4 {
		return true
	}
	
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (h *RESTHandler) redactSensitiveData(data string) string {
	result := data
	for _, pattern := range h.redactPatterns {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}

func (h *RESTHandler) CopyRequest(req *http.Request, targetURL string) (*http.Request, error) {
	var body io.Reader
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		body = bytes.NewBuffer(bodyBytes)
	}
	
	newReq, err := http.NewRequest(req.Method, targetURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create new request: %w", err)
	}
	
	for key, values := range req.Header {
		for _, value := range values {
			newReq.Header.Add(key, value)
		}
	}
	
	return newReq, nil
}

func (h *RESTHandler) CopyResponse(resp *http.Response, writer http.ResponseWriter) error {
	for key, values := range resp.Header {
		for _, value := range values {
			writer.Header().Add(key, value)
		}
	}
	
	writer.WriteHeader(resp.StatusCode)
	
	if resp.Body != nil {
		_, err := io.Copy(writer, resp.Body)
		if err != nil {
			return fmt.Errorf("failed to copy response body: %w", err)
		}
	}
	
	return nil
}