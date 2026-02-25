package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

const (
	stderrFlushInterval = 2 * time.Second
	stderrMaxBatchSize  = 50
)

// taskLogger sends structured logs and batched stderr to the server via ReportTaskLog RPC.
type taskLogger struct {
	client    taskguildv1connect.AgentManagerServiceClient
	taskID    string
	stderrBuf []string
	stderrMu  sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func newTaskLogger(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, taskID string) *taskLogger {
	ctx, cancel := context.WithCancel(ctx)
	tl := &taskLogger{
		client: client,
		taskID: taskID,
		ctx:    ctx,
		cancel: cancel,
	}
	tl.wg.Add(1)
	go tl.stderrFlusher()
	return tl
}

// Log sends a structured log entry to the server.
func (tl *taskLogger) Log(category v1.TaskLogCategory, level v1.TaskLogLevel, message string, metadata map[string]string) {
	_, err := tl.client.ReportTaskLog(tl.ctx, connect.NewRequest(&v1.ReportTaskLogRequest{
		TaskId:   tl.taskID,
		Level:    level,
		Category: category,
		Message:  message,
		Metadata: metadata,
	}))
	if err != nil {
		log.Printf("[task:%s] failed to report task log: %v", tl.taskID, err)
	}
}

// LogStderr buffers a stderr line for batched sending.
func (tl *taskLogger) LogStderr(line string) {
	tl.stderrMu.Lock()
	tl.stderrBuf = append(tl.stderrBuf, line)
	shouldFlush := len(tl.stderrBuf) >= stderrMaxBatchSize
	tl.stderrMu.Unlock()

	if shouldFlush {
		tl.flushStderr()
	}
}

// flushStderr sends accumulated stderr lines as a single log entry.
func (tl *taskLogger) flushStderr() {
	tl.stderrMu.Lock()
	if len(tl.stderrBuf) == 0 {
		tl.stderrMu.Unlock()
		return
	}
	lines := tl.stderrBuf
	tl.stderrBuf = nil
	tl.stderrMu.Unlock()

	message := strings.Join(lines, "\n")
	tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_STDERR, v1.TaskLogLevel_TASK_LOG_LEVEL_DEBUG, message, nil)
}

// stderrFlusher periodically flushes buffered stderr.
func (tl *taskLogger) stderrFlusher() {
	defer tl.wg.Done()
	ticker := time.NewTicker(stderrFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tl.ctx.Done():
			return
		case <-ticker.C:
			tl.flushStderr()
		}
	}
}

// Close flushes remaining stderr and stops the background flusher.
func (tl *taskLogger) Close() {
	tl.cancel()
	tl.wg.Wait()
	tl.flushStderr()
}
