package monitor

import (
	"context"
	"sync"
	"time"

	"probe/common/wire"
	"probe/server/internal/repository"
)

// MetricRecorder persists the latest report into a fixed bucket. Writes are
// throttled so a 3-second agent does not turn into a 3-second SQLite writer.
type MetricRecorder struct {
	repo       *repository.HistoryRepo
	interval   time.Duration
	mu         sync.Mutex
	lastWrite  map[int64]time.Time
	lastBucket map[int64]int64
}

func NewMetricRecorder(repo *repository.HistoryRepo, interval time.Duration) *MetricRecorder {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &MetricRecorder{
		repo: repo, interval: interval,
		lastWrite: map[int64]time.Time{}, lastBucket: map[int64]int64{},
	}
}

func (r *MetricRecorder) Record(ctx context.Context, agentID int64, report wire.Report, now time.Time) error {
	measured := now
	if report.Timestamp > 0 {
		if delta := now.Unix() - report.Timestamp; delta <= 600 && delta >= -600 {
			measured = time.Unix(report.Timestamp, 0)
		}
	}
	bucket := measured.Unix() / int64(r.interval.Seconds()) * int64(r.interval.Seconds())
	r.mu.Lock()
	if r.lastBucket[agentID] == bucket {
		if last, ok := r.lastWrite[agentID]; ok && now.Sub(last) < 30*time.Second {
			r.mu.Unlock()
			return nil
		}
	}
	r.lastWrite[agentID] = now
	r.lastBucket[agentID] = bucket
	r.mu.Unlock()
	return r.repo.UpsertMetric(ctx, agentID, bucket, report)
}
