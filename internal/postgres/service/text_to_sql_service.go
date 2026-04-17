package service

import (
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

	postgresrepo "backend/internal/postgres/repository"
	_ "backend/internal/repositories"
	// "backend/internal/services"
	_ "backend/internal/utils"

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

// TextToSQLService handles communication with the FastAPI Text-to-SQL service
type TextToSQLService struct {
	baseURL    string
	httpClient *http.Client
	projectRepo  *postgresrepo.PostgresProjectRepository
	dsnProvider	 DSNProvider
	// instanceRepo *repositories.DatabaseInstanceRepository
	// credRepo     *repositories.DatabaseCredentialRepository
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
	Question  string `json:"question" binding:"required"`
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
func NewTextToSQLService(dsnProvider DSNProvider, projectRepo  *postgresrepo.PostgresProjectRepository) *TextToSQLService {
	baseURL := os.Getenv("TEXT_TO_SQL_URL")	

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
		// credRepo: credRepo,
	}
}

func (s *TextToSQLService) GenerateSQL(userID uuid.UUID, req *TextToSQLRequest, projectId uuid.UUID) (*TextToSQLResponse, error) {
	ctx := context.Background()

	project, err := s.projectRepo.GetByIDAndUserID(context.Background(), projectId, userID)
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


	dbConnection := DatabaseConnection {
		Host: parsed.Hostname()    ,
		Port: port    ,			
		Database: "app",
		User: parsed.User.Username(),	
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
	resp, err := s.httpClient.Post(
		targetURL,
		"application/json",
		bytes.NewBuffer(jsonBody),
	)
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