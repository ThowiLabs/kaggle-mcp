//go:build !linux

package runtimemetrics

type CPU struct {
	Available     bool     `json:"available"`
	Percent       *float64 `json:"percent"`
	LogicalCores  int      `json:"logical_cores"`
	CapacityCores float64  `json:"capacity_cores,omitempty"`
	Source        string   `json:"source"`
	Error         string   `json:"error,omitempty"`
}
type Memory struct {
	Available bool    `json:"available"`
	Total     uint64  `json:"total_bytes"`
	Used      uint64  `json:"used_bytes"`
	Free      uint64  `json:"free_bytes"`
	Percent   float64 `json:"used_percent"`
	Source    string  `json:"source"`
	Error     string  `json:"error,omitempty"`
}
type Sample struct {
	CPU    CPU    `json:"cpu"`
	Memory Memory `json:"memory"`
}
type Monitor struct{}

func New() *Monitor { return &Monitor{} }
func (*Monitor) Read() Sample {
	return Sample{CPU: CPU{Error: "Solo disponible en Kaggle Linux"}, Memory: Memory{Error: "Solo disponible en Kaggle Linux"}}
}
