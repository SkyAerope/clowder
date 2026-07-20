package health

import "time"

type PersistedState struct {
	Provider       string
	Model          string
	Status         string
	AvgTTFTMs      int64
	ErrorCount     int
	BackoffCount   int
	UnhealthyUntil *time.Time
	LastErrorAt    *time.Time
	LastSuccessAt  *time.Time
	LastRule       string
}

func NewPersistent(states []PersistedState, save func(PersistedState)) *Tracker {
	t := &Tracker{m: map[[2]string]*entry{}, save: save}
	now := time.Now()
	for _, state := range states {
		status := state.Status
		if status == "" {
			status = Probing
		}
		if status == Unhealthy && (state.UnhealthyUntil == nil || !now.Before(*state.UnhealthyUntil)) {
			status = Probing
		}
		e := &entry{
			status: status, avgTTFTMs: state.AvgTTFTMs, errorCount: state.ErrorCount,
			backoffCount: state.BackoffCount, lastRule: state.LastRule,
		}
		if status == Unhealthy && state.UnhealthyUntil != nil {
			e.until = *state.UnhealthyUntil
		}
		if state.LastErrorAt != nil {
			e.lastError = *state.LastErrorAt
		}
		if state.LastSuccessAt != nil {
			e.lastSuccess = *state.LastSuccessAt
		}
		if e.avgTTFTMs > 0 {
			e.ttftN = 1
		}
		t.m[[2]string{state.Provider, state.Model}] = e
		if status != state.Status {
			t.persist(state.Provider, state.Model, e)
		}
	}
	return t
}

func (t *Tracker) persist(provider, model string, e *entry) {
	if t.save == nil {
		return
	}
	state := PersistedState{
		Provider: provider, Model: model, Status: e.status,
		AvgTTFTMs: e.avgTTFTMs, ErrorCount: e.errorCount, BackoffCount: e.backoffCount,
		LastRule: e.lastRule,
	}
	if !e.until.IsZero() {
		until := e.until
		state.UnhealthyUntil = &until
	}
	if !e.lastError.IsZero() {
		lastError := e.lastError
		state.LastErrorAt = &lastError
	}
	if !e.lastSuccess.IsZero() {
		lastSuccess := e.lastSuccess
		state.LastSuccessAt = &lastSuccess
	}
	t.save(state)
}
