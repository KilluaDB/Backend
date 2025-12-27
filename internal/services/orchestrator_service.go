package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	orchestrator "github.com/KilluaDB/Orchestrator"
	"github.com/google/uuid"
)

type OrchestratorService struct {
	orchestrator *orchestrator.Orchestrator
	ctx          context.Context
}

type CreateContainerRequest struct {
	SessionName   string                 `json:"session_name"`
	DatabaseType  string                 `json:"database_type"`
	Configuration map[string]interface{} `json:"configuration,omitempty"`
}

type CreateContainerResponse struct {
	ID             string `json:"id"`
	SessionName    string `json:"session_name"`
	Status         string `json:"status"`
	ContainerID    string `json:"container_id"`
	ContainerName  string `json:"container_name"`
	ConnectionInfo struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
		Database string `json:"database"`
	} `json:"connection_info"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func NewOrchestratorService() (*OrchestratorService, error) {
	ctx := context.Background()

	// Get Redis connection details from environment
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		return nil, fmt.Errorf("REDIS_ADDR environment variable is required")
	}

	networkName := os.Getenv("ORCHESTRATOR_NETWORK_NAME")
	if networkName == "" {
		return nil, fmt.Errorf("ORCHESTRATOR_NETWORK_NAME environment variable is required")
	}

	subnetCIDR := os.Getenv("ORCHESTRATOR_SUBNET_CIDR")
	if subnetCIDR == "" {
		return nil, fmt.Errorf("ORCHESTRATOR_SUBNET_CIDR environment variable is required")
	}

	gateway := os.Getenv("ORCHESTRATOR_GATEWAY")
	if gateway == "" {
		return nil, fmt.Errorf("ORCHESTRATOR_GATEWAY environment variable is required")
	}

	monitorIntervalStr := os.Getenv("ORCHESTRATOR_MONITOR_INTERVAL")
	if monitorIntervalStr == "" {
		return nil, fmt.Errorf("ORCHESTRATOR_MONITOR_INTERVAL environment variable is required")
	}
	monitorInterval, err := strconv.Atoi(monitorIntervalStr)
	if err != nil {
		return nil, fmt.Errorf("ORCHESTRATOR_MONITOR_INTERVAL must be a valid integer: %w", err)
	}

	// Create orchestrator config
	config := &orchestrator.Config{
		RedisAddr:       redisAddr,
		NetworkName:     networkName,
		SubnetCIDR:      subnetCIDR,
		Gateway:         gateway,
		MonitorInterval: monitorInterval,
	}

	// Create orchestrator instance
	orch, err := orchestrator.New(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestrator: %w", err)
	}

	// Initialize network and sync existing containers
	// Handle the case where the network already exists gracefully
	if err := orch.Initialize(ctx); err != nil {
		// Check if the error is about network already existing
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "already exists") ||
			strings.Contains(errMsg, "network") && strings.Contains(errMsg, "exists") {
			log.Printf("Warning: Network already exists, continuing with existing network: %v", err)
			// Network already exists is not a fatal error, we can continue
		} else {
			return nil, fmt.Errorf("failed to initialize orchestrator: %w", err)
		}
	}

	log.Println("Orchestrator initialized successfully")

	return &OrchestratorService{
		orchestrator: orch,
		ctx:          ctx,
	}, nil
}

func (s *OrchestratorService) CreateContainer(req CreateContainerRequest) (*CreateContainerResponse, error) {
	// Get database image based on type
	image := s.getDatabaseImage(req.DatabaseType)
	if image == "" {
		return nil, fmt.Errorf("unsupported database type: %s", req.DatabaseType)
	}

	// Generate container name
	containerName := fmt.Sprintf("%s-%s", req.DatabaseType, uuid.New().String()[:8])

	// Generate credentials
	user := "admin"
	password := uuid.New().String()[:16]
	database := req.SessionName

	// Build environment variables
	env := map[string]string{
		"POSTGRES_PASSWORD":          password,
		"POSTGRES_USER":              user,
		"POSTGRES_DB":                database,
		"MYSQL_ROOT_PASSWORD":        password,
		"MYSQL_DATABASE":             database,
		"MYSQL_USER":                 user,
		"MONGO_INITDB_ROOT_USERNAME": user,
		"MONGO_INITDB_ROOT_PASSWORD": password,
		"MONGO_INITDB_DATABASE":      database,
	}

	// Add database-specific env vars
	switch req.DatabaseType {
	case "postgresql":
		env["POSTGRES_PASSWORD"] = password
		env["POSTGRES_USER"] = user
		env["POSTGRES_DB"] = database
	case "mysql":
		env["MYSQL_ROOT_PASSWORD"] = password
		env["MYSQL_DATABASE"] = database
		env["MYSQL_USER"] = user
	case "mongodb":
		env["MONGO_INITDB_ROOT_USERNAME"] = user
		env["MONGO_INITDB_ROOT_PASSWORD"] = password
		env["MONGO_INITDB_DATABASE"] = database
	}

	// Get default port
	port := s.getDefaultPort(req.DatabaseType)

	// Set resource limits from configuration if provided
	resourceLimits := orchestrator.ResourceLimits{
		Memory:   512 * 1024 * 1024, // Default 512MiB
		CPUQuota: 100000,            // Default 1 CPU
	}

	if req.Configuration != nil {
		if memoryMB, ok := req.Configuration["memory_mb"].(float64); ok {
			resourceLimits.Memory = int64(memoryMB * 1024 * 1024)
		}
		if cpu, ok := req.Configuration["cpu"].(float64); ok {
			resourceLimits.CPUQuota = int64(cpu * 100000)
		}
	}

	// Get volume mount path based on database type
	volumeMountPath := s.getVolumeMountPath(req.DatabaseType)

	// Create container options
	opts := orchestrator.ContainerOptions{
		Name:            containerName,
		Image:           image,
		Env:             env,
		ResourceLimits:  resourceLimits,
		VolumeMountPath: volumeMountPath,
	}

	// Create and start container
	log.Printf("Creating container with name: %s, image: %s", containerName, image)
	containerID, err := s.orchestrator.CreateContainer(s.ctx, opts)
	if err != nil {
		log.Printf("ERROR: Orchestrator CreateContainer failed: %v", err)
		return nil, fmt.Errorf("failed to create container: %w", err)
	}
	log.Printf("Container created with ID: %s", containerID)

	// Get container IP
	ip, ok := s.orchestrator.GetContainerIP(containerID)
	if !ok {
		log.Printf("Container IP not found in memory, trying Redis for container: %s", containerID)
		// Try to get from Redis
		ip, err = s.orchestrator.GetContainerIPFromRedis(s.ctx, containerID)
		if err != nil {
			log.Printf("ERROR: Failed to get container IP from Redis: %v", err)
			return nil, fmt.Errorf("failed to get container IP: %w", err)
		}
	}
	log.Printf("Container IP retrieved: %s", ip)

	response := &CreateContainerResponse{
		ID:            containerID,
		SessionName:   req.SessionName,
		Status:        "running",
		ContainerID:   containerID,
		ContainerName: containerName,
		ConnectionInfo: struct {
			Host     string `json:"host"`
			Port     int    `json:"port"`
			User     string `json:"user"`
			Password string `json:"password"`
			Database string `json:"database"`
		}{
			Host:     ip,
			Port:     port,
			User:     user,
			Password: password,
			Database: database,
		},
	}

	return response, nil
}

func (s *OrchestratorService) GetContainerStatus(containerID string) (*CreateContainerResponse, error) {
	// Get container IP
	ip, ok := s.orchestrator.GetContainerIP(containerID)
	if !ok {
		var err error
		ip, err = s.orchestrator.GetContainerIPFromRedis(s.ctx, containerID)
		if err != nil {
			return nil, fmt.Errorf("container not found: %s", containerID)
		}
	}

	// For now, we'll return a basic response
	// In a full implementation, you'd query Docker for container status
	response := &CreateContainerResponse{
		ID:          containerID,
		ContainerID: containerID,
		Status:      "running",
		ConnectionInfo: struct {
			Host     string `json:"host"`
			Port     int    `json:"port"`
			User     string `json:"user"`
			Password string `json:"password"`
			Database string `json:"database"`
		}{
			Host: ip,
			Port: 5432, // Default, should be stored/retrieved
		},
	}

	return response, nil
}

func (s *OrchestratorService) DeleteContainer(containerID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.orchestrator.StopContainer(ctx, containerID)
}

// GetContainerIP gets the container IP address from the orchestrator
// Returns the IP and true if found, or empty string and false if not found
func (s *OrchestratorService) GetContainerIP(containerID string) (string, bool) {
	return s.orchestrator.GetContainerIP(containerID)
}

// GetContainerIPFromRedis gets the container IP address from Redis
// This is a fallback when the IP is not in memory
func (s *OrchestratorService) GetContainerIPFromRedis(ctx context.Context, containerID string) (string, error) {
	return s.orchestrator.GetContainerIPFromRedis(ctx, containerID)
}

// Helper functions

func (s *OrchestratorService) getDatabaseImage(databaseType string) string {
	images := map[string]string{
		"postgresql": "postgres:16-alpine",
		"mysql":      "mysql:8.0",
		"mongodb":    "mongo:7",
		"redis":      "redis:7-alpine",
	}

	if image, ok := images[databaseType]; ok {
		return image
	}

	return ""
}

func (s *OrchestratorService) getDefaultPort(databaseType string) int {
	ports := map[string]int{
		"postgresql": 5432,
		"mysql":      3306,
		"mongodb":    27017,
		"redis":      6379,
	}

	if port, ok := ports[databaseType]; ok {
		return port
	}

	return 5432
}

func (s *OrchestratorService) getVolumeMountPath(databaseType string) string {
	paths := map[string]string{
		"postgresql": "/var/lib/postgresql/data",
		"mysql":      "/var/lib/mysql",
		"mongodb":    "/data/db",
		"redis":      "/data",
	}

	if path, ok := paths[databaseType]; ok {
		return path
	}

	// Default to PostgreSQL path if unknown
	return "/var/lib/postgresql/data"
}

// Close closes the orchestrator
func (s *OrchestratorService) Close() error {
	if s.orchestrator != nil {
		return s.orchestrator.Close()
	}
	return nil
}
