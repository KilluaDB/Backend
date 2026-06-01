package server

import (
	"backend/internal/backup"
	"backend/internal/config"
	"backend/internal/database"
	"backend/internal/handlers"
	mongohandler "backend/internal/mongodb/handler"
	mongoinfra "backend/internal/mongodb/infra"
	mongorepo "backend/internal/mongodb/repository"
	mongosvc "backend/internal/mongodb/service"
	pghandler "backend/internal/postgres/handler"
	pginfra "backend/internal/postgres/infra"
	postgresrepo "backend/internal/postgres/repository"
	postgressvc "backend/internal/postgres/service"
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

var pgInstanceManager *pginfra.PostgresConnectionManager
var mongoInstanceManager *mongoinfra.MongoConnectionManager

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

	// Redis for persistent refresh tokens (client lives for process lifetime)
	redisClient, err := config.RedisClient()
	if err != nil {
		log.Fatalf("failed to connect to Redis: %v", err)
	}
	refreshStore := config.NewRefreshTokenStore(redisClient, 30*24*time.Hour) // 30 days TTL

	// Dependency injection
	userRepo := repositories.NewUserRepository(pool)
	projectRepo := repositories.NewProjectRepository(pool)
	userService := services.NewUserService(userRepo, projectRepo, pool)
	authService := services.NewAuthService(userRepo, refreshStore)
	authHandler := handlers.NewAuthHandler(authService)
	userHandler := handlers.NewUserHandler(userService)

	// Google Auth dependencies
	googleAuthService := services.NewGoogleAuthService(userRepo)
	oauthConfig, err := config.OAuthConfig()
	if err != nil {
		log.Fatalf("failed to initialize OAuth config: %v", err)
	}
	googleAuthHandler := handlers.NewGoogleAuthHandler(googleAuthService, oauthConfig)

	// Project dependencies (provisioner uses K8s operators for DB instances)
	provisioner, err := services.NewOperatorProvisioner()
	if err != nil {
		log.Fatalf("failed to initialize operator provisioner: %v", err)
	}
	dsnService := services.NewInstanceDsnService(projectRepo, provisioner)
	instanceConn := pginfra.NewPostgresConnectionManager(dsnService)
	pgInstanceManager = instanceConn
	// Postgres-specific: table (includes row/column ops), schema, query
	tableRepo := postgresrepo.NewTableRepository()
	tableService := postgressvc.NewTableService(instanceConn, tableRepo)
	projectService := services.NewProjectService(projectRepo, provisioner, tableService, instanceConn)
	projectHandler := handlers.NewProjectHandler(projectService)

	// Query dependencies
	const maxPostgresQueryLimit = 50
	pgQueryService := postgressvc.NewQueryService(instanceConn, maxPostgresQueryLimit)
	pgQueryHandler := pghandler.NewQueryHandler(pgQueryService)

	// textToSqlRepo := repositories.NewQueryHistoryRepository(pool)
	textToSqlService := postgressvc.NewTextToSQLService(dsnService, projectRepo)
	textToSqlHandler := pghandler.NewTextToSQLHandler(textToSqlService, pgQueryService)

	//
	// tableRepo := repositories.NewTableRepository(pool)
	// tableService := services.NewTableService(projectRepo, dbInstanceRepo, dbCredentialRepo, queryHistoryRepo, tableRepo)

	//	mongoQueryHistoryRepo := mongorepo.NewQueryHistoryRepository(pool)
	//	mongoQueryService := mongosvc.NewQueryService(instanceConn, mongoDBDriver, mongoQueryHistoryRepo)
	//	mongoQueryHandler := mongodbhandler.NewQueryHandler(mongoQueryService)

	schemaService := postgressvc.NewSchemaService(instanceConn)
	schemaHandler := pghandler.NewSchemaHandler(schemaService)
	tableHandler := pghandler.NewTableHandler(tableService)
	dashOverviewSvc := postgressvc.NewDashboardOverviewService(instanceConn, projectRepo)
	dashMetricsSvc := postgressvc.NewDashboardMetricsService(instanceConn)
	dashboardHandler := pghandler.NewDashboardHandler(dashOverviewSvc, dashMetricsSvc)
	postgresHandler := pghandler.NewPostgresHandler(tableHandler, schemaHandler, pgQueryHandler, dashboardHandler, textToSqlHandler)

	// MongoDB collection management
	mongoConn := mongoinfra.NewMongoConnectionManager(dsnService)
	mongoInstanceManager = mongoConn
	mongoColRepo := mongorepo.NewCollectionRepository()
	mongoColService := mongosvc.NewCollectionService(mongoConn, mongoColRepo)
	mongoColHandler := mongohandler.NewCollectionHandler(mongoColService)
	mongoHandler := mongohandler.NewMongoHandler(mongoColHandler)

	// Backup (export/import) handler — dispatches by project.DBType internally.
	backupService := backup.NewService(projectRepo, dsnService)
	backupHandler := backup.NewHandler(backupService)

	// Initialize Gin router (custom logger skips /health to avoid health-check log noise)
	router := gin.New()
	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Skip: func(c *gin.Context) bool { return c.Request.URL.Path == "/health" },
	}))
	router.Use(gin.Recovery())

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	// Register all routes
	routes.RegisterRoutes(router, authHandler, googleAuthHandler, userHandler, userRepo, projectHandler, projectRepo, postgresHandler, mongoHandler, backupHandler)
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           router,
		IdleTimeout:       time.Minute,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0, // handlers manage their own write deadline for long downloads
	}

	return server
}

// CloseResources releases long-lived pools owned by the server package.
func CloseResources() {
	if pgInstanceManager != nil {
		pgInstanceManager.CloseAll()
	}
	if mongoInstanceManager != nil {
		mongoInstanceManager.CloseAll()
	}
	database.Close()
}

func validateRequiredEnvVars() error {
	required := map[string]string{
		"PORT":                 os.Getenv("PORT"),
		"DB_HOST":              os.Getenv("DB_HOST"),
		"DB_PORT":              os.Getenv("DB_PORT"),
		"DB_USERNAME":          os.Getenv("DB_USERNAME"),
		"DB_PASSWORD":          os.Getenv("DB_PASSWORD"),
		"DB_DATABASE":          os.Getenv("DB_DATABASE"),
		"REDIS_ADDR":           os.Getenv("REDIS_ADDR"),
		"ACCESS_TOKEN_SECRET":  os.Getenv("ACCESS_TOKEN_SECRET"),
		"REFRESH_TOKEN_SECRET": os.Getenv("REFRESH_TOKEN_SECRET"),
		"GOOGLE_CLIENT_ID":     os.Getenv("GOOGLE_CLIENT_ID"),
		"GOOGLE_CLIENT_SECRET": os.Getenv("GOOGLE_CLIENT_SECRET"),
		"GOOGLE_REDIRECT_URL":  os.Getenv("GOOGLE_REDIRECT_URL"),
	}

	for name, value := range required {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	// K8s provisioner: DB_INSTANCES_NAMESPACE_POSTGRES, DB_INSTANCES_NAMESPACE_MONGO (optional), KUBECONFIG (optional)
	return nil
}
