package gpumetrics

// NVIDIA GPU telemetry without Python, NVML bindings or a shell.
// Queries are bounded and never modify a device or another agent's process.
import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	backend      = "nvidia-smi"
	maxOutput    = 128 << 10
	queryTimeout = 6 * time.Second
)
const (
	inventoryFields = "index,uuid,name,driver_version,memory.total,pci.bus_id"
	metricFields    = "index,uuid,name,utilization.gpu,utilization.memory,memory.total,memory.used,memory.free,temperature.gpu,power.draw,power.limit,fan.speed,pstate"
	processFields   = "gpu_uuid,pid,process_name,used_gpu_memory"
)

type Runner interface {
	Run(context.Context, ...string) (string, error)
}
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, args ...string) (string, error) {
	binary, err := exec.LookPath(backend)
	if err != nil {
		return "", ErrUnavailable
	}
	task, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	cmd := exec.CommandContext(task, binary, args...)
	// Capture a bounded amount of output; nvidia-smi stdout/stderr do not contain
	// customer data or configuration secrets under these fixed query flags.
	var out boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if task.Err() != nil {
		return "", fmt.Errorf("nvidia-smi cancelado o excedió %s: %w", queryTimeout, task.Err())
	}
	if err != nil {
		message := strings.TrimSpace(out.String())
		if len(message) > 300 {
			message = message[:300]
		}
		return "", fmt.Errorf("no se pudieron leer las métricas NVIDIA: %s (%w)", message, err)
	}
	if out.overflow {
		return "", errors.New("respuesta nvidia-smi demasiado grande")
	}
	return out.String(), nil
}

type boundedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	n := len(data)
	if b.Len()+n > maxOutput {
		b.overflow = true
	}
	if b.Len() < maxOutput {
		_, _ = b.Buffer.Write(data[:min(n, maxOutput-b.Len())])
	}
	return n, nil
}

var ErrUnavailable = errors.New("nvidia-smi no está instalado en este runtime")

type Collector struct{ Runner Runner }

func New() Collector { return Collector{Runner: ExecRunner{}} }
func (c Collector) runner() Runner {
	if c.Runner == nil {
		return ExecRunner{}
	}
	return c.Runner
}

type Device struct {
	Index                    int      `json:"index"`
	UUID                     string   `json:"uuid"`
	Name                     string   `json:"name"`
	DriverVersion            string   `json:"driver_version,omitempty"`
	PCIBusID                 string   `json:"pci_bus_id,omitempty"`
	MemoryTotalMiB           *float64 `json:"memory_total_mib"`
	MemoryUsedMiB            *float64 `json:"memory_used_mib,omitempty"`
	MemoryFreeMiB            *float64 `json:"memory_free_mib,omitempty"`
	MemoryPercent            *float64 `json:"memory_percent,omitempty"`
	UtilizationGPUPercent    *float64 `json:"utilization_gpu_percent,omitempty"`
	UtilizationMemoryPercent *float64 `json:"utilization_memory_percent,omitempty"`
	TemperatureCelsius       *float64 `json:"temperature_celsius,omitempty"`
	PowerWatts               *float64 `json:"power_watts,omitempty"`
	PowerLimitWatts          *float64 `json:"power_limit_watts,omitempty"`
	FanPercent               *float64 `json:"fan_percent,omitempty"`
	PerformanceState         string   `json:"performance_state,omitempty"`
}
type Process struct {
	GPUUUID       string   `json:"gpu_uuid"`
	PID           int      `json:"pid"`
	Name          string   `json:"name"`
	UsedMemoryMiB *float64 `json:"used_memory_mib,omitempty"`
}
type Report struct {
	Available          bool      `json:"available"`
	Backend            string    `json:"backend"`
	CollectedAt        time.Time `json:"collected_at"`
	GPUCount           int       `json:"gpu_count"`
	GPUs               []Device  `json:"gpus"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Hint               string    `json:"hint,omitempty"`
	CUDAVisibleDevices string    `json:"cuda_visible_devices,omitempty"`
}
type ProcessReport struct {
	Available   bool      `json:"available"`
	Backend     string    `json:"backend"`
	CollectedAt time.Time `json:"collected_at"`
	Processes   []Process `json:"processes"`
	Count       int       `json:"count"`
	Status      string    `json:"status"`
	Reason      string    `json:"reason,omitempty"`
	Hint        string    `json:"hint,omitempty"`
}

func newReport() Report {
	r := Report{Backend: backend, CollectedAt: time.Now().UTC(), GPUs: []Device{}, Status: "unavailable"}
	if v, ok := os.LookupEnv("CUDA_VISIBLE_DEVICES"); ok && len(v) <= 128 {
		r.CUDAVisibleDevices = v
	}
	return r
}
func classify(err error) (status, reason, hint string) {
	if errors.Is(err, ErrUnavailable) {
		return "not_installed", err.Error(), "En Kaggle activa Accelerator: GPU; si sigue fallando, comprueba el controlador NVIDIA."
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled", "consulta cancelada", "Vuelve a intentarlo."
	}
	return "driver_or_query_error", err.Error(), "Comprueba que Kaggle tenga GPU activada y que nvidia-smi pueda comunicarse con el driver."
}
func rows(out string, count int) ([][]string, error) {
	if strings.TrimSpace(out) == "" {
		return [][]string{}, nil
	}
	reader := csv.NewReader(strings.NewReader(out))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	result := [][]string{}
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV nvidia-smi inválido: %w", err)
		}
		if len(row) != count {
			return nil, fmt.Errorf("nvidia-smi devolvió %d columnas, se esperaban %d", len(row), count)
		}
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		result = append(result, row)
		if len(result) > 256 {
			return nil, errors.New("demasiados dispositivos/procesos GPU en una sola consulta")
		}
	}
	return result, nil
}
func optionalNumber(raw string) *float64 {
	value := strings.TrimSpace(raw)
	if value == "" || strings.EqualFold(value, "N/A") || strings.EqualFold(value, "Not Supported") || value == "[Not Supported]" {
		return nil
	}
	// Some driver builds include units even with 'nounits'.
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(value, " MiB"), " W"), " %"), " C"))
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &f
}
func normalizeIndex(raw string) (int, error) {
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 || index > 1024 {
		return 0, fmt.Errorf("índice GPU inválido: %q", raw)
	}
	return index, nil
}
func (c Collector) Detect(ctx context.Context) (Report, error) {
	report := newReport()
	out, err := c.runner().Run(ctx, "--query-gpu="+inventoryFields, "--format=csv,noheader,nounits")
	if err != nil {
		report.Status, report.Reason, report.Hint = classify(err)
		return report, nil
	}
	parsed, err := rows(out, 6)
	if err != nil {
		return report, err
	}
	for _, v := range parsed {
		index, e := normalizeIndex(v[0])
		if e != nil {
			return report, e
		}
		device := Device{Index: index, UUID: v[1], Name: v[2], DriverVersion: v[3], MemoryTotalMiB: optionalNumber(v[4]), PCIBusID: v[5]}
		report.GPUs = append(report.GPUs, device)
	}
	report.GPUCount = len(report.GPUs)
	report.Available = report.GPUCount > 0
	if report.Available {
		report.Status = "ready"
	} else {
		report.Status = "no_devices"
		report.Hint = "Kaggle puede estar ejecutándose en CPU. Activa Accelerator: GPU en la configuración del notebook."
	}
	return report, nil
}
func (c Collector) Metrics(ctx context.Context) (Report, error) {
	report := newReport()
	out, err := c.runner().Run(ctx, "--query-gpu="+metricFields, "--format=csv,noheader,nounits")
	if err != nil {
		report.Status, report.Reason, report.Hint = classify(err)
		return report, nil
	}
	parsed, err := rows(out, 13)
	if err != nil {
		return report, err
	}
	for _, v := range parsed {
		idx, e := normalizeIndex(v[0])
		if e != nil {
			return report, e
		}
		device := Device{Index: idx, UUID: v[1], Name: v[2],
			UtilizationGPUPercent: optionalNumber(v[3]), UtilizationMemoryPercent: optionalNumber(v[4]),
			MemoryTotalMiB: optionalNumber(v[5]), MemoryUsedMiB: optionalNumber(v[6]), MemoryFreeMiB: optionalNumber(v[7]),
			TemperatureCelsius: optionalNumber(v[8]), PowerWatts: optionalNumber(v[9]),
			PowerLimitWatts: optionalNumber(v[10]), FanPercent: optionalNumber(v[11]), PerformanceState: v[12],
		}
		if device.MemoryTotalMiB != nil && *device.MemoryTotalMiB > 0 && device.MemoryUsedMiB != nil {
			p := 100 * (*device.MemoryUsedMiB) / (*device.MemoryTotalMiB)
			device.MemoryPercent = &p
		}
		report.GPUs = append(report.GPUs, device)
	}
	report.GPUCount = len(report.GPUs)
	report.Available = report.GPUCount > 0
	if report.Available {
		report.Status = "ready"
	} else {
		report.Status = "no_devices"
		report.Hint = "Activa Accelerator: GPU en Kaggle si necesitas aceleración."
	}
	return report, nil
}
func (c Collector) Processes(ctx context.Context) (ProcessReport, error) {
	report := ProcessReport{Backend: backend, CollectedAt: time.Now().UTC(), Processes: []Process{}, Status: "unavailable"}
	out, err := c.runner().Run(ctx, "--query-compute-apps="+processFields, "--format=csv,noheader,nounits")
	if err != nil {
		report.Status, report.Reason, report.Hint = classify(err)
		return report, nil
	}
	parsed, err := rows(out, 4)
	if err != nil {
		return report, err
	}
	for _, v := range parsed {
		pid, e := strconv.Atoi(v[1])
		if e != nil || pid <= 0 {
			continue
		}
		report.Processes = append(report.Processes, Process{GPUUUID: v[0], PID: pid, Name: v[2], UsedMemoryMiB: optionalNumber(v[3])})
	}
	report.Available = true
	report.Status = "ready"
	report.Count = len(report.Processes)
	return report, nil
}
