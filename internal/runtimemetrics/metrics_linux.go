//go:build linux

package runtimemetrics

import (
	"bufio"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

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
type reading struct {
	usage    uint64
	total    uint64
	idle     uint64
	at       time.Time
	mode     string
	capacity float64
}
type Monitor struct {
	mu   sync.Mutex
	last reading
}

func New() *Monitor { return &Monitor{} }
func parseUint(path string) (uint64, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return 0, e
	}
	return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
}
func cgroupCPU() (reading, error) {
	b, e := os.ReadFile("/sys/fs/cgroup/cpu.stat")
	if e != nil {
		return reading{}, e
	}
	var usage uint64
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "usage_usec" {
			usage, e = strconv.ParseUint(f[1], 10, 64)
			if e != nil {
				return reading{}, e
			}
			found = true
			break
		}
	}
	if !found {
		return reading{}, errors.New("no usage_usec")
	}
	cpuMax, e := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if e != nil {
		return reading{}, e
	}
	v := strings.Fields(string(cpuMax))
	if len(v) < 2 {
		return reading{}, errors.New("cpu.max inválido")
	}
	capacity := float64(runtime.NumCPU())
	if v[0] != "max" {
		quota, eq := strconv.ParseFloat(v[0], 64)
		period, ep := strconv.ParseFloat(v[1], 64)
		if eq != nil || ep != nil || quota <= 0 || period <= 0 {
			return reading{}, errors.New("cpu.max inválido")
		}
		capacity = quota / period
	}
	if capacity <= 0 {
		return reading{}, errors.New("cpu capacity inválida")
	}
	return reading{usage: usage, mode: "cgroup-v2", capacity: capacity, at: time.Now()}, nil
}
func hostCPU() (reading, error) {
	b, e := os.ReadFile("/proc/stat")
	if e != nil {
		return reading{}, e
	}
	first := strings.Fields(strings.SplitN(string(b), "\n", 2)[0])
	if len(first) < 5 || first[0] != "cpu" {
		return reading{}, errors.New("proc stat inválido")
	}
	var total, idle uint64
	for i, v := range first[1:] {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return reading{}, err
		}
		total += n
		if i == 3 || i == 4 {
			idle += n
		}
	}
	return reading{total: total, idle: idle, mode: "host", capacity: float64(runtime.NumCPU()), at: time.Now()}, nil
}
func (m *Monitor) readCPU() CPU {
	m.mu.Lock()
	defer m.mu.Unlock()
	now, err := cgroupCPU()
	if err != nil {
		now, err = hostCPU()
	}
	if err != nil {
		return CPU{Error: "No se puede leer el uso CPU"}
	}
	previous := m.last
	if previous.mode != now.mode || previous.at.IsZero() {
		// First poll: two actual readings rather than a simulated zero.
		previous = now
		time.Sleep(180 * time.Millisecond)
		if now.mode == "host" {
			now, err = hostCPU()
		} else {
			now, err = cgroupCPU()
		}
		if err != nil {
			return CPU{Error: "Lectura CPU incompleta"}
		}
	}
	m.last = now
	var percentage float64
	if now.mode == "cgroup-v2" {
		elapsed := now.at.Sub(previous.at).Seconds() * 1e6 * now.capacity
		if elapsed <= 0 || now.usage < previous.usage {
			return CPU{Available: true, Source: now.mode, LogicalCores: runtime.NumCPU(), CapacityCores: now.capacity}
		}
		percentage = float64(now.usage-previous.usage) / elapsed * 100
	} else {
		if now.total <= previous.total || now.idle < previous.idle {
			return CPU{Available: true, Source: now.mode, LogicalCores: runtime.NumCPU(), CapacityCores: now.capacity}
		}
		percentage = 100 * float64((now.total-previous.total)-(now.idle-previous.idle)) / float64(now.total-previous.total)
	}
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}
	return CPU{Available: true, Percent: &percentage, LogicalCores: runtime.NumCPU(), CapacityCores: now.capacity, Source: now.mode}
}
func cgroupMemory() (Memory, error) {
	total, e := parseUint("/sys/fs/cgroup/memory.max")
	if e != nil || total == 0 || total > 1<<60 {
		return Memory{}, errors.New("sin límite cgroup finito")
	}
	current, e := parseUint("/sys/fs/cgroup/memory.current")
	if e != nil {
		return Memory{}, e
	}
	if current > total {
		current = total
	}
	return Memory{Available: true, Total: total, Used: current, Free: total - current, Percent: 100 * float64(current) / float64(total), Source: "cgroup-v2"}, nil
}
func hostMemory() (Memory, error) {
	f, e := os.Open("/proc/meminfo")
	if e != nil {
		return Memory{}, e
	}
	defer f.Close()
	values := map[string]uint64{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		parts := strings.Fields(scan.Text())
		if len(parts) >= 2 {
			key := strings.TrimSuffix(parts[0], ":")
			if key == "MemTotal" || key == "MemAvailable" {
				v, e := strconv.ParseUint(parts[1], 10, 64)
				if e == nil {
					values[key] = v * 1024
				}
			}
		}
	}
	if scan.Err() != nil || values["MemTotal"] == 0 {
		return Memory{}, errors.New("meminfo no disponible")
	}
	total := values["MemTotal"]
	free := values["MemAvailable"]
	if free > total {
		free = total
	}
	return Memory{Available: true, Total: total, Free: free, Used: total - free, Percent: 100 * float64(total-free) / float64(total), Source: "host"}, nil
}
func (m *Monitor) Read() Sample {
	cpu := m.readCPU()
	mem, err := cgroupMemory()
	if err != nil {
		mem, err = hostMemory()
	}
	if err != nil {
		mem = Memory{Error: "No se pudo leer la RAM"}
	}
	return Sample{CPU: cpu, Memory: mem}
}
