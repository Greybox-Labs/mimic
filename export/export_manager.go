package export

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"mimic/config"
	"mimic/storage"
)

type ExportManager struct {
	config   *config.Config
	database *storage.Database
}

func NewExportManager(cfg *config.Config, db *storage.Database) *ExportManager {
	return &ExportManager{
		config:   cfg,
		database: db,
	}
}

func (e *ExportManager) ExportSession(sessionName, outputPath string) error {
	session, err := e.database.GetSession(sessionName)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	interactions, err := e.database.GetInteractionsBySession(session.ID)
	if err != nil {
		return fmt.Errorf("failed to get interactions: %w", err)
	}

	exportInteractions := make([]storage.ExportInteraction, len(interactions))
	for i, interaction := range interactions {
		exportInteraction, err := e.convertToExportInteraction(interaction)
		if err != nil {
			return fmt.Errorf("failed to convert interaction %d: %w", interaction.ID, err)
		}
		exportInteractions[i] = exportInteraction
	}

	exportData := storage.ExportData{
		Version:      "1.0",
		Session:      *session,
		Interactions: exportInteractions,
	}

	return e.writeExportData(exportData, outputPath)
}

func (e *ExportManager) ImportSession(inputPath, sessionName, mergeStrategy string) error {
	exportData, err := e.readExportData(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read export data: %w", err)
	}

	if err := e.validateExportData(exportData); err != nil {
		return fmt.Errorf("invalid export data: %w", err)
	}

	targetSessionName := sessionName
	if targetSessionName == "" {
		targetSessionName = exportData.Session.SessionName
	}

	switch mergeStrategy {
	case "replace":
		if err := e.database.ClearSession(targetSessionName); err != nil {
			return fmt.Errorf("failed to clear existing session: %w", err)
		}
	case "append":
		// Do nothing, just append to existing session
	default:
		mergeStrategy = "append"
	}

	interactions := make([]storage.Interaction, len(exportData.Interactions))
	for i, exportInteraction := range exportData.Interactions {
		interaction, err := e.convertFromExportInteraction(exportInteraction)
		if err != nil {
			return fmt.Errorf("failed to convert interaction %d: %w", i, err)
		}
		interactions[i] = interaction
	}

	if err := e.database.ImportInteractions(targetSessionName, interactions); err != nil {
		return fmt.Errorf("failed to import interactions: %w", err)
	}

	return nil
}

func (e *ExportManager) convertToExportInteraction(interaction storage.Interaction) (storage.ExportInteraction, error) {
	var requestHeaders map[string]string
	if interaction.RequestHeaders != "" {
		if err := json.Unmarshal([]byte(interaction.RequestHeaders), &requestHeaders); err != nil {
			return storage.ExportInteraction{}, fmt.Errorf("failed to unmarshal request headers: %w", err)
		}
	}

	var responseHeaders map[string]string
	if interaction.ResponseHeaders != "" {
		if err := json.Unmarshal([]byte(interaction.ResponseHeaders), &responseHeaders); err != nil {
			return storage.ExportInteraction{}, fmt.Errorf("failed to unmarshal response headers: %w", err)
		}
	}

	var requestBody interface{}
	if len(interaction.RequestBody) > 0 {
		if err := json.Unmarshal(interaction.RequestBody, &requestBody); err != nil {
			requestBody = string(interaction.RequestBody)
		}
	}

	var responseBody interface{}
	if len(interaction.ResponseBody) > 0 {
		if err := json.Unmarshal(interaction.ResponseBody, &responseBody); err != nil {
			responseBody = string(interaction.ResponseBody)
		}
	}

	return storage.ExportInteraction{
		RequestID:  interaction.RequestID,
		Protocol:   interaction.Protocol,
		Method:     interaction.Method,
		Endpoint:   interaction.Endpoint,
		Request: storage.InteractionRequest{
			Headers: requestHeaders,
			Body:    requestBody,
		},
		Response: storage.InteractionResponse{
			Status:  interaction.ResponseStatus,
			Headers: responseHeaders,
			Body:    responseBody,
		},
		Timestamp:      interaction.Timestamp,
		SequenceNumber: interaction.SequenceNumber,
	}, nil
}

func (e *ExportManager) convertFromExportInteraction(exportInteraction storage.ExportInteraction) (storage.Interaction, error) {
	requestHeaders, err := json.Marshal(exportInteraction.Request.Headers)
	if err != nil {
		return storage.Interaction{}, fmt.Errorf("failed to marshal request headers: %w", err)
	}

	responseHeaders, err := json.Marshal(exportInteraction.Response.Headers)
	if err != nil {
		return storage.Interaction{}, fmt.Errorf("failed to marshal response headers: %w", err)
	}

	var requestBody []byte
	if exportInteraction.Request.Body != nil {
		if str, ok := exportInteraction.Request.Body.(string); ok {
			requestBody = []byte(str)
		} else {
			requestBody, err = json.Marshal(exportInteraction.Request.Body)
			if err != nil {
				return storage.Interaction{}, fmt.Errorf("failed to marshal request body: %w", err)
			}
		}
	}

	var responseBody []byte
	if exportInteraction.Response.Body != nil {
		if str, ok := exportInteraction.Response.Body.(string); ok {
			responseBody = []byte(str)
		} else {
			responseBody, err = json.Marshal(exportInteraction.Response.Body)
			if err != nil {
				return storage.Interaction{}, fmt.Errorf("failed to marshal response body: %w", err)
			}
		}
	}

	return storage.Interaction{
		RequestID:       exportInteraction.RequestID,
		Protocol:        exportInteraction.Protocol,
		Method:          exportInteraction.Method,
		Endpoint:        exportInteraction.Endpoint,
		RequestHeaders:  string(requestHeaders),
		RequestBody:     requestBody,
		ResponseStatus:  exportInteraction.Response.Status,
		ResponseHeaders: string(responseHeaders),
		ResponseBody:    responseBody,
		Timestamp:       exportInteraction.Timestamp,
		SequenceNumber:  exportInteraction.SequenceNumber,
	}, nil
}

func (e *ExportManager) writeExportData(data storage.ExportData, outputPath string) error {
	var jsonData []byte
	var err error

	if e.config.Export.PrettyPrint {
		jsonData, err = json.MarshalIndent(data, "", "  ")
	} else {
		jsonData, err = json.Marshal(data)
	}
	if err != nil {
		return fmt.Errorf("failed to marshal export data: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	var writer io.Writer = file
	if e.config.Export.Compress && strings.HasSuffix(outputPath, ".gz") {
		gzWriter := gzip.NewWriter(file)
		defer gzWriter.Close()
		writer = gzWriter
	}

	if _, err := writer.Write(jsonData); err != nil {
		return fmt.Errorf("failed to write export data: %w", err)
	}

	return nil
}

func (e *ExportManager) readExportData(inputPath string) (*storage.ExportData, error) {
	file, err := os.Open(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open input file: %w", err)
	}
	defer file.Close()

	var reader io.Reader = file
	if strings.HasSuffix(inputPath, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	var exportData storage.ExportData
	if err := json.NewDecoder(reader).Decode(&exportData); err != nil {
		return nil, fmt.Errorf("failed to decode export data: %w", err)
	}

	return &exportData, nil
}

func (e *ExportManager) validateExportData(data *storage.ExportData) error {
	if data.Version == "" {
		return fmt.Errorf("missing version field")
	}

	if data.Session.SessionName == "" {
		return fmt.Errorf("missing session name")
	}

	for i, interaction := range data.Interactions {
		if interaction.RequestID == "" {
			return fmt.Errorf("missing request ID in interaction %d", i)
		}
		if interaction.Protocol == "" {
			return fmt.Errorf("missing protocol in interaction %d", i)
		}
		if interaction.Method == "" {
			return fmt.Errorf("missing method in interaction %d", i)
		}
		if interaction.Endpoint == "" {
			return fmt.Errorf("missing endpoint in interaction %d", i)
		}
	}

	return nil
}

func (e *ExportManager) ListExportFormats() []string {
	return []string{"json"}
}

func (e *ExportManager) GetExportInfo(sessionName string) (*storage.ExportData, error) {
	session, err := e.database.GetSession(sessionName)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	interactions, err := e.database.GetInteractionsBySession(session.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get interactions: %w", err)
	}

	return &storage.ExportData{
		Version:      "1.0",
		Session:      *session,
		Interactions: make([]storage.ExportInteraction, len(interactions)),
	}, nil
}