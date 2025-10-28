package mock

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"mimic/config"
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
