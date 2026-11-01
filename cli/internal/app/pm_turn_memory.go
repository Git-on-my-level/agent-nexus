// Per-turn working state for `anx pm serve`: one pmTurnMemory instance tracks
// harness attempts, give-up state, delivery attempts and heartbeat disabling
// for a claimed turn.

package app

import (
	"sync"
)

type pmTurnMemory struct {
	mu                sync.Mutex
	byID              map[string]*pmTurnMemEntry
	heartbeatDisabled bool
}

type pmTurnMemEntry struct {
	harnessRuns      int
	deliveryAttempts int
	givenUp          bool
	giveUpLogged     bool
}

func newPMTurnMemory() *pmTurnMemory {
	return &pmTurnMemory{byID: map[string]*pmTurnMemEntry{}}
}

func (a *App) turnMem() *pmTurnMemory {
	if a == nil {
		return newPMTurnMemory()
	}
	if a.pmTurns == nil {
		a.pmTurns = newPMTurnMemory()
	}
	return a.pmTurns
}

func (m *pmTurnMemory) entry(id string) *pmTurnMemEntry {
	if m == nil {
		return &pmTurnMemEntry{}
	}
	if m.byID == nil {
		m.byID = map[string]*pmTurnMemEntry{}
	}
	e := m.byID[id]
	if e == nil {
		e = &pmTurnMemEntry{}
		m.byID[id] = e
	}
	return e
}

func (m *pmTurnMemory) observeTerminal(id string) {
	if m == nil || id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byID, id)
}

func (m *pmTurnMemory) noteHarnessRun(id string) int {
	if m == nil || id == "" {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entry(id)
	e.harnessRuns++
	return e.harnessRuns
}

func (m *pmTurnMemory) harnessRuns(id string) int {
	if m == nil || id == "" {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.byID[id]
	if e == nil {
		return 0
	}
	return e.harnessRuns
}

func (m *pmTurnMemory) canRunHarness(id string) bool {
	if m == nil || id == "" {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.byID[id]
	if e == nil {
		return true
	}
	return e.harnessRuns < maxHarnessRunsPerTurn && !e.givenUp
}

func (m *pmTurnMemory) markGivenUp(id string) {
	if m == nil || id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entry(id)
	e.givenUp = true
}

func (m *pmTurnMemory) isGivenUp(id string) bool {
	if m == nil || id == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.byID[id]
	return e != nil && e.givenUp
}

func (m *pmTurnMemory) consumeGiveUpLog(id string) bool {
	if m == nil || id == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.byID[id]
	if e == nil || e.giveUpLogged {
		return false
	}
	e.giveUpLogged = true
	return true
}

func (m *pmTurnMemory) noteDeliveryAttempt(id string) int {
	if m == nil || id == "" {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entry(id)
	e.deliveryAttempts++
	return e.deliveryAttempts
}

func (m *pmTurnMemory) disableHeartbeat() (first bool) {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.heartbeatDisabled {
		return false
	}
	m.heartbeatDisabled = true
	return true
}

func (m *pmTurnMemory) heartbeatOff() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.heartbeatDisabled
}
