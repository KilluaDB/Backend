package service

import (
	"backend/internal/model"
	"backend/internal/postgres/infra"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for HTTP handlers to map 4xx/502/503 distinctly.
var (
	ErrProjectNotFound          = errors.New("project not found or not accessible")
	ErrNoRunningDBInstance      = errors.New("no running database instance for this project")
	ErrNoDBCredentials          = errors.New("no credentials configured for this database instance")
	ErrTextToSQLUnavailable     = errors.New("text-to-sql service unavailable")
	ErrTextToSQLInvalidResponse = errors.New("invalid response from text-to-sql service")
)

// projectGetter is satisfied by *repository.ProjectRepository in production.
type projectGetter interface {
	GetByIDAndUserID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*model.Project, error)
}

// TextToSQLService handles communication with the FastAPI Text-to-SQL service
type TextToSQLService struct {
	baseURL     string
	httpClient  *http.Client
	projectRepo projectGetter
	dsnProvider infra.DSNProvider
}

// DatabaseConnection represents DB connection details for schema extraction
type DatabaseConnection struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
}

type TextToSQLRequest struct {
	Question     string             `json:"question" binding:"required"`
	DBConnection DatabaseConnection `json:"db_connection"`
}

// GenerateSQLResponse is the response from the FastAPI service
type TextToSQLResponse struct {
	Success    bool     `json:"success"`
	SQL        string   `json:"sql,omitempty"`
	Error      string   `json:"error,omitempty"`
	TablesUsed []string `json:"tables_used,omitempty"`
}

// NewTextToSQLService creates a new Text-to-SQL service client
func NewTextToSQLService(dsnProvider infra.DSNProvider, projectRepo projectGetter) *TextToSQLService {
	baseURL := os.Getenv("TEXT_TO_SQL")
	timeout := 120 * time.Second
	if s := os.Getenv("TEXT_TO_SQL_HTTP_TIMEOUT_SECONDS"); s != "" {
		if sec, err := strconv.Atoi(s); err == nil && sec > 0 {
			timeout = time.Duration(sec) * time.Second
		}
	}

	return &TextToSQLService{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout, // LLM + schema extraction can exceed 30s
		},
		projectRepo: projectRepo,
		dsnProvider: dsnProvider,
	}
}

func (s *TextToSQLService) GenerateSQL(ctx context.Context, userID uuid.UUID, req *TextToSQLRequest, projectId uuid.UUID) (*TextToSQLResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	project, err := s.projectRepo.GetByIDAndUserID(ctx, projectId, userID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, ErrProjectNotFound
	}

	dsn, _, err := s.dsnProvider.GetConnectionDSN(ctx, userID, projectId)
	if err != nil {
		log.Printf("[TextToSQLService] DSN resolution failed for project=%s user=%s: %v", projectId.String(), userID.String(), err)
		errMsg := strings.ToLower(err.Error())
		switch {
		case strings.Contains(errMsg, "no running database instance"):
			return nil, ErrNoRunningDBInstance
		case strings.Contains(errMsg, "project not found or not accessible"):
			return nil, ErrProjectNotFound
		}
		return nil, fmt.Errorf("get connection DSN: %w", err)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		log.Printf("[TextToSQLService] DSN parse failed for project=%s user=%s dsn=%q err=%v", projectId.String(), userID.String(), dsn, err)
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		log.Printf("[TextToSQLService] DSN port parse failed for project=%s user=%s host=%q raw_port=%q err=%v", projectId.String(), userID.String(), parsed.Hostname(), parsed.Port(), err)
		return nil, fmt.Errorf("parse DSN port: %w", err)
	}
	password, _ := parsed.User.Password()

	dbConnection := DatabaseConnection{
		Host:     parsed.Hostname(),
		Port:     port,
		Database: "app",
		User:     parsed.User.Username(),
		Password: password,
	}

	req.DBConnection = dbConnection

	jsonBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request to FastAPI
	targetURL := s.baseURL + "/api/v1/generate"
	log.Printf("[TextToSQLService] Sending request to FastAPI url=%s project=%s user=%s", targetURL, projectId.String(), userID.String())
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[TextToSQLService] Request failed: %v", err)
		return nil, fmt.Errorf("%w: %v", ErrTextToSQLUnavailable, err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var result TextToSQLResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[TextToSQLService] Failed to parse response: %v", err)
		return nil, fmt.Errorf("%w: %v", ErrTextToSQLInvalidResponse, err)
	}

	if !result.Success {
		log.Printf("[TextToSQLService] SQL generation failed: %s", result.Error)
		// Return the response so caller can see the error
		return &result, nil
	}

	return &result, nil
}
