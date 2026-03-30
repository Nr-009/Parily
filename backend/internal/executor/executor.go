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

func RunContainer(cli *dockerclient.Client, tempDir, entryPath, language string) (*RunResult, error) {
	cfg, ok := langConfigs[language]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", language)
	}
	log.Printf("[executor] running language=%s image=%s entry=%s", language, cfg.Image, entryPath)

	cmd := append(cfg.Command, entryPath)
	log.Printf("[executor] command: %v", cmd)

	ctx := context.Background()
	start := time.Now()

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
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	containerID := resp.ID
	log.Printf("[executor] container created: %s image=%s cmd=%v", containerID[:12], cfg.Image, cmd)

	if err := cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}
	log.Printf("[executor] container started: %s", containerID[:12])

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
		}
	case status := <-statusCh:
		exitCode = int(status.StatusCode)
		log.Printf("[executor] container finished exit_code=%d", exitCode)
	case <-timeoutCtx.Done():
		exitCode = -1
		log.Printf("[executor] container timed out after 30s, killing")
		_ = cli.ContainerKill(ctx, containerID, "SIGKILL")
	}

	durationMs := time.Since(start).Milliseconds()
	log.Printf("[executor] duration=%dms", durationMs)

	logs, err := cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
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