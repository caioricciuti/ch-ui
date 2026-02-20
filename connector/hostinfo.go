package connector

import (
	"os"
	"runtime"
	"time"
)

// HostInfo contains system metrics from the host machine
type HostInfo struct {
	Hostname    string `json:"hostname"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	CPUCores    int    `json:"cpu_cores"`
	MemoryTotal uint64 `json:"memory_total"` // bytes
	MemoryFree  uint64 `json:"memory_free"`  // bytes
	DiskTotal   uint64 `json:"disk_total"`   // bytes
	DiskFree    uint64 `json:"disk_free"`    // bytes
	GoVersion   string `json:"go_version"`
	AgentUptime int64  `json:"agent_uptime"` // seconds
	CollectedAt string `json:"collected_at"` // ISO 8601
}

// CollectHostInfo gathers system metrics from the host machine
func CollectHostInfo(agentStartTime time.Time) *HostInfo {
	info := &HostInfo{
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		CPUCores:    runtime.NumCPU(),
		GoVersion:   runtime.Version(),
		AgentUptime: int64(time.Since(agentStartTime).Seconds()),
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Hostname
	if hostname, err := os.Hostname(); err == nil {
		info.Hostname = hostname
	} else {
		info.Hostname = "unknown"
	}

	// Memory stats (use Go runtime as cross-platform source)
	info.MemoryTotal, info.MemoryFree = getMemoryInfo()

	// Disk stats for root filesystem (platform-specific)
	info.DiskTotal, info.DiskFree = getDiskInfo()

	return info
}

// getMemoryInfo returns total and free memory in bytes
// Uses runtime.MemStats as a cross-platform approach
func getMemoryInfo() (total, free uint64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Use runtime stats as cross-platform source
	// Sys is total memory obtained from OS
	// Alloc is memory currently in use
	total = m.Sys
	free = m.Sys - m.Alloc
	return
}
