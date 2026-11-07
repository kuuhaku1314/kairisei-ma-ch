package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"kairisei.local/server/internal/accountstore"
)

const maintenanceKey = "server-maintenance"

type MaintenanceState struct {
	Enabled       bool        `json:"enabled"`
	Revision      int         `json:"revision"`
	GameRequests  int         `json:"game_requests"`
	AdminRequests int         `json:"admin_requests"`
	Busy          bool        `json:"busy"`
	Cleanup       *CleanupJob `json:"cleanup,omitempty"`
}

type CleanupJob struct {
	GroupID     int                                    `json:"group_id,omitempty"`
	Scope       string                                 `json:"scope"`
	Mode        string                                 `json:"mode"`
	State       string                                 `json:"state"`
	Error       string                                 `json:"error,omitempty"`
	Report      *accountstore.OperationalCleanupReport `json:"report,omitempty"`
	AuditReport *accountstore.AuditCleanupReport       `json:"audit_report,omitempty"`
	// History cleanup report (scope "history").
	HistoryReport *accountstore.HistoryCleanupReport `json:"history_report,omitempty"`
}

type maintenanceGate struct {
	mu        sync.Mutex
	state     MaintenanceState
	cancel    context.CancelFunc
	done      chan struct{}
	gameIdle  chan struct{}
	adminIdle chan struct{}
}

func (o *Runtime) loadMaintenance() error {
	doc, err := o.storage.ReadDocument(maintenanceKey)
	if err != nil {
		return err
	}
	var saved struct {
		Enabled bool `json:"enabled"`
	}
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &saved); err != nil {
			return err
		}
	}
	o.maintenance.state = MaintenanceState{Enabled: saved.Enabled, Revision: doc.Revision}
	return nil
}

func (o *Runtime) MaintenanceState() MaintenanceState {
	o.maintenance.mu.Lock()
	defer o.maintenance.mu.Unlock()
	return o.maintenance.state
}

// Admission and its active count share a lock: cleanup cannot race the last
// accepted request, including registration before an account lock exists.
func (o *Runtime) AdmitGameRequest() (func(), bool) {
	m := &o.maintenance
	m.mu.Lock()
	if m.state.Enabled {
		m.mu.Unlock()
		return nil, false
	}
	m.state.GameRequests++
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		m.state.GameRequests--
		if m.state.GameRequests == 0 && m.gameIdle != nil {
			close(m.gameIdle)
			m.gameIdle = nil
		}
		m.mu.Unlock()
	}, true
}

var ErrMaintenanceConflict = errors.New("维护状态已变化或有任务正在进行，请刷新后重试")

func (o *Runtime) AdmitAdminMutation() (func(), bool) {
	m := &o.maintenance
	m.mu.Lock()
	if m.state.Busy {
		m.mu.Unlock()
		return nil, false
	}
	m.state.AdminRequests++
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		m.state.AdminRequests--
		if m.state.AdminRequests == 0 && m.adminIdle != nil {
			close(m.adminIdle)
			m.adminIdle = nil
		}
		m.mu.Unlock()
	}, true
}

// Stop new work, let accepted HTTP writes finish, then interrupt outstanding
// battles. The action drains room transactions before taking account locks.
func (o *Runtime) InterruptBattles(ctx context.Context, expected int, interrupt func(context.Context) error) error {
	m := &o.maintenance
	m.mu.Lock()
	if !m.state.Enabled || m.state.Busy || expected != m.state.Revision {
		m.mu.Unlock()
		return ErrMaintenanceConflict
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	m.cancel, m.done = cancel, done
	m.state.Busy = true
	// Allocate waiters only for an actual maintenance drain, not per request.
	if m.state.GameRequests != 0 && m.gameIdle == nil {
		m.gameIdle = make(chan struct{})
	}
	if m.state.AdminRequests != 0 && m.adminIdle == nil {
		m.adminIdle = make(chan struct{})
	}
	gameIdle, adminIdle := m.gameIdle, m.adminIdle
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		m.state.Busy = false
		m.mu.Unlock()
		close(done)
	}()
	for _, idle := range []chan struct{}{gameIdle, adminIdle} {
		if idle == nil {
			continue
		}
		select {
		case <-idle:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return interrupt(ctx)
}

func (o *Runtime) SetMaintenance(enabled bool, expected int, setRooms func(bool) error) (MaintenanceState, error) {
	m := &o.maintenance
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Busy || expected != m.state.Revision {
		return m.state, ErrMaintenanceConflict
	}
	if err := setRooms(enabled); err != nil {
		return m.state, fmt.Errorf("%w: %v", ErrMaintenanceConflict, err)
	}
	doc, err := o.storage.WriteDocument(maintenanceKey, m.state.Revision, struct {
		Enabled bool `json:"enabled"`
	}{enabled}, maintenanceKey)
	if err != nil {
		_ = setRooms(m.state.Enabled)
		return m.state, err
	}
	m.state.Enabled, m.state.Revision = enabled, doc.Revision
	return m.state, nil
}

// A single existing cleanup job owns admission and its completion channel.
// Register before spawning so shutdown cannot miss an accepted operation.
func (o *Runtime) RunCleanup(job CleanupJob, work func(context.Context, *CleanupJob) error) error {
	m := &o.maintenance
	m.mu.Lock()
	if !m.state.Enabled {
		m.mu.Unlock()
		return errors.New("请先开启服务器维护模式")
	}
	if m.state.Busy || m.state.GameRequests != 0 || m.state.AdminRequests != 0 {
		m.mu.Unlock()
		return errors.New("仍有请求正在处理，请稍后刷新再试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	done := make(chan struct{})
	m.cancel, m.done = cancel, done
	m.state.Busy = true
	running := job
	running.State = "running"
	m.state.Cleanup = &running
	m.mu.Unlock()
	go func() {
		defer close(done)
		defer cancel()
		finished := job
		finished.State = "complete"
		if err := work(ctx, &finished); err != nil {
			finished.State = "failed"
			finished.Error = err.Error()
		}
		m.mu.Lock()
		m.state.Cleanup = &finished
		m.state.Busy = false
		m.mu.Unlock()
	}()
	return nil
}

func (o *Runtime) CloseMaintenance() {
	m := &o.maintenance
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
