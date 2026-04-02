package executor

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-units"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"parily.dev/app/internal/metrics"
)

type LangConfig struct {
	Image   string
	Command []string
}

var langConfigs = map[string]LangConfig{
	"python":     {Image: "python:3.11-alpine", Command: []string{"python3"}},
	"javascript": {Image: "node:20-alpine", Command: []string{"node"}},
	"typescript": {Image: "node:20-alpine", Command: []string{"npx", "ts-node"}},
	"go":         {Image: "golang:1.21-alpine", Command: []string{"go", "run"}},
	"java":       {Image: "eclipse-temurin:21-alpine", Command: []string{"java"}},
}

type RunResult struct {
	Output     string
	ExitCode   int
	DurationMs int64
}

// RunContainer now accepts ctx so it can attach spans to the parent trace
// from runExecution. This is what makes the Docker timing visible in Jaeger.
func RunContainer(ctx context.Context, cli *dockerclient.Client, tempDir, entryPath, language string) (*RunResult, error) {
	cfg, ok := langConfigs[language]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	// parent span for the entire container lifecycle
	tracer := otel.Tracer("pairly")
	ctx, span := tracer.Start(ctx, "RunContainer",
		oteltrace.WithAttributes(
			attribute.String("language", language),
			attribute.String("image", cfg.Image),
			attribute.String("entry", entryPath),
		),
	)
	defer span.End()

	log.Printf("[executor] running language=%s image=%s entry=%s", language, cfg.Image, entryPath)

	cmd := append(cfg.Command, entryPath)
	log.Printf("[executor] command: %v", cmd)

	start := time.Now()

	// child span for container creation — measures Docker image + container setup time
	_, createSpan := tracer.Start(ctx, "ContainerCreate")
	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image:      cfg.Image,
		Cmd:        cmd,
		WorkingDir: "/workspace",
		User:       "nobody",
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeBind,
				Source:   tempDir,
				Target:   "/workspace",
				ReadOnly: true,
			},
		},
		NetworkMode:    "none",
		ReadonlyRootfs: true,
		SecurityOpt:    []string{"no-new-privileges"},
		Resources: container.Resources{
			Memory:    256 * 1024 * 1024,
			NanoCPUs:  500000000,
			PidsLimit: int64Ptr(64),
			Ulimits: []*units.Ulimit{
				{Name: "nofile", Soft: 64, Hard: 64},
			},
		},
		AutoRemove: true,
	}, nil, nil, "")
	createSpan.End()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		return nil, fmt.Errorf("create container: %w", err)
	}

	containerID := resp.ID
	log.Printf("[executor] container created: %s image=%s cmd=%v", containerID[:12], cfg.Image, cmd)

	if err := cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		return nil, fmt.Errorf("start container: %w", err)
	}
	log.Printf("[executor] container started: %s", containerID[:12])

	// child span for container execution — this is where user code actually runs
	// this span shows exactly how long the user's code took to execute
	_, execSpan := tracer.Start(ctx, "ContainerExec",
		oteltrace.WithAttributes(
			attribute.String("container.id", containerID[:12]),
		),
	)

	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	statusCh, errCh := cli.ContainerWait(timeoutCtx, containerID, container.WaitConditionNotRunning)
	log.Printf("[executor] waiting for container to finish...")

	var exitCode int

	select {
	case err := <-errCh:
		if err != nil {
			exitCode = -1
			log.Printf("[executor] container wait error: %v", err)
			execSpan.RecordError(err)
			execSpan.SetStatus(otelcodes.Error, err.Error())
		}
	case status := <-statusCh:
		exitCode = int(status.StatusCode)
		log.Printf("[executor] container finished exit_code=%d", exitCode)
		execSpan.SetAttributes(attribute.Int("exit.code", exitCode))
	case <-timeoutCtx.Done():
		exitCode = -1
		log.Printf("[executor] container timed out after 30s, killing")
		metrics.ExecutionTimeoutTotal.Inc()
		execSpan.SetStatus(otelcodes.Error, "execution timed out after 30s")
		_ = cli.ContainerKill(ctx, containerID, "SIGKILL")
	}
	execSpan.End()

	durationMs := time.Since(start).Milliseconds()
	log.Printf("[executor] duration=%dms", durationMs)

	// add final attributes to parent span
	span.SetAttributes(
		attribute.Int("exit.code", exitCode),
		attribute.Int64("duration.ms", durationMs),
	)

	logs, err := cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		return nil, fmt.Errorf("collect logs: %w", err)
	}
	defer logs.Close()

	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, logs); err != nil {
		return nil, fmt.Errorf("read logs: %w", err)
	}

	output := buf.String()
	log.Printf("[executor] raw output: %q", output)

	truncated := false
	if len(output) > 50*1024 {
		output = output[:50*1024]
		truncated = true
		log.Printf("[executor] output truncated at 50kb")
		metrics.ExecutionOutputTruncatedTotal.Inc()
	}

	if exitCode == -1 {
		output += "\n[Process killed: execution exceeded 30 second time limit]"
	}

	log.Printf("[executor] done exit_code=%d duration=%dms truncated=%v output_len=%d",
		exitCode, durationMs, truncated, len(output))

	return &RunResult{
		Output:     output,
		ExitCode:   exitCode,
		DurationMs: durationMs,
	}, nil
}

func int64Ptr(i int64) *int64 { return &i }