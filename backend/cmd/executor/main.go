package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.mongodb.org/mongo-driver/mongo"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"parily.dev/app/internal/config"
	executor "parily.dev/app/internal/executor"
	"parily.dev/app/internal/kafka"
	"parily.dev/app/internal/metrics"
	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
	"parily.dev/app/internal/redis"
	"parily.dev/app/internal/tracing"
	pb "parily.dev/app/proto"
)

type executorServer struct {
	pb.UnimplementedExecutorServiceServer
	db        *pgxpool.Pool
	mongoDB   *mongo.Database
	dockerCli *dockerclient.Client
	rdb       *redis.Client
	kafka     *kafka.Producer

	// wg tracks in-flight runExecution goroutines.
	// Nobody waits on this during normal operation — it is only
	// blocked on during shutdown to ensure we never close Redis,
	// Kafka, or Postgres while a goroutine is still writing to them.
	wg sync.WaitGroup
}

func lockKey(roomID, fileID string) string {
	return fmt.Sprintf("exec:lock:%s:%s", roomID, fileID)
}

func roomChannel(roomID string) string {
	return fmt.Sprintf("room:%s:room", roomID)
}

func (s *executorServer) Execute(ctx context.Context, req *pb.ExecuteRequest) (*pb.ExecuteResponse, error) {
	log.Printf("Execute called: execution_id=%s room_id=%s file_id=%s",
		req.ExecutionId, req.RoomId, req.FileId)

	acquired, err := s.rdb.SetNX(lockKey(req.RoomId, req.FileId), "1", 30)
	if err != nil {
		return nil, fmt.Errorf("lock check failed: %w", err)
	}
	if !acquired {
		log.Printf("[main] lock held for room=%s file=%s", req.RoomId, req.FileId)
		return nil, status.Error(codes.ResourceExhausted, "already running")
	}

	log.Printf("[main] lock acquired for room=%s file=%s", req.RoomId, req.FileId)

	spanCtx := oteltrace.SpanFromContext(ctx).SpanContext()
	execCtx := oteltrace.ContextWithSpanContext(context.Background(), spanCtx)

	// wg.Add(1) BEFORE launching the goroutine — if we did it inside
	// the goroutine there would be a race where shutdown calls wg.Wait()
	// before the goroutine even registers itself.
	s.wg.Add(1)
	go s.runExecution(execCtx, req.RoomId, req.FileId, req.ExecutionId)

	return &pb.ExecuteResponse{}, nil
}

func (s *executorServer) runExecution(ctx context.Context, roomID, fileID, executionID string) {
	// wg.Done() signals shutdown that this goroutine has finished.
	// During normal operation nobody is waiting on wg — this is free.
	// Only during shutdown does wg.Wait() block until this returns.
	defer s.wg.Done()

	log.Printf("[main] starting execution room=%s file=%s", roomID, fileID)

	tracer := otel.Tracer("pairly")
	ctx, span := tracer.Start(ctx, "runExecution",
		oteltrace.WithAttributes(
			attribute.String("room.id", roomID),
			attribute.String("file.id", fileID),
			attribute.String("execution.id", executionID),
		),
	)
	defer span.End()

	defer func() {
		if err := s.rdb.Del(lockKey(roomID, fileID)); err != nil {
			log.Printf("[main] failed to delete lock: %v", err)
		}
		log.Printf("[main] lock released room=%s file=%s", roomID, fileID)
	}()

	tempDir, entryPath, language, err := executor.ReconstructFileTree(ctx, s.db, s.mongoDB, roomID, executionID, fileID)
	if err != nil {
		log.Printf("[main] failed to reconstruct file tree: %v", err)
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		s.publishError(roomID, fileID, "internal_error")
		return
	}
	defer os.RemoveAll(tempDir)

	result, err := executor.RunContainer(ctx, s.dockerCli, tempDir, entryPath, language)
	if err != nil {
		log.Printf("[main] failed to run container: %v", err)
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		s.publishError(roomID, fileID, "internal_error")
		return
	}

	log.Printf("[main] execution done exit_code=%d duration=%dms", result.ExitCode, result.DurationMs)
	metrics.ExecutionsTotal.WithLabelValues(language).Inc()
	metrics.ExecutionDuration.Observe(float64(result.DurationMs) / 1000)

	span.SetAttributes(
		attribute.String("language", language),
		attribute.Int("exit.code", result.ExitCode),
		attribute.Int64("duration.ms", result.DurationMs),
	)

	event := map[string]any{
		"type":        "execution_done",
		"file_id":     fileID,
		"room_id":     roomID,
		"exit_code":   result.ExitCode,
		"duration_ms": result.DurationMs,
		"output":      result.Output,
		"truncated":   len(result.Output) >= 50*1024,
	}
	data, _ := json.Marshal(event)
	if err := s.rdb.Publish(roomChannel(roomID), data); err != nil {
		log.Printf("[main] failed to publish execution_done: %v", err)
		metrics.RedisPublishErrorsTotal.Inc()
	}
	log.Printf("[main] published execution_done to %s", roomChannel(roomID))

	execRepo := mongoRepo.NewExecutionRepository(s.mongoDB)
	if err := execRepo.SaveExecution(ctx, mongoRepo.ExecutionResult{
		ExecutionID: executionID,
		RoomID:      roomID,
		FileID:      fileID,
		Output:      result.Output,
		ExitCode:    result.ExitCode,
		DurationMs:  result.DurationMs,
		Truncated:   len(result.Output) >= 50*1024,
		ExecutedAt:  time.Now(),
	}); err != nil {
		log.Printf("[main] failed to save execution: %v", err)
	}

	go func() {
		kctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		kafkaEvent := kafka.ExecutionEvent{
			ExecutionID: executionID,
			RoomID:      roomID,
			FileID:      fileID,
			Language:    language,
			Output:      result.Output,
			ExitCode:    result.ExitCode,
			DurationMs:  result.DurationMs,
			ExecutedAt:  time.Now().UTC(),
		}
		if err := s.kafka.PublishExecutionEvent(kctx, kafkaEvent); err != nil {
			log.Printf("[main] failed to publish execution event to kafka: %v", err)
			payload, _ := json.Marshal(kafkaEvent)
			_ = s.kafka.PublishDeadLetter(kctx, "execution-events", payload, err.Error())
			metrics.KafkaPublishErrorsTotal.WithLabelValues("execution-events").Inc()
		} else {
			log.Printf("[main] execution event published to kafka file=%s", fileID)
		}
	}()
}

func (s *executorServer) publishError(roomID, fileID, reason string) {
	event := map[string]string{
		"type":    "execution_error",
		"file_id": fileID,
		"reason":  reason,
	}
	data, _ := json.Marshal(event)
	if err := s.rdb.Publish(roomChannel(roomID), data); err != nil {
		log.Printf("[main] failed to publish execution_error: %v", err)
	}
}

// pullImages takes the signal context so SIGTERM during startup aborts
// immediately. Safe because the gRPC server has not started yet —
// no connections exist, no work is in progress.
// On subsequent restarts Docker serves from host cache so this is instant.
func pullImages(ctx context.Context, cli *dockerclient.Client) {
	images := []string{
		"python:3.11-alpine",
		"node:20-alpine",
		"golang:1.21-alpine",
		"eclipse-temurin:21-alpine",
	}
	for _, img := range images {
		// check signal before each pull — fast exit if SIGTERM already arrived
		select {
		case <-ctx.Done():
			log.Println("[startup] shutdown signal received — aborting image pull")
			return
		default:
		}

		log.Printf("[startup] pulling image %s...", img)
		reader, err := cli.ImagePull(ctx, img, image.PullOptions{})
		if err != nil {
			if ctx.Err() != nil {
				log.Println("[startup] shutdown signal received during pull — aborting")
				return
			}
			log.Printf("[startup] failed to pull %s: %v", img, err)
			continue
		}
		io.Copy(io.Discard, reader)
		reader.Close()
		log.Printf("[startup] pulled %s", img)
	}
}

func main() {
	cfg, err := config.LoadExecutor()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	metrics.InitExecutor()

	// OTel deferred first so it runs last — flushes spans from shutdown itself.
	tracingShutdown, err := tracing.Init("pairly-executor", cfg.JaegerEndpoint)
	if err != nil {
		log.Fatalf("failed to init tracing: %v", err)
	}
	defer tracingShutdown()

	pgPool, err := pg.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pgPool.Close()
	log.Println("postgres connected")

	mongoDB, err := mongoRepo.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to mongodb: %v", err)
	}
	log.Println("mongodb connected")
	defer mongoDB.Client().Disconnect(context.Background())

	redisClient, err := redis.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}
	defer redisClient.Close()
	log.Println("redis connected")

	kafkaProducer := kafka.NewProducer(cfg.KafkaBroker)
	defer kafkaProducer.Close()
	log.Printf("kafka producer connected broker=%s", cfg.KafkaBroker)

	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("failed to create docker client: %v", err)
	}
	defer cli.Close()
	log.Println("docker client connected")

	// Signal context — passed to pullImages so startup aborts on SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Metrics HTTP server with its own shutdown path.
	metricsSrv := &http.Server{
		Addr:    ":" + cfg.MetricsPort,
		Handler: promhttp.Handler(),
	}
	go func() {
		log.Printf("executor metrics server listening on :%s", cfg.MetricsPort)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[main] metrics server error: %v", err)
		}
	}()

	// pullImages respects ctx — if SIGTERM arrives mid-pull we exit here
	// before the gRPC server ever starts. No connections, no work, clean exit.
	pullImages(ctx, cli)
	if ctx.Err() != nil {
		log.Println("shutdown signal received before gRPC server started — exiting")
		return
	}
	log.Println("all images ready")

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv := &executorServer{
		db:        pgPool,
		mongoDB:   mongoDB,
		dockerCli: cli,
		rdb:       redisClient,
		kafka:     kafkaProducer,
	}

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	pb.RegisterExecutorServiceServer(grpcServer, srv)

	go func() {
		log.Println("executor gRPC server listening on :50051")
		if err := grpcServer.Serve(lis); err != nil {
			log.Printf("[main] gRPC server stopped: %v", err)
		}
	}()

	// Block until SIGTERM or SIGINT.
	<-ctx.Done()
	log.Println("shutdown signal received")

	log.Println("stopping gRPC server...")
	grpcServer.GracefulStop()
	log.Println("gRPC server stopped")

	waitCh := make(chan struct{})
	go func() {
		srv.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		log.Println("all in-flight executions finished")
	case <-time.After(30 * time.Second):
		log.Println("shutdown timeout — proceeding with executions still running")
	}

	metricsCtx, metricsCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer metricsCancel()
	if err := metricsSrv.Shutdown(metricsCtx); err != nil {
		log.Printf("[main] metrics server shutdown error: %v", err)
	}

	log.Println("executor shutdown complete")
}