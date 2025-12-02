package server

import (
    "context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/gin-gonic/gin"
	_ "github.com/joho/godotenv/autoload"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"my_project/internal/handlers"
	"my_project/internal/repositories"
	"my_project/internal/routes"
	"my_project/internal/services"
)

type Server struct {
	port int
	db   *gorm.DB
}

func NewServer() *http.Server {
	port, _ := strconv.Atoi(os.Getenv("PORT"))

	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=UTC",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USERNAME"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_DATABASE"),
		os.Getenv("DB_PORT"),
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// Test Redis connection and fail fast with a clear message
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := rdb.Ping(ctx).Err(); err != nil {
			log.Fatalf("failed to connect to Redis at %s:%s: %v", os.Getenv("REDIS_HOST"), os.Getenv("REDIS_PORT"), err)
		}
		log.Println("Connected to Redis successfully")
	}

	// Execute script.sql
	sqlBytes, err := os.ReadFile("database/script.sql")
	if err != nil {
		log.Fatalf("failed to read SQL file: %v", err)
	}
	if err := db.Exec(string(sqlBytes)).Error; err != nil {
		log.Fatalf("failed to execute SQL file: %v", err)
	}

	s := &Server{
		port: port,
		db:   db,
	}

	// Dependency injection
	userRepo := repositories.NewUserRepository(db)
	// sessionRepo := repositories.NewSessionRepository(db)
	redisRepo := repositories.NewRedisRepository(rdb)
	userService := services.NewUserService(userRepo, redisRepo)
	authHandler := handlers.NewAuthHandler(userService)
	userHandler := handlers.NewUserHandler(userService)

	// Query service dependencies (project-bound connections execution)
	projectRepo := repositories.NewProjectsRepository(db)
	instanceRepo := repositories.NewDatabaseInstanceRepository(db)
	credRepo := repositories.NewDatabaseCredentialRepository(db)
	execRepo := repositories.NewQueryHistoryRepository(db)
	queryService := services.NewQueryService(projectRepo, instanceRepo, credRepo, execRepo, db)
	queryHandler := handlers.NewQueryHandler(queryService)

	// Initialize Gin router
	router := gin.Default()
	// routes.RegisterRoutes(router, authHandler, userHandler) // register all routes
	routes.RegisterRoutes(router, authHandler, userHandler, queryHandler) // register all routes

	// Create and configure the HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      router,
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return server
}
