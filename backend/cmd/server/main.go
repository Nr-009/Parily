package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"go.uber.org/zap"

	"github.com/gin-contrib/cors"
	"parily.dev/app/internal/auth"
	"parily.dev/app/internal/config"
	"parily.dev/app/internal/health"
	"parily.dev/app/internal/logger"
	"parily.dev/app/internal/mongo"
	"parily.dev/app/internal/postgres"
	"parily.dev/app/internal/redis"
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

	mongoDB, err := mongo.Connect(cfg)
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

	// ── Handlers ──────────────────────────────────────────────────────────────
	hub := wshandler.NewHub(redisClient)
	wsHandler := wshandler.NewHandler(hub)
	authHandler := auth.NewHandler(pgPool, cfg)

	// ── Router ────────────────────────────────────────────────────────────────
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
	}))

	// Health
	r.GET("/health/live", health.Live)
	r.GET("/health/ready", health.Ready(pgPool, mongoDB, redisClient))

	// Auth public routes
	authHandler.RegisterRoutes(r.Group("/auth"))

	// GET /auth/me — inline auth check until middleware is built in 2.4
	r.GET("/auth/me", func(c *gin.Context) {
		claims, err := auth.ParseToken(c, cfg.JWTSecret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set("userID", claims.UserID)
		authHandler.Me(c)
	})

	// WebSocket
	r.GET("/ws/:room", wsHandler.ServeWS)

	// ── Start ─────────────────────────────────────────────────────────────────
	logger.Log.Info("Server listening", zap.String("port", cfg.ServerPort))
	if err := r.Run(":" + cfg.ServerPort); err != nil {
		logger.Log.Fatal("Server failed", zap.Error(err))
	}
}
