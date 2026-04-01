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
	"os"
	"strconv"
	"strings"
	"time"

	postgresrepo "backend/internal/postgres/repository"
	"backend/internal/repositories"
	"backend/internal/utils"

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
	instanceRepo *repositories.DatabaseInstanceRepository
	credRepo     *repositories.DatabaseCredentialRepository
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
func NewTextToSQLService(instanceRepo *repositories.DatabaseInstanceRepository, credRepo *repositories.DatabaseCredentialRepository, projectRepo  *postgresrepo.PostgresProjectRepository) *TextToSQLService {
	baseURL := os.Getenv("TEXT_TO_SQL_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:5001" // Default FastAPI URL (AI/integration main.py); use host.docker.internal in Docker (see docker-compose.yml)
	}
	baseURL = strings.TrimRight(baseURL, "/")

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
		instanceRepo: instanceRepo,
		credRepo: credRepo,
	}
}

func (s *TextToSQLService) GenerateSQL(userID uuid.UUID, req *TextToSQLRequest, projectId uuid.UUID) (*TextToSQLResponse, error) {
	project, err := s.projectRepo.GetByIDAndUserID(context.Background(), projectId, userID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, ErrProjectNotFound
	}

	// Find running DB instance for this project
	inst, err := s.instanceRepo.GetRunningByProjectID(projectId)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, ErrNoRunningDBInstance
	}

	// Fetch credentials for the instance
	cred, err := s.credRepo.GetLatestByInstanceID(inst.ID)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, ErrNoDBCredentials
	}

	dbPassword, err := utils.DecryptString(cred.PasswordEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt db password: %w", err)
	}

	dbConnection := DatabaseConnection {
		Host: *inst.Host    ,
		Port: *inst.Port    ,
		Database: "postgres",
		User: cred.Username,
		Password: dbPassword,
	}

	req.DBConnection = dbConnection

	jsonBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request to FastAPI
	resp, err := s.httpClient.Post(
		s.baseURL+"/api/v1/generate",
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

func (s *TextToSQLService) HealthCheck() error {
	resp, err := s.httpClient.Get(s.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("text-to-sql service unavailable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("text-to-sql service unhealthy: status %d", resp.StatusCode)
	}

	return nil
}