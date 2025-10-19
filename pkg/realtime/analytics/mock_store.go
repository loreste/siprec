package analytics

import "sync"

// MockStateStore implements StateStore for tests.
type MockStateStore struct {
	mu    sync.RWMutex
	store map[string]*State
}

func NewMockStateStore() *MockStateStore {
	return &MockStateStore{store: make(map[string]*State)}
}

func (s *MockStateStore) Get(callID string) (*State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.store[callID]
	if !ok {
		return nil, nil
	}
	copy := *state
	return &copy, nil
}

func (s *MockStateStore) Set(callID string, state *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *state
	s.store[callID] = &copy
	return nil
}

func (s *MockStateStore) Delete(callID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.store, callID)
	return nil
}
