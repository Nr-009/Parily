package executor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/mongo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
)

type TreeNode struct {
	File     pg.File
	Children []*TreeNode
}

func ReconstructFileTree(
	ctx context.Context,
	db *pgxpool.Pool,
	mongoDB *mongo.Database,
	roomID string,
	executionID string,
	fileID string,
) (string, string, string, error) {

	// parent span for the entire file tree reconstruction
	// child spans show exactly where time is spent — Postgres vs Yjs decode
	tracer := otel.Tracer("pairly")
	ctx, span := tracer.Start(ctx, "ReconstructFileTree",
		oteltrace.WithAttributes(
			attribute.String("room.id", roomID),
			attribute.String("file.id", fileID),
			attribute.String("execution.id", executionID),
		),
	)
	defer span.End()

	// child span — Postgres fetch
	_, fetchSpan := tracer.Start(ctx, "FetchFilesFromPostgres")
	allFiles, err := pg.GetFilesForRoom(ctx, db, roomID)
	fetchSpan.End()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		return "", "", "", fmt.Errorf("fetch files: %w", err)
	}

	var files []pg.File
	var entryLanguage string
	for _, f := range allFiles {
		if f.ID == fileID {
			entryLanguage = f.Language
		}
		if f.IsActive {
			files = append(files, f)
		}
	}
	log.Printf("[builder] fetched %d total files, %d active, entry language=%s", len(allFiles), len(files), entryLanguage)

	span.SetAttributes(
		attribute.Int("files.total", len(allFiles)),
		attribute.Int("files.active", len(files)),
		attribute.String("language", entryLanguage),
	)

	var (
		contentMap sync.Map
		wg         sync.WaitGroup
		fetchErr   error
		fetchMu    sync.Mutex
	)

	docRepo := mongoRepo.NewDocumentRepository(mongoDB)

	// child span — concurrent Yjs decode across all files
	// duration shows total wall time for the goroutine pool
	_, decodeSpan := tracer.Start(ctx, "DecodeYjsConcurrent",
		oteltrace.WithAttributes(
			attribute.Int("file.count", len(files)),
		),
	)

	for _, f := range files {
		if f.IsFolder {
			continue
		}
		wg.Add(1)
		go func(file pg.File) {
			defer wg.Done()

			doc, err := docRepo.LoadDocument(ctx, file.ID)
			if err != nil {
				fetchMu.Lock()
				fetchErr = fmt.Errorf("load document %s: %w", file.ID, err)
				fetchMu.Unlock()
				return
			}

			var text string
			if doc != nil && len(doc.YjsState) > 0 {
				text, err = DecodeYjsToText(doc.YjsState)
				if err != nil {
					fetchMu.Lock()
					fetchErr = fmt.Errorf("decode yjs %s: %w", file.ID, err)
					fetchMu.Unlock()
					return
				}
			}

			preview := text
			if len(preview) > 50 {
				preview = preview[:50]
			}
			log.Printf("[decoder] file=%s decoded %d chars: %q", file.Name, len(text), preview)
			contentMap.Store(file.ID, text)
		}(f)
	}

	roots := buildTree(files)
	log.Printf("[builder] tree structure:")
	printTree(roots, "  ")

	wg.Wait()
	decodeSpan.End()
	log.Printf("[builder] all files decoded")

	if fetchErr != nil {
		span.RecordError(fetchErr)
		span.SetStatus(otelcodes.Error, fetchErr.Error())
		return "", "", "", fetchErr
	}

	tempDir := fmt.Sprintf("/tmp/exec-%s", executionID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		return "", "", "", fmt.Errorf("create temp dir: %w", err)
	}
	log.Printf("[builder] created temp dir: %s", tempDir)

	var entryPath string
	if err := walkTree(roots, tempDir, &contentMap, fileID, &entryPath); err != nil {
		os.RemoveAll(tempDir)
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		return "", "", "", err
	}

	entryPath = strings.TrimPrefix(entryPath, tempDir+"/")

	log.Printf("[builder] file tree fully written to %s", tempDir)
	log.Printf("[builder] entry path: %s", entryPath)
	return tempDir, entryPath, entryLanguage, nil
}

func buildTree(files []pg.File) []*TreeNode {
	nodeMap := make(map[string]*TreeNode)

	for i := range files {
		nodeMap[files[i].ID] = &TreeNode{File: files[i]}
	}

	var roots []*TreeNode

	for _, node := range nodeMap {
		if node.File.ParentID == nil {
			roots = append(roots, node)
		} else {
			parent, exists := nodeMap[*node.File.ParentID]
			if !exists {
				roots = append(roots, node)
				continue
			}
			parent.Children = append(parent.Children, node)
		}
	}

	return roots
}

func printTree(nodes []*TreeNode, indent string) {
	for _, node := range nodes {
		kind := "file"
		if node.File.IsFolder {
			kind = "dir"
		}
		log.Printf("%s[%s] %s", indent, kind, node.File.Name)
		printTree(node.Children, indent+"  ")
	}
}

func walkTree(nodes []*TreeNode, currentPath string, contentMap *sync.Map, fileID string, entryPath *string) error {
	for _, node := range nodes {
		fullPath := filepath.Join(currentPath, node.File.Name)

		if node.File.ID == fileID {
			*entryPath = fullPath
		}

		if node.File.IsFolder {
			if err := os.MkdirAll(fullPath, 0755); err != nil {
				return fmt.Errorf("create folder %s: %w", fullPath, err)
			}
			log.Printf("[mkdir] %s", fullPath)
			if err := walkTree(node.Children, fullPath, contentMap, fileID, entryPath); err != nil {
				return err
			}
		} else {
			text := ""
			if val, ok := contentMap.Load(node.File.ID); ok {
				text = val.(string)
			}
			if err := os.WriteFile(fullPath, []byte(text), 0644); err != nil {
				return fmt.Errorf("write file %s: %w", fullPath, err)
			}
			log.Printf("[write] %s (%d chars)", fullPath, len(text))
		}
	}
	return nil
}