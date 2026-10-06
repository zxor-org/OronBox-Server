package syndication

import (
	"context"
	"math"
	"time"
)

type Task struct {
	ID            string
	Attempts      int
	NextAttemptAt time.Time
}
type RetryWorker struct {
	MaxAttempts int
	BaseDelay   time.Duration
	Run         func(context.Context, Task) error
}

func (w RetryWorker) Execute(ctx context.Context, t Task) Task {
	if w.MaxAttempts <= 0 {
		w.MaxAttempts = 5
	}
	if w.BaseDelay <= 0 {
		w.BaseDelay = 30 * time.Second
	}
	for t.Attempts < w.MaxAttempts {
		if err := w.Run(ctx, t); err == nil {
			return t
		}
		t.Attempts++
		if t.Attempts >= w.MaxAttempts {
			break
		}
		d := time.Duration(float64(w.BaseDelay) * math.Pow(2, float64(t.Attempts-1)))
		if d > 10*time.Minute {
			d = 10 * time.Minute
		}
		t.NextAttemptAt = time.Now().Add(d)
		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return t
		case <-timer.C:
		}
	}
	return t
}
