package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ThowiLabs/kagssh-go/internal/gpumetrics"
)

type fakeGPU struct{ queries []string }

func (f *fakeGPU) Run(ctx context.Context, args ...string) (string, error) {
	f.queries = append(f.queries, args[0])
	if len(args) == 0 {
		return "", errors.New("sin query")
	}
	switch {
	case args[0] == "--query-gpu=index,uuid,name,driver_version,memory.total,pci.bus_id":
		return "0, GPU-test, NVIDIA T4, 535.1, 15360, 00000000:00:1E.0", nil
	case args[0] == "--query-gpu=index,uuid,name,utilization.gpu,utilization.memory,memory.total,memory.used,memory.free,temperature.gpu,power.draw,power.limit,fan.speed,pstate":
		return "0, GPU-test, NVIDIA T4, 40, 5, 15360, 7680, 7680, 54, 45, 70, N/A, P2", nil
	case args[0] == "--query-compute-apps=gpu_uuid,pid,process_name,used_gpu_memory":
		return "GPU-test, 4555, python, 4096", nil
	default:
		return "", errors.New("query no reconocida")
	}
}
func TestGPUToolsAccessibleOverMCP(t *testing.T) {
	server := newTestServer(t)
	raw, err := json.Marshal(server.definitions())
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Name string `json:"name"`
	}
	if err = json.Unmarshal(raw, &tools); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, tool := range tools {
		found[tool.Name] = true
	}
	for _, name := range []string{"gpu_detect", "gpu_metrics", "gpu_processes", "gpu_monitor"} {
		if !found[name] || !gpuTool(name) {
			t.Fatalf("herramienta GPU no publicada: %s", name)
		}
	}
	fake := &fakeGPU{}
	collector := gpumetrics.Collector{Runner: fake}
	for _, name := range []string{"gpu_detect", "gpu_metrics", "gpu_processes"} {
		out, err := invokeGPU(context.Background(), name, nil, collector)
		if err != nil || out == nil {
			t.Fatalf("%s devolvió %v, error %v", name, out, err)
		}
	}
	result, err := invokeGPU(context.Background(), "gpu_monitor", json.RawMessage(`{"samples":1,"interval_seconds":1}`), collector)
	if err != nil || result == nil {
		t.Fatalf("gpu_monitor: %v", err)
	}
	if len(fake.queries) != 4 {
		t.Fatalf("se esperaban cuatro sondeos, got %d", len(fake.queries))
	}
}
func TestGPUMonitorRejectsLongOrMalformedArguments(t *testing.T) {
	runner := gpumetrics.Collector{Runner: &fakeGPU{}}
	for _, payload := range []string{"", "not-json", `{"samples":0,"interval_seconds":1}`, `{"samples":11,"interval_seconds":1}`, `{"samples":10,"interval_seconds":5}`, `{"samples":2,"interval_seconds":0}`} {
		if _, err := invokeGPU(context.Background(), "gpu_monitor", json.RawMessage(payload), runner); err == nil {
			t.Errorf("parámetros GPU monitor inválidos aceptados: %s", payload)
		}
	}
}
