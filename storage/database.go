package storage

import (
	"database/sql"
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

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

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
		FOREIGN KEY (session_id) REFERENCES sessions(id)
	);`

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_endpoint_method ON interactions(endpoint, method);",
		"CREATE INDEX IF NOT EXISTS idx_session_sequence ON interactions(session_id, sequence_number);",
		"CREATE INDEX IF NOT EXISTS idx_request_id ON interactions(request_id);",
	}

	if _, err := d.db.Exec(sessionsTable); err != nil {
		return fmt.Errorf("failed to create sessions table: %w", err)
	}

	if _, err := d.db.Exec(interactionsTable); err != nil {
		return fmt.Errorf("failed to create interactions table: %w", err)
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
			response_body, timestamp, sequence_number, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

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
	)
	if err != nil {
		return fmt.Errorf("failed to record interaction: %w", err)
	}

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
			   response_body, timestamp, sequence_number, metadata
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
			   response_body, timestamp, sequence_number, metadata
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

	// Delete all interactions first (due to foreign key constraints)
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
				response_body, timestamp, sequence_number, metadata
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

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
		)
		if err != nil {
			return fmt.Errorf("failed to import interaction: %w", err)
		}
	}

	return tx.Commit()
}
