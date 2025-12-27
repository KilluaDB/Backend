package server

import (
	"backend/internal/config"
	"backend/internal/database"
	"backend/internal/handlers"
	"backend/internal/repositories"
	"backend/internal/routes"
	"backend/internal/services"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv/autoload"
)

type Server struct {
	port int
	pool *pgxpool.Pool
}

func NewServer() *http.Server {
	// Validate required environment variables
	if err := validateRequiredEnvVars(); err != nil {
		log.Fatalf("Missing required environment variable: %v", err)
	}

	portStr := os.Getenv("PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatalf("PORT must be a valid integer: %v", err)
	}
	if port <= 0 || port > 65535 {
		log.Fatalf("PORT must be between 1 and 65535, got: %d", port)
	}

	// Ensure database exists (create if it doesn't)
	if err := database.EnsureDatabaseExists(); err != nil {
		log.Fatalf("failed to ensure database exists: %v", err)
	}

	// Connect to database using pgxpool
	pool, err := database.Connect()
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// Run migrations
	if err := database.RunMigrations(pool); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	s := &Server{
		port: port,
		pool: pool,
	}

	// Dependency injection
	userRepo := repositories.NewUserRepository(pool)
	sessionRepo := repositories.NewSessionRepository(pool)
	userService := services.NewUserService(userRepo, sessionRepo)
	authService := services.NewAuthService(userRepo)
	authHandler := handlers.NewAuthHandler(authService)
	userHandler := handlers.NewUserHandler(userService)

	// Google Auth dependencies
	googleAuthService := services.NewGoogleAuthService(userRepo)
	oauthConfig, err := config.OAuthConfig()
	if err != nil {
		log.Fatalf("failed to initialize OAuth config: %v", err)
	}
	googleAuthHandler := handlers.NewGoogleAuthHandler(googleAuthService, oauthConfig)

	// Project dependencies
	projectRepo := repositories.NewProjectRepository(pool)
	dbInstanceRepo := repositories.NewDatabaseInstanceRepository(pool)
	dbCredentialRepo := repositories.NewDatabaseCredentialRepository(pool)
	orchestratorService, err := services.NewOrchestratorService()
	if err != nil {
		log.Fatalf("failed to initialize orchestrator: %v", err)
	}
	projectService := services.NewProjectService(projectRepo, orchestratorService, dbInstanceRepo, dbCredentialRepo)
	projectHandler := handlers.NewProjectHandler(projectService)

	// Query dependencies
	queryHistoryRepo := repositories.NewQueryHistoryRepository(pool)
	queryService := services.NewQueryService(projectRepo, dbInstanceRepo, dbCredentialRepo, queryHistoryRepo, orchestratorService)
	queryHandler := handlers.NewQueryHandler(queryService)

	//
	tableRepo := repositories.NewTableRepository(pool)
	tableService := services.NewTableService(projectRepo, dbInstanceRepo, dbCredentialRepo, queryHistoryRepo, tableRepo, orchestratorService)
	tableHandler := handlers.NewTableHandler(tableService)

	// Schema dependencies
	schemaService := services.NewSchemaService(projectRepo, dbInstanceRepo, dbCredentialRepo, orchestratorService)
	schemaHandler := handlers.NewSchemaHandler(schemaService)

	// Initialize Gin router
	router := gin.Default()

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	// Register all routes
	routes.RegisterRoutes(router, authHandler, googleAuthHandler, userHandler, userRepo, projectHandler, queryHandler, schemaHandler, tableHandler)
	// Create and configure the HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      router,
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 5 * time.Minute, // Increased to handle long-running queries
	}

	return server
}

func validateRequiredEnvVars() error {
	required := map[string]string{
		"PORT":                          os.Getenv("PORT"),
		"DB_HOST":                       os.Getenv("DB_HOST"),
		"DB_PORT":                       os.Getenv("DB_PORT"),
		"DB_USERNAME":                   os.Getenv("DB_USERNAME"),
		"DB_PASSWORD":                   os.Getenv("DB_PASSWORD"),
		"DB_DATABASE":                   os.Getenv("DB_DATABASE"),
		"DB_ADMIN_USER":                 os.Getenv("DB_ADMIN_USER"),
		"DB_ADMIN_PASSWORD":             os.Getenv("DB_ADMIN_PASSWORD"),
		"ACCESS_TOKEN_SECRET":           os.Getenv("ACCESS_TOKEN_SECRET"),
		"REFRESH_TOKEN_SECRET":          os.Getenv("REFRESH_TOKEN_SECRET"),
		"REDIS_ADDR":                    os.Getenv("REDIS_ADDR"),
		"ORCHESTRATOR_NETWORK_NAME":     os.Getenv("ORCHESTRATOR_NETWORK_NAME"),
		"ORCHESTRATOR_SUBNET_CIDR":      os.Getenv("ORCHESTRATOR_SUBNET_CIDR"),
		"ORCHESTRATOR_GATEWAY":          os.Getenv("ORCHESTRATOR_GATEWAY"),
		"ORCHESTRATOR_MONITOR_INTERVAL": os.Getenv("ORCHESTRATOR_MONITOR_INTERVAL"),
		"GOOGLE_CLIENT_ID":              os.Getenv("GOOGLE_CLIENT_ID"),
		"GOOGLE_CLIENT_SECRET":          os.Getenv("GOOGLE_CLIENT_SECRET"),
		"GOOGLE_REDIRECT_URL":           os.Getenv("GOOGLE_REDIRECT_URL"),
	}

	for name, value := range required {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}

	return nil
}
