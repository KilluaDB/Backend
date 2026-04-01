package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"my_project/internal/repositories"
	"my_project/internal/utils"

	"github.com/google/uuid"
)

// TextToSQLService handles communication with the FastAPI Text-to-SQL service
type TextToSQLService struct {
	baseURL    string
	httpClient *http.Client
	projectRepo  *repositories.ProjectRepository
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
func NewTextToSQLService(instanceRepo *repositories.DatabaseInstanceRepository, credRepo *repositories.DatabaseCredentialRepository, projectRepo  *repositories.ProjectRepository) *TextToSQLService {
	baseURL := os.Getenv("TEXT_TO_SQL_URL")
	if baseURL == "" {
		baseURL = "http://localhost:5001" // Default FastAPI URL
	}

	return &TextToSQLService{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // LLM calls can take time
		},
		projectRepo: projectRepo,
		instanceRepo: instanceRepo,
		credRepo: credRepo,
	}
}

func (s *TextToSQLService) GenerateSQL(userID uuid.UUID, req *TextToSQLRequest, projectId uuid.UUID) (*TextToSQLResponse, error) {
	project, err := s.projectRepo.GetByIDAndUserID(projectId, userID)
	if err != nil {
		// return nil, nil, err
	}
	if project == nil {
		// return nil, nil, errors.New("project not found or not accessible")
	}

	// Find running DB instance for this project
	inst, err := s.instanceRepo.GetRunningByProjectID(projectId)
	if err != nil {
		// return nil, nil, err
	}
	if inst == nil {
		// return nil, nil, errors.New("no running database instance for this project")
	}

	// Fetch credentials for the instance
	cred, err := s.credRepo.GetLatestByInstanceID(inst.ID)
	if err != nil {
		// return nil, nil, err
	}
	if cred == nil {
		// return nil, nil, errors.New("no credentials configured for this database instance")
	}

	dbPassword, err := utils.DecryptString(cred.PasswordEncrypted)
	dbConnection := DatabaseConnection {
		Host: *inst.Endpoint    ,
		Port: *inst.Port    ,
		Database: "postgres",
		User: cred.Username,
		Password: dbPassword,
	}

	req.DBConnection = dbConnection

	jsonBody, err := json.Marshal(req)
	if err != nil {
		// return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request to FastAPI
	resp, err := s.httpClient.Post(
		s.baseURL+"/api/v1/generate",
		"application/json",
		bytes.NewBuffer(jsonBody),
	)
	if err != nil {
		log.Printf("[TextToSQLService] Request failed: %v", err)
		return nil, errors.New("text-to-sql service unavailable")
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
		return nil, errors.New("invalid response from text-to-sql service")
	}

	if !result.Success {
		log.Printf("[TextToSQLService] SQL generation failed: %s", result.Error)
		// Return the response so caller can see the error
		return &result, nil
	}

	return &result, nil
}