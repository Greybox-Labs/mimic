package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	db *sql.DB
}

func NewDatabase(dbPath string) (*Database, error) {
	if len(dbPath) == 0 {
		return nil, fmt.Errorf("database path cannot be empty")
	}

	// Expand tilde in path
	if dbPath[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		dbPath = filepath.Join(homeDir, dbPath[1:])
	}

	// Ensure directory exists
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Add WAL mode and busy timeout for better concurrency
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Allow more concurrent connections with WAL mode
	// WAL mode supports multiple readers and one writer simultaneously
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	database := &Database{db: db}
	if err := database.createTables(); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return database, nil
}

func (d *Database) createTables() error {
	sessionsTable := `
	CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_name TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		description TEXT
	);`

	interactionsTable := `
	CREATE TABLE IF NOT EXISTS interactions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id INTEGER NOT NULL,
		request_id TEXT UNIQUE NOT NULL,
		protocol TEXT NOT NULL CHECK(protocol IN ('REST', 'gRPC')),
		method TEXT NOT NULL,
		endpoint TEXT NOT NULL,
		request_headers TEXT,
		request_body BLOB,
		response_status INTEGER,
		response_headers TEXT,
		response_body BLOB,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		sequence_number INTEGER NOT NULL,
		metadata TEXT,
		is_streaming INTEGER DEFAULT 0,
		FOREIGN KEY (session_id) REFERENCES sessions(id)
	);`

	streamChunksTable := `
	CREATE TABLE IF NOT EXISTS stream_chunks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		interaction_id INTEGER NOT NULL,
		chunk_index INTEGER NOT NULL,
		data BLOB,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		time_delta INTEGER DEFAULT 0,
		FOREIGN KEY (interaction_id) REFERENCES interactions(id) ON DELETE CASCADE
	);`

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_endpoint_method ON interactions(endpoint, method);",
		"CREATE INDEX IF NOT EXISTS idx_session_sequence ON interactions(session_id, sequence_number);",
		"CREATE INDEX IF NOT EXISTS idx_request_id ON interactions(request_id);",
		"CREATE INDEX IF NOT EXISTS idx_stream_chunks ON stream_chunks(interaction_id, chunk_index);",
	}

	if _, err := d.db.Exec(sessionsTable); err != nil {
		return fmt.Errorf("failed to create sessions table: %w", err)
	}

	if _, err := d.db.Exec(interactionsTable); err != nil {
		return fmt.Errorf("failed to create interactions table: %w", err)
	}

	if _, err := d.db.Exec(streamChunksTable); err != nil {
		return fmt.Errorf("failed to create stream_chunks table: %w", err)
	}

	for _, index := range indexes {
		if _, err := d.db.Exec(index); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) CreateSession(sessionName, description string) (*Session, error) {
	query := `INSERT INTO sessions (session_name, description) VALUES (?, ?)`
	result, err := d.db.Exec(query, sessionName, description)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get session ID: %w", err)
	}

	return &Session{
		ID:          int(id),
		SessionName: sessionName,
		CreatedAt:   time.Now(),
		Description: description,
	}, nil
}

func (d *Database) GetSession(sessionName string) (*Session, error) {
	query := `SELECT id, session_name, created_at, description FROM sessions WHERE session_name = ?`
	row := d.db.QueryRow(query, sessionName)

	var session Session
	err := row.Scan(&session.ID, &session.SessionName, &session.CreatedAt, &session.Description)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session not found: %s", sessionName)
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return &session, nil
}

func (d *Database) GetOrCreateSession(sessionName, description string) (*Session, error) {
	session, err := d.GetSession(sessionName)
	if err != nil {
		if err.Error() == fmt.Sprintf("session not found: %s", sessionName) {
			return d.CreateSession(sessionName, description)
		}
		return nil, err
	}
	return session, nil
}

func (d *Database) ListSessions() ([]Session, error) {
	query := `SELECT id, session_name, created_at, description FROM sessions ORDER BY created_at DESC`
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var session Session
		err := rows.Scan(&session.ID, &session.SessionName, &session.CreatedAt, &session.Description)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

func (d *Database) RecordInteraction(interaction *Interaction) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	sequenceNumber, err := d.getNextSequenceNumber(tx, interaction.SessionID, interaction.Endpoint)
	if err != nil {
		return fmt.Errorf("failed to get sequence number: %w", err)
	}

	interaction.SequenceNumber = sequenceNumber
	interaction.Timestamp = time.Now()

	query := `
		INSERT INTO interactions (
			session_id, request_id, protocol, method, endpoint,
			request_headers, request_body, response_status, response_headers,
			response_body, timestamp, sequence_number, metadata, is_streaming
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := tx.Exec(query,
		interaction.SessionID,
		interaction.RequestID,
		interaction.Protocol,
		interaction.Method,
		interaction.Endpoint,
		interaction.RequestHeaders,
		interaction.RequestBody,
		interaction.ResponseStatus,
		interaction.ResponseHeaders,
		interaction.ResponseBody,
		interaction.Timestamp,
		interaction.SequenceNumber,
		interaction.Metadata,
		interaction.IsStreaming,
	)
	if err != nil {
		return fmt.Errorf("failed to record interaction: %w", err)
	}

	// Get the interaction ID for potential stream chunks
	interactionID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get interaction ID: %w", err)
	}
	interaction.ID = int(interactionID)

	return tx.Commit()
}

func (d *Database) getNextSequenceNumber(tx *sql.Tx, sessionID int, endpoint string) (int, error) {
	query := `SELECT COALESCE(MAX(sequence_number), 0) + 1 FROM interactions WHERE session_id = ? AND endpoint = ?`
	row := tx.QueryRow(query, sessionID, endpoint)

	var sequenceNumber int
	err := row.Scan(&sequenceNumber)
	if err != nil {
		return 0, fmt.Errorf("failed to get next sequence number: %w", err)
	}

	return sequenceNumber, nil
}

func (d *Database) FindMatchingInteractions(sessionID int, method, endpoint string) ([]Interaction, error) {
	query := `
		SELECT id, session_id, request_id, protocol, method, endpoint,
			   request_headers, request_body, response_status, response_headers,
			   response_body, timestamp, sequence_number, metadata, is_streaming
		FROM interactions
		WHERE session_id = ? AND method = ? AND endpoint = ?
		ORDER BY sequence_number ASC`

	rows, err := d.db.Query(query, sessionID, method, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to find matching interactions: %w", err)
	}
	defer rows.Close()

	var interactions []Interaction
	for rows.Next() {
		var interaction Interaction
		err := rows.Scan(
			&interaction.ID,
			&interaction.SessionID,
			&interaction.RequestID,
			&interaction.Protocol,
			&interaction.Method,
			&interaction.Endpoint,
			&interaction.RequestHeaders,
			&interaction.RequestBody,
			&interaction.ResponseStatus,
			&interaction.ResponseHeaders,
			&interaction.ResponseBody,
			&interaction.Timestamp,
			&interaction.SequenceNumber,
			&interaction.Metadata,
			&interaction.IsStreaming,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan interaction: %w", err)
		}
		interactions = append(interactions, interaction)
	}

	return interactions, nil
}

func (d *Database) GetInteractionsBySession(sessionID int) ([]Interaction, error) {
	query := `
		SELECT id, session_id, request_id, protocol, method, endpoint,
			   request_headers, request_body, response_status, response_headers,
			   response_body, timestamp, sequence_number, metadata, is_streaming
		FROM interactions
		WHERE session_id = ?
		ORDER BY sequence_number ASC`

	rows, err := d.db.Query(query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get interactions by session: %w", err)
	}
	defer rows.Close()

	var interactions []Interaction
	for rows.Next() {
		var interaction Interaction
		err := rows.Scan(
			&interaction.ID,
			&interaction.SessionID,
			&interaction.RequestID,
			&interaction.Protocol,
			&interaction.Method,
			&interaction.Endpoint,
			&interaction.RequestHeaders,
			&interaction.RequestBody,
			&interaction.ResponseStatus,
			&interaction.ResponseHeaders,
			&interaction.ResponseBody,
			&interaction.Timestamp,
			&interaction.SequenceNumber,
			&interaction.Metadata,
			&interaction.IsStreaming,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan interaction: %w", err)
		}
		interactions = append(interactions, interaction)
	}

	return interactions, nil
}

func (d *Database) GetAllSessions() ([]Session, error) {
	query := `
		SELECT id, session_name, created_at, description
		FROM sessions
		ORDER BY created_at DESC`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var session Session
		err := rows.Scan(
			&session.ID,
			&session.SessionName,
			&session.CreatedAt,
			&session.Description,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

func (d *Database) ClearAllSessions() error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete all stream chunks and interactions first (due to foreign key constraints)
	_, err = tx.Exec("DELETE FROM stream_chunks")
	if err != nil {
		return fmt.Errorf("failed to delete stream chunks: %w", err)
	}

	_, err = tx.Exec("DELETE FROM interactions")
	if err != nil {
		return fmt.Errorf("failed to delete interactions: %w", err)
	}

	// Then delete all sessions
	_, err = tx.Exec("DELETE FROM sessions")
	if err != nil {
		return fmt.Errorf("failed to delete sessions: %w", err)
	}

	return tx.Commit()
}

func (d *Database) ClearSession(sessionName string) error {
	session, err := d.GetSession(sessionName)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete stream chunks first (due to foreign key constraints)
	if _, err := tx.Exec("DELETE FROM stream_chunks WHERE interaction_id IN (SELECT id FROM interactions WHERE session_id = ?)", session.ID); err != nil {
		return fmt.Errorf("failed to delete stream chunks: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM interactions WHERE session_id = ?", session.ID); err != nil {
		return fmt.Errorf("failed to delete interactions: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM sessions WHERE id = ?", session.ID); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return tx.Commit()
}

func (d *Database) ImportInteractions(sessionName string, interactions []Interaction) error {
	session, err := d.GetOrCreateSession(sessionName, "Imported session")
	if err != nil {
		return fmt.Errorf("failed to get or create session: %w", err)
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, interaction := range interactions {
		interaction.SessionID = session.ID
		query := `
			INSERT INTO interactions (
				session_id, request_id, protocol, method, endpoint,
				request_headers, request_body, response_status, response_headers,
				response_body, timestamp, sequence_number, metadata, is_streaming
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		_, err = tx.Exec(query,
			interaction.SessionID,
			interaction.RequestID,
			interaction.Protocol,
			interaction.Method,
			interaction.Endpoint,
			interaction.RequestHeaders,
			interaction.RequestBody,
			interaction.ResponseStatus,
			interaction.ResponseHeaders,
			interaction.ResponseBody,
			interaction.Timestamp,
			interaction.SequenceNumber,
			interaction.Metadata,
			interaction.IsStreaming,
		)
		if err != nil {
			return fmt.Errorf("failed to import interaction: %w", err)
		}
	}

	return tx.Commit()
}

// ImportInteractionWithChunks imports a single interaction along with its stream chunks
func (d *Database) ImportInteractionWithChunks(sessionName string, interaction Interaction, chunks []StreamChunk) error {
	session, err := d.GetOrCreateSession(sessionName, "Imported session")
	if err != nil {
		return fmt.Errorf("failed to get or create session: %w", err)
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	interaction.SessionID = session.ID
	query := `
		INSERT INTO interactions (
			session_id, request_id, protocol, method, endpoint,
			request_headers, request_body, response_status, response_headers,
			response_body, timestamp, sequence_number, metadata, is_streaming
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := tx.Exec(query,
		interaction.SessionID,
		interaction.RequestID,
		interaction.Protocol,
		interaction.Method,
		interaction.Endpoint,
		interaction.RequestHeaders,
		interaction.RequestBody,
		interaction.ResponseStatus,
		interaction.ResponseHeaders,
		interaction.ResponseBody,
		interaction.Timestamp,
		interaction.SequenceNumber,
		interaction.Metadata,
		interaction.IsStreaming,
	)
	if err != nil {
		return fmt.Errorf("failed to import interaction: %w", err)
	}

	// Get the newly created interaction ID
	interactionID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get interaction ID: %w", err)
	}

	// Import stream chunks if any
	if len(chunks) > 0 {
		chunkQuery := `
			INSERT INTO stream_chunks (
				interaction_id, chunk_index, data, timestamp, time_delta
			) VALUES (?, ?, ?, ?, ?)`

		for _, chunk := range chunks {
			// Use chunk timestamp if provided, otherwise use current time
			timestamp := chunk.Timestamp
			if timestamp.IsZero() {
				timestamp = time.Now()
			}

			_, err = tx.Exec(chunkQuery,
				interactionID,
				chunk.ChunkIndex,
				chunk.Data,
				timestamp,
				chunk.TimeDelta,
			)
			if err != nil {
				return fmt.Errorf("failed to import stream chunk: %w", err)
			}
		}
	}

	return tx.Commit()
}

// RecordStreamChunk stores a single chunk of a streaming response
func (d *Database) RecordStreamChunk(chunk *StreamChunk) error {
	query := `
		INSERT INTO stream_chunks (
			interaction_id, chunk_index, data, timestamp, time_delta
		) VALUES (?, ?, ?, ?, ?)`

	_, err := d.db.Exec(query,
		chunk.InteractionID,
		chunk.ChunkIndex,
		chunk.Data,
		chunk.Timestamp,
		chunk.TimeDelta,
	)
	if err != nil {
		return fmt.Errorf("failed to record stream chunk: %w", err)
	}

	return nil
}

// GetStreamChunks retrieves all chunks for a streaming interaction
func (d *Database) GetStreamChunks(interactionID int) ([]StreamChunk, error) {
	query := `
		SELECT id, interaction_id, chunk_index, data, timestamp, time_delta
		FROM stream_chunks
		WHERE interaction_id = ?
		ORDER BY chunk_index ASC`

	rows, err := d.db.Query(query, interactionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get stream chunks: %w", err)
	}
	defer rows.Close()

	var chunks []StreamChunk
	for rows.Next() {
		var chunk StreamChunk
		err := rows.Scan(
			&chunk.ID,
			&chunk.InteractionID,
			&chunk.ChunkIndex,
			&chunk.Data,
			&chunk.Timestamp,
			&chunk.TimeDelta,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan stream chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// MarkInteractionAsPartial updates an interaction's metadata to indicate that
// some chunks failed to record, leaving the interaction in a partial state.
func (d *Database) MarkInteractionAsPartial(interactionID int, failedChunks []int) error {
	// Build metadata struct and marshal to JSON
	metadata := map[string]interface{}{
		"status":        "partial",
		"failed_chunks": failedChunks,
	}

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `UPDATE interactions SET metadata = ? WHERE id = ?`
	_, err = d.db.Exec(query, string(metadataBytes), interactionID)
	if err != nil {
		return fmt.Errorf("failed to mark interaction as partial: %w", err)
	}

	return nil
}
