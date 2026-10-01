package device

import (
	"context"
	"time"
)

// StartHealthChecker periodically checks for dead sessions and cleans them up.
// It also polls ADB for newly connected/disconnected devices.
// Returns a cancel function to stop the goroutine.
func (m *Manager) StartHealthChecker(interval time.Duration) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.cleanupDeadSessions()
			}
		}
	}()

	m.logger.Info("session health checker started", "interval", interval.String())
	return cancel
}

// cleanupDeadSessions finds sessions that are no longer alive and removes them.
// Dead sessions are detached under m.mu but closed outside it, since Close
// can block for seconds.
func (m *Manager) cleanupDeadSessions() {
	m.mu.Lock()
	var dead []*Session
	for serial, session := range m.sessions {
		if !session.IsAlive() {
			m.logger.Warn("cleaning up dead session", "serial", serial)
			m.rememberAudioFailureLocked(serial, session)
			dead = append(dead, session)
			delete(m.sessions, serial)
			delete(m.sessionTiers, serial)
			delete(m.fullViewers, serial)
		}
	}
	m.mu.Unlock()

	for _, session := range dead {
		session.Close()
	}
}

// ActiveSessionCount returns the number of active sessions.
func (m *Manager) ActiveSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}
