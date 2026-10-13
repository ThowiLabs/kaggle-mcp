package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ThowiLabs/kagssh-go/internal/gpumetrics"
)

func gpuTool(name string) bool {
	switch name {
	case "gpu_detect", "gpu_metrics", "gpu_processes", "gpu_monitor":
		return true
	}
	return false
}
func invokeGPU(ctx context.Context, name string, raw json.RawMessage, collector gpumetrics.Collector) (any, error) {
	switch name {
	case "gpu_detect":
		return collector.Detect(ctx)
	case "gpu_metrics":
		return collector.Metrics(ctx)
	case "gpu_processes":
		return collector.Processes(ctx)
	case "gpu_monitor":
		var a struct {
			Samples         int `json:"samples"`
			IntervalSeconds int `json:"interval_seconds"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, errors.New("argumentos gpu_monitor inválidos")
		}
		return collector.Monitor(ctx, a.Samples, a.IntervalSeconds)
	}
	return nil, errors.New("herramienta GPU desconocida")
}
