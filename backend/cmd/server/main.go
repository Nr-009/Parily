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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"parily.dev/app/internal/auth"
	"parily.dev/app/internal/config"
	"parily.dev/app/internal/health"
	"parily.dev/app/internal/kafka"
	"parily.dev/app/internal/logger"
	"parily.dev/app/internal/middleware"
	mongoClient "parily.dev/app/internal/mongo"
	"parily.dev/app/internal/postgres"
	"parily.dev/app/internal/redis"
	"parily.dev/app/internal/rooms"
	wshandler "parily.dev/app/internal/websocket"
	pb "parily.dev/app/proto"
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

	kafkaProducer := kafka.NewProducer(cfg.KafkaBroker)
	defer kafkaProducer.Close()
	logger.Log.Info("Kafka producer connected", zap.String("broker", cfg.KafkaBroker))


	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.PostgresUser,
		cfg.PostgresPassword,
		cfg.PostgresHost,
		cfg.PostgresPort,
		cfg.PostgresDB,
	)

	grpcConn, err := grpc.NewClient(
		"executor:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		logger.Log.Fatal("failed to connect to executor", zap.Error(err))
	}
	defer grpcConn.Close()
	executorClient := pb.NewExecutorServiceClient(grpcConn)
	logger.Log.Info("executor gRPC client connected")

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
	roomHub := wshandler.NewRoomHub(redisClient, logger.Log)
	notifyHub := wshandler.NewNotifyHub(redisClient, logger.Log)

	// ── Handlers ──────────────────────────────────────────────────────────────
	wsHandler := wshandler.NewHandler(hub, pgPool, cfg, logger.Log)
	roomHandler := wshandler.NewRoomHandler(roomHub, pgPool, cfg, logger.Log, executorClient)
	notifyHandler := wshandler.NewNotifyHandler(notifyHub, pgPool, cfg, logger.Log)
	authHandler := auth.NewHandler(pgPool, cfg, logger.Log)
	roomsHandler := rooms.NewHandler(pgPool, mongoDB, redisClient, notifyHub, kafkaProducer, cfg.KafkaBroker)

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

	// WebSocket — Yjs sync per file
	r.GET("/ws/:roomId/:fileId", wsHandler.ServeWS)
	// WebSocket — room channel (permissions + presence)
	r.GET("/room-ws/:roomId", roomHandler.ServeRoom)
	// WebSocket — user notification channel (dashboard only)
	r.GET("/notify-ws", notifyHandler.ServeNotify)

	logger.Log.Info("Server listening", zap.String("port", cfg.ServerPort))
	if err := r.Run(":" + cfg.ServerPort); err != nil {
		logger.Log.Fatal("Server failed", zap.Error(err))
	}
}
