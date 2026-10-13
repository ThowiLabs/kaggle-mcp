package gpumetrics

import (
	"context"
	"errors"
	"time"
)

type Sample struct {
	ElapsedSeconds int    `json:"elapsed_seconds"`
	Report         Report `json:"report"`
}
type MonitoringReport struct {
	Backend         string    `json:"backend"`
	StartedAt       time.Time `json:"started_at"`
	Samples         []Sample  `json:"samples"`
	Count           int       `json:"count"`
	IntervalSeconds int       `json:"interval_seconds"`
	Status          string    `json:"status"`
}

// Monitor gathers at most 10 individual snapshots; no background tasks,
// no files, no indefinite loops and respects MCP client cancellation.
func (c Collector) Monitor(ctx context.Context, samples, intervalSeconds int) (MonitoringReport, error) {
	report := MonitoringReport{Backend: backend, StartedAt: time.Now().UTC(), IntervalSeconds: intervalSeconds, Samples: []Sample{}, Status: "finished"}
	if samples < 1 || samples > 10 || intervalSeconds < 1 || intervalSeconds > 5 {
		return report, errors.New("samples debe ser 1–10 e interval_seconds 1–5")
	}
	if samples > 1 && (samples-1)*intervalSeconds > 20 {
		return report, errors.New("duración máxima de monitor GPU es 20 segundos")
	}
	// El sondeo puede tardar varios segundos en responder; no exceder 30s totales.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for i := 0; i < samples; i++ {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if i > 0 {
			timer := time.NewTimer(time.Duration(intervalSeconds) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return report, ctx.Err()
			case <-timer.C:
			}
		}
		snapshot, err := c.Metrics(ctx)
		if err != nil {
			return report, err
		}
		report.Samples = append(report.Samples, Sample{ElapsedSeconds: i * intervalSeconds, Report: snapshot})
		report.Count = len(report.Samples)
		if !snapshot.Available {
			report.Status = snapshot.Status
			break
		}
	}
	return report, nil
}
