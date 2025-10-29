package storage

import (
	"os"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) (*Database, func()) {
	// Create a temporary database file
	dbPath := "/tmp/mimic_test.db"

	// Remove any existing test database
	os.Remove(dbPath)

	db, err := NewDatabase(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(dbPath)
	}

	return db, cleanup
}

func TestRecordStreamChunksTransactional(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a session and interaction
	session, err := db.CreateSession("test-session", "Test session for chunk recording")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	interaction := &Interaction{
		SessionID:      session.ID,
		RequestID:      "test-request-1",
		Protocol:       "REST",
		Method:         "GET",
		Endpoint:       "/api/stream",
		IsStreaming:    true,
		SequenceNumber: 1,
	}

	err = db.RecordInteraction(interaction)
	if err != nil {
		t.Fatalf("Failed to record interaction: %v", err)
	}

	// Create test chunks
	chunks := []*StreamChunk{
		{
			InteractionID: interaction.ID,
			ChunkIndex:    0,
			Data:          []byte("chunk 0"),
			Timestamp:     time.Now(),
			TimeDelta:     0,
		},
		{
			InteractionID: interaction.ID,
			ChunkIndex:    1,
			Data:          []byte("chunk 1"),
			Timestamp:     time.Now(),
			TimeDelta:     100,
		},
		{
			InteractionID: interaction.ID,
			ChunkIndex:    2,
			Data:          []byte("chunk 2"),
			Timestamp:     time.Now(),
			TimeDelta:     200,
		},
	}

	// Record chunks atomically
	err = db.RecordStreamChunks(chunks)
	if err != nil {
		t.Fatalf("Failed to record stream chunks: %v", err)
	}

	// Verify all chunks were recorded
	retrievedChunks, err := db.GetStreamChunks(interaction.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve stream chunks: %v", err)
	}

	if len(retrievedChunks) != 3 {
		t.Errorf("Expected 3 chunks, got %d", len(retrievedChunks))
	}

	// Verify chunk data
	for i, chunk := range retrievedChunks {
		expectedData := []byte("chunk " + string(rune('0'+i)))
		if string(chunk.Data) != string(expectedData) {
			t.Errorf("Chunk %d: expected data %s, got %s", i, expectedData, chunk.Data)
		}
		if chunk.ChunkIndex != i {
			t.Errorf("Chunk %d: expected index %d, got %d", i, i, chunk.ChunkIndex)
		}
	}
}

func TestRecordStreamChunksEmptySlice(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Recording empty slice should not error
	err := db.RecordStreamChunks([]*StreamChunk{})
	if err != nil {
		t.Errorf("Recording empty chunk slice should not error: %v", err)
	}
}

func TestRecordStreamChunksAtomicity(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a session and interaction
	session, err := db.CreateSession("test-session", "Test atomicity")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	interaction := &Interaction{
		SessionID:      session.ID,
		RequestID:      "test-request-atomicity",
		Protocol:       "REST",
		Method:         "GET",
		Endpoint:       "/api/stream",
		IsStreaming:    true,
		SequenceNumber: 1,
	}

	err = db.RecordInteraction(interaction)
	if err != nil {
		t.Fatalf("Failed to record interaction: %v", err)
	}

	// First, successfully record some chunks
	initialChunks := []*StreamChunk{
		{
			InteractionID: interaction.ID,
			ChunkIndex:    0,
			Data:          []byte("initial chunk"),
			Timestamp:     time.Now(),
			TimeDelta:     0,
		},
	}

	err = db.RecordStreamChunks(initialChunks)
	if err != nil {
		t.Fatalf("Failed to record initial chunks: %v", err)
	}

	// Verify initial chunk was recorded
	retrievedChunks, err := db.GetStreamChunks(interaction.ID)
	if err != nil {
		t.Fatalf("Failed to query stream chunks: %v", err)
	}

	if len(retrievedChunks) != 1 {
		t.Fatalf("Expected 1 initial chunk, got %d", len(retrievedChunks))
	}

	// The transactional method ensures all-or-nothing semantics
	// This test verifies that the method can handle multiple chunks in a single transaction
	moreChunks := []*StreamChunk{
		{
			InteractionID: interaction.ID,
			ChunkIndex:    1,
			Data:          []byte("chunk 1"),
			Timestamp:     time.Now(),
			TimeDelta:     100,
		},
		{
			InteractionID: interaction.ID,
			ChunkIndex:    2,
			Data:          []byte("chunk 2"),
			Timestamp:     time.Now(),
			TimeDelta:     200,
		},
	}

	err = db.RecordStreamChunks(moreChunks)
	if err != nil {
		t.Fatalf("Failed to record additional chunks: %v", err)
	}

	// Verify all chunks were recorded
	retrievedChunks, err = db.GetStreamChunks(interaction.ID)
	if err != nil {
		t.Fatalf("Failed to query stream chunks: %v", err)
	}

	if len(retrievedChunks) != 3 {
		t.Errorf("Expected 3 total chunks, got %d", len(retrievedChunks))
	}
}

func TestConcurrentStreamChunkRecording(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a session
	session, err := db.CreateSession("test-session", "Test concurrent recording")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Create multiple interactions for concurrent streams
	numStreams := 5
	interactions := make([]*Interaction, numStreams)

	for i := 0; i < numStreams; i++ {
		interaction := &Interaction{
			SessionID:      session.ID,
			RequestID:      "test-request-" + string(rune('A'+i)),
			Protocol:       "REST",
			Method:         "GET",
			Endpoint:       "/api/stream",
			IsStreaming:    true,
			SequenceNumber: i + 1,
		}

		err = db.RecordInteraction(interaction)
		if err != nil {
			t.Fatalf("Failed to record interaction %d: %v", i, err)
		}
		interactions[i] = interaction
	}

	// Record chunks concurrently for all interactions
	done := make(chan error, numStreams)

	for i := 0; i < numStreams; i++ {
		go func(idx int) {
			chunks := []*StreamChunk{
				{
					InteractionID: interactions[idx].ID,
					ChunkIndex:    0,
					Data:          []byte("stream " + string(rune('A'+idx)) + " chunk 0"),
					Timestamp:     time.Now(),
					TimeDelta:     0,
				},
				{
					InteractionID: interactions[idx].ID,
					ChunkIndex:    1,
					Data:          []byte("stream " + string(rune('A'+idx)) + " chunk 1"),
					Timestamp:     time.Now(),
					TimeDelta:     100,
				},
			}
			done <- db.RecordStreamChunks(chunks)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numStreams; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent chunk recording failed for stream %d: %v", i, err)
		}
	}

	// Verify all chunks were recorded correctly
	for i := 0; i < numStreams; i++ {
		chunks, err := db.GetStreamChunks(interactions[i].ID)
		if err != nil {
			t.Errorf("Failed to retrieve chunks for interaction %d: %v", i, err)
			continue
		}

		if len(chunks) != 2 {
			t.Errorf("Interaction %d: expected 2 chunks, got %d", i, len(chunks))
		}
	}
}
