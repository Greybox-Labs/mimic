package mock

import (
	"testing"

	"mimic/config"
	"mimic/storage"
)

func TestMockEngineWithGRPC(t *testing.T) {
	// Create temporary database
	db, err := storage.NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Create gRPC mock config
	proxyConfig := config.ProxyConfig{
		Mode:        "mock",
		Protocol:    "grpc",
		SessionName: "test-session",
	}

	// Create mock engine
	engine, err := NewMockEngine(proxyConfig, db)
	if err != nil {
		t.Fatalf("Failed to create mock engine: %v", err)
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

func TestMockEngineWithHTTP(t *testing.T) {
	// Create temporary database
	db, err := storage.NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Create HTTP mock config
	proxyConfig := config.ProxyConfig{
		Mode:        "mock",
		Protocol:    "http",
		SessionName: "test-session",
	}

	// Create mock engine
	engine, err := NewMockEngine(proxyConfig, db)
	if err != nil {
		t.Fatalf("Failed to create mock engine: %v", err)
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
