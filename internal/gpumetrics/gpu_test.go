package gpumetrics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls [][]string
	run   func(context.Context, []string) (string, error)
}

func (f *fakeRunner) Run(ctx context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{}, args...))
	return f.run(ctx, args)
}
func TestDetectTwoNvidiaDevices(t *testing.T) {
	runner := &fakeRunner{run: func(_ context.Context, args []string) (string, error) {
		if args[0] != "--query-gpu="+inventoryFields {
			return "", errors.New("inventario incorrecto")
		}
		return "0, GPU-abc123, Tesla T4, 535.129, 15360, 00000000:00:1E.0\n1, GPU-def456, Tesla T4, 535.129, 15360, 00000000:00:1F.0\n", nil
	}}
	report, err := (Collector{Runner: runner}).Detect(context.Background())
	if err != nil || !report.Available || report.GPUCount != 2 || report.Status != "ready" {
		t.Fatalf("detección incorrecta: %#v %v", report, err)
	}
	if report.GPUs[1].Name != "Tesla T4" || report.GPUs[1].MemoryTotalMiB == nil || *report.GPUs[1].MemoryTotalMiB != 15360 {
		t.Fatalf("GPU secundaria no detectada: %#v", report.GPUs[1])
	}
	if len(runner.calls) != 1 {
		t.Fatal("debe ejecutar una sola consulta")
	}
}
func TestMetricsAndMissingFields(t *testing.T) {
	runner := &fakeRunner{run: func(_ context.Context, args []string) (string, error) {
		if args[0] != "--query-gpu="+metricFields {
			return "", errors.New("query de métricas incorrecta")
		}
		return "0, GPU-abc, Tesla T4, 78, 45, 15360, 6144, 9216, 69, 49.3, 70, N/A, P0\n" +
			"1, GPU-def, Tesla T4, N/A, N/A, 15360, 0, 15360, N/A, N/A, N/A, N/A, P8\n", nil
	}}
	result, err := (Collector{Runner: runner}).Metrics(context.Background())
	if err != nil || result.GPUCount != 2 {
		t.Fatalf("lectura métricas: %#v %v", result, err)
	}
	gpu := result.GPUs[0]
	if gpu.UtilizationGPUPercent == nil || *gpu.UtilizationGPUPercent != 78 || gpu.MemoryPercent == nil || *gpu.MemoryPercent != 40 {
		t.Fatalf("cálculo de GPU y VRAM: %#v", gpu)
	}
	if gpu.PowerWatts == nil || *gpu.PowerWatts != 49.3 || gpu.FanPercent != nil {
		t.Fatal("N/A o potencia mal interpretados")
	}
	if result.GPUs[1].UtilizationGPUPercent != nil || result.GPUs[1].MemoryPercent == nil || *result.GPUs[1].MemoryPercent != 0 {
		t.Fatal("métricas opcionales y ceros incorrectos")
	}
}
func TestGPUComputeProcessNamesAndMemory(t *testing.T) {
	runner := &fakeRunner{run: func(_ context.Context, args []string) (string, error) {
		if args[0] != "--query-compute-apps="+processFields {
			return "", errors.New("query de procesos incorrecta")
		}
		return "GPU-abc, 1234, \"python, trainer\", 1024\nGPU-def, 4321, python, N/A\n", nil
	}}
	report, err := (Collector{Runner: runner}).Processes(context.Background())
	if err != nil || !report.Available || report.Count != 2 || report.Processes[0].Name != "python, trainer" {
		t.Fatalf("procesos GPU: %#v %v", report, err)
	}
	if report.Processes[0].UsedMemoryMiB == nil || *report.Processes[0].UsedMemoryMiB != 1024 || report.Processes[1].UsedMemoryMiB != nil {
		t.Fatal("VRAM del proceso inválida")
	}
}
func TestNoGPUAndUnavailableDriverAreNotFalseSuccess(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status string
	}{
		{ErrUnavailable, "not_installed"},
		{errors.New("NVIDIA-SMI couldn't communicate with driver"), "driver_or_query_error"},
	} {
		collector := Collector{Runner: &fakeRunner{run: func(context.Context, []string) (string, error) { return "", tc.err }}}
		report, err := collector.Metrics(context.Background())
		if err != nil || report.Available || report.Status != tc.status || report.Reason == "" || report.Hint == "" {
			t.Fatalf("error GPU no distinguido: %#v %v", report, err)
		}
		processes, err := collector.Processes(context.Background())
		if err != nil || processes.Available || processes.Status != tc.status {
			t.Fatalf("error procesos no distinguido: %#v %v", processes, err)
		}
	}
}
func TestQueryValidationAndCSVErrors(t *testing.T) {
	if _, err := rows("0, Tesla T4", 6); err == nil {
		t.Fatal("faltan columnas")
	}
	if _, err := rows("0, \"broken", 3); err == nil {
		t.Fatal("CSV malformado aceptado")
	}
	if optionalNumber("N/A") != nil || optionalNumber("[Not Supported]") != nil || optionalNumber("bad") != nil {
		t.Fatal("campo no soportado inválido")
	}
	v := optionalNumber("32.5 W")
	if v == nil || *v != 32.5 {
		t.Fatal("unidad no parseada")
	}
	if _, err := normalizeIndex("-1"); err == nil {
		t.Fatal("índice negativo aceptado")
	}
}
func TestBoundedOutputNeverGrowsWithoutLimit(t *testing.T) {
	var b boundedBuffer
	input := strings.Repeat("x", maxOutput+5000)
	n, _ := b.Write([]byte(input))
	if n != len(input) || !b.overflow || b.Len() != maxOutput {
		t.Fatalf("buffer sin límite: %d", b.Len())
	}
}
func TestGPUCurrentMonitoringSnapshots(t *testing.T) {
	runner := &fakeRunner{run: func(_ context.Context, _ []string) (string, error) {
		return "0, GPU-abc, T4, 10, 20, 15360, 400, 14960, 45, 20, 70, N/A, P2", nil
	}}
	report, err := (Collector{Runner: runner}).Monitor(context.Background(), 2, 1)
	if err != nil || report.Count != 2 || report.Samples[0].ElapsedSeconds != 0 || report.Samples[1].ElapsedSeconds != 1 {
		t.Fatalf("monitor GPU inválido: %#v %v", report, err)
	}
	for _, tc := range []struct{ samples, seconds int }{{0, 1}, {11, 1}, {2, 0}, {2, 6}, {10, 3}} {
		if _, err := (Collector{Runner: runner}).Monitor(context.Background(), tc.samples, tc.seconds); err == nil {
			t.Errorf("monitor aceptó límites incorrectos %+v", tc)
		}
	}
}
func TestMonitoringCancellationStopsImmediately(t *testing.T) {
	collector := Collector{Runner: &fakeRunner{run: func(context.Context, []string) (string, error) {
		return "0, GPU-abc, T4, 10, 20, 15360, 400, 14960, 45, 20, 70, N/A, P2", nil
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := collector.Monitor(ctx, 5, 5)
	if !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatalf("monitor no cancelable: %v", err)
	}
}
