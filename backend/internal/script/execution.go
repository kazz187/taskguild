package script

import (
	"sync"

	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
)

// InMemoryResultStore stores script execution results in memory.
type InMemoryResultStore struct {
	mu      sync.RWMutex
	results map[string]*taskguildv1.GetScriptExecutionResultResponse
}

func NewInMemoryResultStore() *InMemoryResultStore {
	return &InMemoryResultStore{
		results: make(map[string]*taskguildv1.GetScriptExecutionResultResponse),
	}
}

func (s *InMemoryResultStore) StoreResult(requestID string, result *taskguildv1.GetScriptExecutionResultResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[requestID] = result
}

func (s *InMemoryResultStore) GetResult(requestID string) (*taskguildv1.GetScriptExecutionResultResponse, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.results[requestID]
	return r, ok
}
