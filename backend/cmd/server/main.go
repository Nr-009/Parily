package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"parily.dev/app/internal/auth"
	"parily.dev/app/internal/config"
	"parily.dev/app/internal/health"
	"parily.dev/app/internal/kafka"
	"parily.dev/app/internal/logger"
	"parily.dev/app/internal/metrics"
	"parily.dev/app/internal/middleware"
	mongoClient "parily.dev/app/internal/mongo"
	"parily.dev/app/internal/postgres"
	"parily.dev/app/internal/redis"
	"parily.dev/app/internal/rooms"
	"parily.dev/app/internal/tracing"
	wshandler "parily.dev/app/internal/websocket"
	pb "parily.dev/app/proto"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	metrics.InitServer()

	// OTel shutdown must be deferred FIRST so it runs LAST.
	// In Go, defers execute LIFO — innermost defer = last to run.
	// We want OTel to flush after everything else closes so spans
	// emitted during shutdown (e.g. hub drain) are not lost.
	tracingShutdown, err := tracing.Init("pairly-backend", cfg.JaegerEndpoint)
	if err != nil {
		logger.Log.Fatal("failed to init tracing", zap.Error(err))
	}
	defer tracingShutdown()

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
	defer mongoDB.Client().Disconnect(context.Background())

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
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
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
	r.Use(middleware.PrometheusMiddleware())
	r.Use(otelgin.Middleware("pairly-backend"))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://127.0.0.1:5173",},
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

	// WebSocket routes
	r.GET("/ws/:roomId/:fileId", wsHandler.ServeWS)
	r.GET("/room-ws/:roomId", roomHandler.ServeRoom)
	r.GET("/notify-ws", notifyHandler.ServeNotify)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// ── HTTP server ───────────────────────────────────────────────────────────
	// We wrap Gin in a net/http.Server so we can call Shutdown() on signal.
	// r.Run() has no shutdown hook — it blocks forever with no way out.
	srv := &http.Server{
		Addr:    ":" + cfg.ServerPort,
		Handler: r,
	}

	// signal.NotifyContext returns a context that cancels on SIGTERM or SIGINT.
	// This is cleaner than manual signal.Notify + channel — the context
	// propagates naturally to anything that accepts a ctx.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Start HTTP server in a goroutine so main can block on the signal context.
	// ErrServerClosed is expected after Shutdown() — not a real error.
	go func() {
		logger.Log.Info("Server listening", zap.String("port", cfg.ServerPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log.Fatal("Server failed", zap.Error(err))
		}
	}()

	// Block here until SIGTERM or SIGINT arrives.
	<-ctx.Done()
	logger.Log.Info("Shutdown signal received")

	// ── Graceful shutdown sequence ────────────────────────────────────────────
	// Order matters:
	// 1. Stop accepting new HTTP/WS connections (Shutdown with timeout)
	// 2. Drain the three WebSocket hubs — close all active connections cleanly
	// 3. Everything else (Postgres, Redis, Kafka, gRPC) closes via defer above
	//
	// Kubernetes sends SIGTERM then waits terminationGracePeriodSeconds (default 30s)
	// before sending SIGKILL. We give ourselves 25s to drain — 5s buffer.

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// Stop accepting new connections. In-flight HTTP requests get up to
	// shutdownCtx deadline to complete. WebSocket upgrades already in progress
	// are allowed to finish their upgrade handshake.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Log.Error("HTTP server shutdown error", zap.Error(err))
	}
	logger.Log.Info("HTTP server stopped accepting connections")

	hub.Shutdown()
	roomHub.Shutdown()
	notifyHub.Shutdown()
	logger.Log.Info("WebSocket hubs drained")
	logger.Log.Info("Server shutdown complete")
}