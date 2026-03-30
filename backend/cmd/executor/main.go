package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/mongo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"parily.dev/app/internal/config"
	executor "parily.dev/app/internal/executor"
	"parily.dev/app/internal/kafka"
	mongoClient "parily.dev/app/internal/mongo"
	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
	"parily.dev/app/internal/redis"
	pb "parily.dev/app/proto"
)

type executorServer struct {
	pb.UnimplementedExecutorServiceServer
	db        *pgxpool.Pool
	mongoDB   *mongo.Database
	dockerCli *dockerclient.Client
	rdb       *redis.Client
	kafka *kafka.Producer
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

	// try to acquire lock — 30s TTL matches execution timeout
	acquired, err := s.rdb.SetNX(lockKey(req.RoomId, req.FileId), "1", 30)
	if err != nil {
		return nil, fmt.Errorf("lock check failed: %w", err)
	}
	if !acquired {
		log.Printf("[main] lock held for room=%s file=%s", req.RoomId, req.FileId)
		return nil, status.Error(codes.ResourceExhausted, "already running")
	}

	log.Printf("[main] lock acquired for room=%s file=%s", req.RoomId, req.FileId)

	// return immediately — do work in background
	go s.runExecution(req.RoomId, req.FileId, req.ExecutionId)

	return &pb.ExecuteResponse{}, nil
}

func (s *executorServer) runExecution(roomID, fileID, executionID string) {
	ctx := context.Background()
	log.Printf("[main] starting execution room=%s file=%s", roomID, fileID)

	defer func() {
		// always release lock after publishing done
		if err := s.rdb.Del(lockKey(roomID, fileID)); err != nil {
			log.Printf("[main] failed to delete lock: %v", err)
		}
		log.Printf("[main] lock released room=%s file=%s", roomID, fileID)
	}()

	// reconstruct file tree
	tempDir, entryPath, language, err := executor.ReconstructFileTree(ctx, s.db, s.mongoDB, roomID, executionID, fileID)
	if err != nil {
		log.Printf("[main] failed to reconstruct file tree: %v", err)
		s.publishError(roomID, fileID, "internal_error")
		return
	}
	defer os.RemoveAll(tempDir)

	// run container
	result, err := executor.RunContainer(s.dockerCli, tempDir, entryPath, language)
	if err != nil {
		log.Printf("[main] failed to run container: %v", err)
		s.publishError(roomID, fileID, "internal_error")
		return
	}

	log.Printf("[main] execution done exit_code=%d duration=%dms", result.ExitCode, result.DurationMs)

	// save to MongoDB
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
    if err := s.kafka.PublishExecutionEvent(kctx, kafka.ExecutionEvent{
        ExecutionID: executionID,
        RoomID:      roomID,
        FileID:      fileID,
        Language:    language,
        Output:      result.Output,
        ExitCode:    result.ExitCode,
        DurationMs:  result.DurationMs,
        ExecutedAt:  time.Now().UTC(),
    }); err != nil {
        log.Printf("[main] failed to publish execution event to kafka: %v", err)
    } else {
        log.Printf("[main] execution event published to kafka file=%s", fileID)
    }
	}()

	// publish execution_done to Redis — RoomHub delivers to all clients
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
	}
	log.Printf("[main] published execution_done to %s", roomChannel(roomID))
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

func pullImages(ctx context.Context, cli *dockerclient.Client) {
	images := []string{
		"python:3.11-alpine",
		"node:20-alpine",
		"golang:1.21-alpine",
		"eclipse-temurin:21-alpine",
	}
	for _, img := range images {
		log.Printf("[startup] pulling image %s...", img)
		reader, err := cli.ImagePull(ctx, img, image.PullOptions{})
		if err != nil {
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

	pgPool, err := pg.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pgPool.Close()
	log.Println("postgres connected")

	mongoDB, err := mongoClient.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to mongodb: %v", err)
	}
	log.Println("mongodb connected")

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

	pullImages(context.Background(), cli)
	log.Println("all images ready")

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterExecutorServiceServer(s, &executorServer{
		db:        pgPool,
		mongoDB:   mongoDB,
		dockerCli: cli,
		rdb:       redisClient,
		kafka:     kafkaProducer,
	})

	log.Println("executor gRPC server listening on :50051")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}