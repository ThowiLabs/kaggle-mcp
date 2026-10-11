//go:build !linux

package diskguard

type DiskUsage struct {
	Available bool    `json:"available"`
	Path      string  `json:"path"`
	Total     uint64  `json:"total_bytes"`
	Used      uint64  `json:"used_bytes"`
	Free      uint64  `json:"free_bytes"`
	Percent   float64 `json:"used_percent"`
	Error     string  `json:"error,omitempty"`
}

func Usage(path string) DiskUsage {
	return DiskUsage{Path: path, Error: "Métricas de volumen disponibles en Kaggle Linux"}
}
