package services

import (
	"net/http"
	"os"
	"time"

	"my_project/internal/repositories"
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