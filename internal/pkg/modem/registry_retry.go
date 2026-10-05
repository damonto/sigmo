package modem

// Only the registry's device loop retries opens. It first discovers the current
// device snapshot so a queued retry cannot resurrect a physically removed device.
func (r *Registry) scheduleOpenRetry(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.cidRecoveryStates[key] == cidRecoverySuspended {
		delete(r.pendingOpens, key)
		return
	}
	if r.pendingOpens == nil {
		r.pendingOpens = make(map[string]struct{})
	}
	r.pendingOpens[key] = struct{}{}
}

func (r *Registry) clearOpenRetry(key string) {
	r.mu.Lock()
	delete(r.pendingOpens, key)
	r.mu.Unlock()
}

func (r *Registry) hasPendingOpens() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.pendingOpens) != 0
}
