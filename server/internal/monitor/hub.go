// Package monitor keeps the in-memory live state that does not belong in
// SQLite: the current agent connection and the latest report.
package monitor

import (
	"context"
	"sync"

	"probe/common/wire"
)

type Hub struct {
	mu     sync.Mutex
	conns  map[int64]*conn
	latest map[int64]wire.Report
}

type conn struct {
	id     string
	cancel context.CancelFunc
}

func NewHub() *Hub {
	return &Hub{conns: map[int64]*conn{}, latest: map[int64]wire.Report{}}
}

// Attach registers id as the current connection for agentID and returns the
// previous connection's cancel function, if any, so the caller can close it.
func (h *Hub) Attach(agentID int64, id string, cancel context.CancelFunc) context.CancelFunc {
	h.mu.Lock()
	defer h.mu.Unlock()
	var previous context.CancelFunc
	if old, ok := h.conns[agentID]; ok {
		previous = old.cancel
	}
	h.conns[agentID] = &conn{id: id, cancel: cancel}
	return previous
}

// Detach removes the connection only if it is still the current one. A late
// close from a replaced connection must not mark the agent offline.
func (h *Hub) Detach(agentID int64, id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	current, ok := h.conns[agentID]
	if !ok || current.id != id {
		return false
	}
	delete(h.conns, agentID)
	return true
}

func (h *Hub) Online(agentID int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[agentID]
	return ok
}

func (h *Hub) Store(agentID int64, report wire.Report) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.latest[agentID] = report
}

func (h *Hub) Latest(agentID int64) (wire.Report, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.latest[agentID]
	return r, ok
}
