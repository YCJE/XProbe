package api

import (
	"context"
	"time"
)

// RunMaintenance purges expired sessions and old history. It is safe to run on
// startup and hourly; the handlers never depend on it for correctness.
func (s *Server) RunMaintenance(ctx context.Context) {
	s.runMaintenanceOnce(ctx)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runMaintenanceOnce(ctx)
		}
	}
}

func (s *Server) RunAlertEngine(ctx context.Context) {
	s.alertEngine.Run(ctx)
}

func (s *Server) runMaintenanceOnce(ctx context.Context) {
	if err := s.sessions.DeleteExpired(ctx, s.now()); err != nil {
		s.logger.Warn("expire sessions", "error", err)
	}
	if err := s.history.Cleanup(ctx, s.cfg.Storage.DataRetentionDays); err != nil {
		s.logger.Warn("cleanup history", "error", err)
	}
}
