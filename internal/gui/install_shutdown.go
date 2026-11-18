package gui

import "time"

// Cancellation has already been broadcast through rootCtx. Wait without holding
// the registry lock so each worker can publish its final state and release it.
func (s *installSessions) waitForShutdown(timeout time.Duration) bool {
	s.mu.Lock()
	pending := make([]<-chan struct{}, 0, len(s.sessions))
	for _, session := range s.sessions {
		if !session.view.Terminal {
			session.cancel()
			pending = append(pending, session.done)
		}
	}
	s.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for _, done := range pending {
		select {
		case <-done:
		case <-timer.C:
			return false
		}
	}
	return true
}
