package mock

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"mimic/config"
	"mimic/proxy"
)

// Helper function to create a mock HTTP request with a body
func createMockRequest(body []byte) *http.Request {
	req, _ := http.NewRequest("POST", "/test", io.NopCloser(bytes.NewBuffer(body)))
	return req
}

func TestFuzzyMatchBody_FunctionResponses(t *testing.T) {
	mockConfig := &config.MockConfig{
		MatchingStrategy: "fuzzy",
	}

	mockEngine := &MockEngine{
		mockConfig: mockConfig,
	}

	// Simulate two requests with the same structure but different function response content
	recorded := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"text": "What is the weather?",
					},
				},
			},
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "web_search",
							"response": map[string]interface{}{
								"result": "It's sunny and 75°F with timestamps 2025-10-28T10:00:00",
							},
						},
					},
				},
			},
		},
		"systemInstruction": map[string]interface{}{
			"parts": []interface{}{
				map[string]interface{}{
					"text": "You are a helpful assistant",
				},
			},
		},
	}

	current := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"text": "What is the weather?",
					},
				},
			},
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "web_search",
							"response": map[string]interface{}{
								"result": "It's cloudy and 65°F with different timestamps 2025-10-28T11:00:00",
							},
						},
					},
				},
			},
		},
		"systemInstruction": map[string]interface{}{
			"parts": []interface{}{
				map[string]interface{}{
					"text": "You are a helpful assistant",
				},
			},
		},
	}

	recordedJSON, _ := json.Marshal(recorded)
	currentJSON, _ := json.Marshal(current)

	// Test fuzzy matching - should match despite different function response content
	if !mockEngine.fuzzyMatchBody(recordedJSON, currentJSON) {
		t.Error("Expected fuzzy match to succeed for requests with same structure but different function response content")
	}

	// Test with different function name - should NOT match
	currentDifferentFunc := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"text": "What is the weather?",
					},
				},
			},
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "different_function", // Different function name
							"response": map[string]interface{}{
								"result": "Some result",
							},
						},
					},
				},
			},
		},
		"systemInstruction": map[string]interface{}{
			"parts": []interface{}{
				map[string]interface{}{
					"text": "You are a helpful assistant",
				},
			},
		},
	}

	currentDifferentJSON, _ := json.Marshal(currentDifferentFunc)

	if mockEngine.fuzzyMatchBody(recordedJSON, currentDifferentJSON) {
		t.Error("Expected fuzzy match to fail for requests with different function names")
	}
}

func TestFuzzyMatchBody_MultipleFunctionResponses(t *testing.T) {
	mockConfig := &config.MockConfig{
		MatchingStrategy: "fuzzy",
	}

	mockEngine := &MockEngine{
		mockConfig: mockConfig,
	}

	// Test with multiple function responses (like parallel tool calls)
	recorded := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "web_search",
							"response": map[string]interface{}{
								"result": "San Francisco weather: 59°F",
							},
						},
					},
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "web_search",
							"response": map[string]interface{}{
								"result": "New York weather: 54°F",
							},
						},
					},
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "web_search",
							"response": map[string]interface{}{
								"result": "London weather: 14°C",
							},
						},
					},
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "web_search",
							"response": map[string]interface{}{
								"result": "Tokyo weather: 56°F",
							},
						},
					},
				},
			},
		},
	}

	current := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "web_search",
							"response": map[string]interface{}{
								"result": "San Francisco weather: 61°F (different!)",
							},
						},
					},
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "web_search",
							"response": map[string]interface{}{
								"result": "New York weather: 52°F (different!)",
							},
						},
					},
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "web_search",
							"response": map[string]interface{}{
								"result": "London weather: 15°C (different!)",
							},
						},
					},
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "web_search",
							"response": map[string]interface{}{
								"result": "Tokyo weather: 58°F (different!)",
							},
						},
					},
				},
			},
		},
	}

	recordedJSON, _ := json.Marshal(recorded)
	currentJSON, _ := json.Marshal(current)

	// Should match - same number and names of function responses
	if !mockEngine.fuzzyMatchBody(recordedJSON, currentJSON) {
		t.Error("Expected fuzzy match to succeed for requests with multiple function responses with same names")
	}
}

func TestExactMatchBody(t *testing.T) {
	mockConfig := &config.MockConfig{
		MatchingStrategy: "exact",
	}

	mockEngine := &MockEngine{
		mockConfig: mockConfig,
	}

	body1 := []byte(`{"test": "value"}`)
	body2 := []byte(`{"test": "value"}`)
	body3 := []byte(`{"test": "different"}`)

	// Exact match should work for identical bodies
	if !mockEngine.matchesBody(body1, createMockRequest(body2)) {
		t.Error("Expected exact match to succeed for identical bodies")
	}

	// Exact match should fail for different bodies
	if mockEngine.matchesBody(body1, createMockRequest(body3)) {
		t.Error("Expected exact match to fail for different bodies")
	}
}

func TestFuzzyMatchBody_NonJSON(t *testing.T) {
	mockConfig := &config.MockConfig{
		MatchingStrategy: "fuzzy",
	}

	mockEngine := &MockEngine{
		mockConfig: mockConfig,
	}

	// For non-JSON content, fuzzy should fall back to exact matching
	body1 := []byte("plain text content")
	body2 := []byte("plain text content")
	body3 := []byte("different text content")

	if !mockEngine.fuzzyMatchBody(body1, body2) {
		t.Error("Expected fuzzy match to succeed for identical non-JSON bodies")
	}

	if mockEngine.fuzzyMatchBody(body1, body3) {
		t.Error("Expected fuzzy match to fail for different non-JSON bodies")
	}
}

func TestMatchesHeaders_FuzzyIgnoreFields(t *testing.T) {
	tests := []struct {
		name          string
		matchStrategy string
		ignoreFields  []string
		recordedHdrs  map[string]string
		requestHdrs   http.Header
		expectedMatch bool
	}{
		{
			name:          "Exact match ignores fuzzy_ignore_fields",
			matchStrategy: "exact",
			ignoreFields:  []string{"X-Request-Id"},
			recordedHdrs: map[string]string{
				"Content-Type": "application/json",
				"X-Request-Id": "recorded-id-123",
			},
			requestHdrs: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"different-id-456"},
			},
			expectedMatch: false, // Exact mode doesn't ignore fields
		},
		{
			name:          "Fuzzy match ignores configured header fields",
			matchStrategy: "fuzzy",
			ignoreFields:  []string{"X-Request-Id", "X-Trace-Id"},
			recordedHdrs: map[string]string{
				"Content-Type": "application/json",
				"X-Request-Id": "recorded-id-123",
				"X-Trace-Id":   "trace-abc",
			},
			requestHdrs: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"different-id-456"},
				"X-Trace-Id":   []string{"trace-xyz"},
			},
			expectedMatch: true, // Should match because ignored fields differ
		},
		{
			name:          "Fuzzy match fails when non-ignored headers differ",
			matchStrategy: "fuzzy",
			ignoreFields:  []string{"X-Request-Id"},
			recordedHdrs: map[string]string{
				"Content-Type": "application/json",
				"X-Request-Id": "recorded-id-123",
			},
			requestHdrs: http.Header{
				"Content-Type": []string{"text/plain"},
				"X-Request-Id": []string{"different-id-456"},
			},
			expectedMatch: false, // Should fail because Content-Type differs
		},
		{
			name:          "Fuzzy match with empty ignore list and dynamic headers",
			matchStrategy: "fuzzy",
			ignoreFields:  []string{},
			recordedHdrs: map[string]string{
				"Content-Type":   "application/json",
				"Content-Length": "123",
				"Date":           "Mon, 01 Jan 2024 00:00:00 GMT",
			},
			requestHdrs: http.Header{
				"Content-Type":   []string{"application/json"},
				"Content-Length": []string{"456"},                           // Different but ignored (dynamic)
				"Date":           []string{"Tue, 02 Jan 2024 00:00:00 GMT"}, // Different but ignored (dynamic)
			},
			expectedMatch: true, // Should match because dynamic headers are ignored
		},
		{
			name:          "Fuzzy unordered also respects fuzzy_ignore_fields",
			matchStrategy: "fuzzy-unordered",
			ignoreFields:  []string{"X-Request-Id"},
			recordedHdrs: map[string]string{
				"Content-Type": "application/json",
				"X-Request-Id": "id-1",
			},
			requestHdrs: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"id-2"},
			},
			expectedMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConfig := &config.MockConfig{
				MatchingStrategy:  tt.matchStrategy,
				FuzzyIgnoreFields: tt.ignoreFields,
			}

			// Create a minimal RESTHandler with empty redact patterns
			restHandler := proxy.NewRESTHandler([]string{})

			mockEngine := &MockEngine{
				mockConfig:  mockConfig,
				restHandler: restHandler,
			}

			recordedJSON, _ := json.Marshal(tt.recordedHdrs)
			result := mockEngine.matchesHeaders(string(recordedJSON), tt.requestHdrs)

			if result != tt.expectedMatch {
				t.Errorf("Expected match=%v, got match=%v", tt.expectedMatch, result)
			}
		})
	}
}
