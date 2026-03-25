package main

import (
	"fmt"
	"log"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"go.uber.org/zap"

	"parily.dev/app/internal/auth"
	"parily.dev/app/internal/config"
	"parily.dev/app/internal/health"
	"parily.dev/app/internal/logger"
	"parily.dev/app/internal/middleware"
	mongoClient "parily.dev/app/internal/mongo"
	"parily.dev/app/internal/postgres"
	"parily.dev/app/internal/redis"
	"parily.dev/app/internal/rooms"
	wshandler "parily.dev/app/internal/websocket"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := logger.Init(cfg.Environment); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	logger.Log.Info("Parily server starting",
		zap.String("port", cfg.ServerPort),
		zap.String("environment", cfg.Environment),
	)

	pgPool, err := postgres.Connect(cfg)
	if err != nil {
		logger.Log.Fatal("Failed to connect to PostgreSQL", zap.Error(err))
	}
	defer pgPool.Close()
	logger.Log.Info("PostgreSQL connected")

	mongoDB, err := mongoClient.Connect(cfg)
	if err != nil {
		logger.Log.Fatal("Failed to connect to MongoDB", zap.Error(err))
	}
	logger.Log.Info("MongoDB connected", zap.String("db", mongoDB.Name()))

	redisClient, err := redis.Connect(cfg)
	if err != nil {
		logger.Log.Fatal("Failed to connect to Redis", zap.Error(err))
	}
	defer redisClient.Close()
	logger.Log.Info("Redis connected")

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.PostgresUser,
		cfg.PostgresPassword,
		cfg.PostgresHost,
		cfg.PostgresPort,
		cfg.PostgresDB,
	)

	m, err := migrate.New("file:///app/migrations", dsn)
	if err != nil {
		logger.Log.Fatal("Failed to initialize migrations", zap.Error(err))
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		logger.Log.Fatal("Failed to run migrations", zap.Error(err))
	} else if err == migrate.ErrNoChange {
		logger.Log.Info("Migrations already up to date")
	} else {
		logger.Log.Info("Migrations applied successfully")
	}

	// ── Hubs ──────────────────────────────────────────────────────────────────
	hub := wshandler.NewHub(redisClient, logger.Log)
	permissionsHub := wshandler.NewPermissionsHub(redisClient, logger.Log)

	// ── Handlers ──────────────────────────────────────────────────────────────
	wsHandler := wshandler.NewHandler(hub, pgPool, cfg, logger.Log)
	permissionsHandler := wshandler.NewPermissionsHandler(permissionsHub, pgPool, cfg, logger.Log)
	authHandler := auth.NewHandler(pgPool, cfg, logger.Log)
	roomsHandler := rooms.NewHandler(pgPool, mongoDB, redisClient)

	// ── Router ────────────────────────────────────────────────────────────────
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
	}))

	// Health
	r.GET("/health/live", health.Live)
	r.GET("/health/ready", health.Ready(pgPool, mongoDB, redisClient))

	// Auth public
	authHandler.RegisterRoutes(r.Group("/auth"))

	// Auth protected
	authProtected := r.Group("/auth")
	authProtected.Use(middleware.RequireAuth(cfg.JWTSecret))
	authProtected.GET("/me", authHandler.Me)

	// API protected
	api := r.Group("/api")
	api.Use(middleware.RequireAuth(cfg.JWTSecret))
	roomsHandler.RegisterRoutes(api.Group("/rooms"))

	// WebSocket — Yjs sync
	r.GET("/ws/:roomId/:fileId", wsHandler.ServeWS)
	// WebSocket — permissions
	r.GET("/ws/:roomId/permissions", permissionsHandler.ServePermissions)
	logger.Log.Info("Server listening", zap.String("port", cfg.ServerPort))
	if err := r.Run(":" + cfg.ServerPort); err != nil {
		logger.Log.Fatal("Server failed", zap.Error(err))
	}
}
