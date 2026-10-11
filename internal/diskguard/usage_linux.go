//go:build linux

package diskguard

import (
	"os"
	"path/filepath"
	"syscall"
)

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
	result := DiskUsage{Path: path}
	p := path
	for {
		if _, err := os.Stat(p); err == nil {
			break
		}
		next := filepath.Dir(p)
		if next == p {
			result.Error = "Volumen no encontrado"
			return result
		}
		p = next
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		result.Error = "No se pudo consultar el volumen"
		return result
	}
	total := uint64(st.Blocks) * uint64(st.Bsize)
	free := uint64(st.Bavail) * uint64(st.Bsize)
	if total == 0 {
		result.Error = "Capacidad no disponible"
		return result
	}
	used := total - uint64(st.Bfree)*uint64(st.Bsize)
	result.Available, result.Total, result.Free, result.Used = true, total, free, used
	result.Percent = 100 * float64(used) / float64(total)
	return result
}
