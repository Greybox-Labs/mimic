package proxy

import (
	"testing"

	"mimic/config"
	"mimic/storage"
)

func TestNewGRPCHandler(t *testing.T) {
	redactPatterns := []string{"password", "token"}
	handler := NewGRPCHandler(redactPatterns)

	if handler == nil {
		t.Fatal("Expected non-nil gRPC handler")
	}

	if len(handler.redactPatterns) != 2 {
		t.Errorf("Expected 2 redact patterns, got %d", len(handler.redactPatterns))
	}
}

func TestGRPCHandlerRedactSensitiveData(t *testing.T) {
	redactPatterns := []string{"password"}
	handler := NewGRPCHandler(redactPatterns)

	data := `{"username": "john", "password": "secret123"}`
	redacted := handler.redactSensitiveData(data)

	if redacted == data {
		t.Error("Expected data to be redacted")
	}

	if !contains(redacted, "[REDACTED]") {
		t.Error("Expected data to contain [REDACTED]")
	}
}

func TestRawGRPCProxyUnaryCallDetection(t *testing.T) {
	db, err := storage.NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	proxyConfig := config.ProxyConfig{
		Protocol:    "grpc",
		TargetHost:  "localhost",
		TargetPort:  9090,
		SessionName: "test-session",
	}

	grpcHandler := NewGRPCHandler([]string{})
	session, _ := db.GetOrCreateSession("test", "test")
	
	rawProxy := NewRawGRPCProxy(&proxyConfig, "record", db, session, grpcHandler)

	// Test unary call detection
	testCases := []struct {
		method   string
		expected bool
	}{
		{"/service/GetInfo", true},
		{"/service/CreateUser", true},
		{"/service/UpdateData", true},
		{"/service/DeleteItem", true},
		{"/service/StreamData", false},
		{"/service/WatchEvents", false},
		{"/astria.sequencerblock.v1.SequencerService/GetUpgradesInfo", true},
	}

	for _, tc := range testCases {
		result := rawProxy.isLikelyUnaryCall(tc.method)
		if result != tc.expected {
			t.Errorf("Method %s: expected %v, got %v", tc.method, tc.expected, result)
		}
	}
}

func TestProxyEngineWithGRPC(t *testing.T) {
	// Create temporary database
	db, err := storage.NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Create gRPC proxy config
	proxyConfig := config.ProxyConfig{
		Protocol:    "grpc",
		TargetHost:  "localhost",
		TargetPort:  9090,
		SessionName: "test-session",
	}

	// Create proxy engine
	engine, err := NewProxyEngine(proxyConfig, db)
	if err != nil {
		t.Fatalf("Failed to create proxy engine: %v", err)
	}
	defer engine.Stop()

	// Verify gRPC components are initialized
	if engine.grpcHandler == nil {
		t.Error("Expected gRPC handler to be initialized")
	}

	if engine.grpcServer == nil {
		t.Error("Expected gRPC server to be initialized for gRPC protocol")
	}
}

func TestProxyEngineWithHTTP(t *testing.T) {
	// Create temporary database
	db, err := storage.NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Create HTTP proxy config
	proxyConfig := config.ProxyConfig{
		Protocol:    "http",
		TargetHost:  "localhost",
		TargetPort:  8080,
		SessionName: "test-session",
	}

	// Create proxy engine
	engine, err := NewProxyEngine(proxyConfig, db)
	if err != nil {
		t.Fatalf("Failed to create proxy engine: %v", err)
	}
	defer engine.Stop()

	// Verify HTTP components are initialized
	if engine.restHandler == nil {
		t.Error("Expected REST handler to be initialized")
	}

	// Verify gRPC components are NOT initialized for HTTP protocol
	if engine.grpcServer != nil {
		t.Error("Expected gRPC server to be nil for HTTP protocol")
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
