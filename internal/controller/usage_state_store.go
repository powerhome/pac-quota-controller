package controller

import "sync"

type namespaceUsageDirtyState struct {
	Compute     bool
	Storage     bool
	Services    bool
	ObjectCount bool
}

type quotaUsageState struct {
	namespaceDirty map[string]namespaceUsageDirtyState
}

type usageStateStore struct {
	mu     sync.RWMutex
	quotas map[string]*quotaUsageState
}

func newUsageStateStore() *usageStateStore {
	return &usageStateStore{quotas: make(map[string]*quotaUsageState)}
}

func (s *usageStateStore) ensureQuotaNamespaces(quotaName string, namespaces []string) *quotaUsageState {
	s.mu.Lock()
	defer s.mu.Unlock()

	quotaState, ok := s.quotas[quotaName]
	if !ok {
		quotaState = &quotaUsageState{namespaceDirty: make(map[string]namespaceUsageDirtyState)}
		s.quotas[quotaName] = quotaState
	}

	for i := range namespaces {
		ns := namespaces[i]
		if ns == "" {
			continue
		}
		if _, exists := quotaState.namespaceDirty[ns]; !exists {
			quotaState.namespaceDirty[ns] = namespaceUsageDirtyState{}
		}
	}

	return quotaState
}

func (s *usageStateStore) markNamespaceServicesDirty(quotaName, namespace string) {
	if quotaName == "" || namespace == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	quotaState, ok := s.quotas[quotaName]
	if !ok {
		quotaState = &quotaUsageState{namespaceDirty: make(map[string]namespaceUsageDirtyState)}
		s.quotas[quotaName] = quotaState
	}

	dirtyState := quotaState.namespaceDirty[namespace]
	dirtyState.Services = true
	quotaState.namespaceDirty[namespace] = dirtyState
}

func (s *usageStateStore) markNamespaceComputeDirty(quotaName, namespace string) {
	if quotaName == "" || namespace == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	quotaState, ok := s.quotas[quotaName]
	if !ok {
		quotaState = &quotaUsageState{namespaceDirty: make(map[string]namespaceUsageDirtyState)}
		s.quotas[quotaName] = quotaState
	}

	dirtyState := quotaState.namespaceDirty[namespace]
	dirtyState.Compute = true
	quotaState.namespaceDirty[namespace] = dirtyState
}

func (s *usageStateStore) markNamespaceStorageDirty(quotaName, namespace string) {
	if quotaName == "" || namespace == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	quotaState, ok := s.quotas[quotaName]
	if !ok {
		quotaState = &quotaUsageState{namespaceDirty: make(map[string]namespaceUsageDirtyState)}
		s.quotas[quotaName] = quotaState
	}

	dirtyState := quotaState.namespaceDirty[namespace]
	dirtyState.Storage = true
	quotaState.namespaceDirty[namespace] = dirtyState
}

func (s *usageStateStore) markNamespaceObjectCountDirty(quotaName, namespace string) {
	if quotaName == "" || namespace == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	quotaState, ok := s.quotas[quotaName]
	if !ok {
		quotaState = &quotaUsageState{namespaceDirty: make(map[string]namespaceUsageDirtyState)}
		s.quotas[quotaName] = quotaState
	}

	dirtyState := quotaState.namespaceDirty[namespace]
	dirtyState.ObjectCount = true
	quotaState.namespaceDirty[namespace] = dirtyState
}

func (s *usageStateStore) getNamespaceDirtyState(quotaName, namespace string) (namespaceUsageDirtyState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	quotaState, ok := s.quotas[quotaName]
	if !ok {
		return namespaceUsageDirtyState{}, false
	}

	dirtyState, ok := quotaState.namespaceDirty[namespace]
	if !ok {
		return namespaceUsageDirtyState{}, false
	}

	return dirtyState, true
}

func (s *usageStateStore) consumeNamespaceServicesDirty(quotaName, namespace string) bool {
	if quotaName == "" || namespace == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	quotaState, ok := s.quotas[quotaName]
	if !ok {
		return false
	}

	dirtyState, ok := quotaState.namespaceDirty[namespace]
	if !ok || !dirtyState.Services {
		return false
	}

	dirtyState.Services = false
	quotaState.namespaceDirty[namespace] = dirtyState
	return true
}

func (s *usageStateStore) consumeNamespaceComputeDirty(quotaName, namespace string) bool {
	if quotaName == "" || namespace == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	quotaState, ok := s.quotas[quotaName]
	if !ok {
		return false
	}

	dirtyState, ok := quotaState.namespaceDirty[namespace]
	if !ok || !dirtyState.Compute {
		return false
	}

	dirtyState.Compute = false
	quotaState.namespaceDirty[namespace] = dirtyState
	return true
}

func (s *usageStateStore) consumeNamespaceStorageDirty(quotaName, namespace string) bool {
	if quotaName == "" || namespace == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	quotaState, ok := s.quotas[quotaName]
	if !ok {
		return false
	}

	dirtyState, ok := quotaState.namespaceDirty[namespace]
	if !ok || !dirtyState.Storage {
		return false
	}

	dirtyState.Storage = false
	quotaState.namespaceDirty[namespace] = dirtyState
	return true
}

func (s *usageStateStore) consumeNamespaceObjectCountDirty(quotaName, namespace string) bool {
	if quotaName == "" || namespace == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	quotaState, ok := s.quotas[quotaName]
	if !ok {
		return false
	}

	dirtyState, ok := quotaState.namespaceDirty[namespace]
	if !ok || !dirtyState.ObjectCount {
		return false
	}

	dirtyState.ObjectCount = false
	quotaState.namespaceDirty[namespace] = dirtyState
	return true
}
