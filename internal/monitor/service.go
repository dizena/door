package monitor

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gopsnet "github.com/shirou/gopsutil/v4/net"

	"door/internal/config"
)

// SystemSnapshot holds live host, resource, and Door directory telemetry.
type SystemSnapshot struct {
	Timestamp   time.Time    `json:"timestamp"`
	Hostname    string       `json:"hostname"`
	OS          string       `json:"os"`
	Platform    string       `json:"platform"`
	Arch        string       `json:"arch"`
	UptimeSec   uint64       `json:"uptime_sec"`
	GoVersion   string       `json:"go_version"`
	DriverMode  string       `json:"driver_mode"`
	ProcessMem  uint64       `json:"process_mem_bytes"`
	CPU         CPUMetrics   `json:"cpu"`
	Memory      MemMetrics   `json:"memory"`
	Disk        DiskMetrics  `json:"disk"`
	Network     NetMetrics   `json:"network"`
	Directories []DirStatus  `json:"directories"`
}

type CPUMetrics struct {
	Cores        int     `json:"cores"`
	UsagePercent float64 `json:"usage_percent"`
	Load1        float64 `json:"load1"`
	Load5        float64 `json:"load5"`
	Load15       float64 `json:"load15"`
}

type MemMetrics struct {
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type DiskMetrics struct {
	Path        string  `json:"path"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type NetMetrics struct {
	BytesSent uint64 `json:"bytes_sent"`
	BytesRecv uint64 `json:"bytes_recv"`
}

type DirStatus struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// Service collects host telemetry safely with context timeouts.
type Service struct {
	driverMode string
}

// NewService constructs a monitor Service.
func NewService(driverMode string) *Service {
	return &Service{driverMode: driverMode}
}

// Snapshot gathers a single point-in-time system snapshot.
func (s *Service) Snapshot(ctx context.Context, paths config.Paths) SystemSnapshot {
	snap := SystemSnapshot{
		Timestamp:  time.Now().UTC(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		GoVersion:  runtime.Version(),
		DriverMode: s.driverMode,
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	snap.ProcessMem = ms.Alloc

	if hInfo, err := host.InfoWithContext(ctx); err == nil {
		snap.Hostname = hInfo.Hostname
		snap.Platform = hInfo.Platform + " " + hInfo.PlatformVersion
		snap.UptimeSec = hInfo.Uptime
	} else if hn, err := os.Hostname(); err == nil {
		snap.Hostname = hn
	}

	snap.CPU.Cores = runtime.NumCPU()
	if pct, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(pct) > 0 {
		snap.CPU.UsagePercent = pct[0]
	}
	if avg, err := load.AvgWithContext(ctx); err == nil {
		snap.CPU.Load1 = avg.Load1
		snap.CPU.Load5 = avg.Load5
		snap.CPU.Load15 = avg.Load15
	}

	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		snap.Memory = MemMetrics{
			TotalBytes:  vm.Total,
			UsedBytes:   vm.Used,
			UsedPercent: vm.UsedPercent,
		}
	}

	diskTarget := paths.ServerDir
	if diskTarget == "" {
		diskTarget = "/"
	}
	if du, err := disk.UsageWithContext(ctx, diskTarget); err != nil {
		if duRoot, err2 := disk.UsageWithContext(ctx, "/"); err2 == nil {
			snap.Disk = DiskMetrics{
				Path:        "/",
				TotalBytes:  duRoot.Total,
				UsedBytes:   duRoot.Used,
				FreeBytes:   duRoot.Free,
				UsedPercent: duRoot.UsedPercent,
			}
		}
	} else {
		snap.Disk = DiskMetrics{
			Path:        diskTarget,
			TotalBytes:  du.Total,
			UsedBytes:   du.Used,
			FreeBytes:   du.Free,
			UsedPercent: du.UsedPercent,
		}
	}

	if counters, err := gopsnet.IOCountersWithContext(ctx, false); err == nil && len(counters) > 0 {
		snap.Network = NetMetrics{
			BytesSent: counters[0].BytesSent,
			BytesRecv: counters[0].BytesRecv,
		}
	}

	snap.Directories = []DirStatus{
		{Key: "server", Label: "面板与运行时", Path: paths.ServerDir, Exists: dirExists(paths.ServerDir)},
		{Key: "database", Label: "数据库目录", Path: paths.DatabaseDir, Exists: dirExists(paths.DatabaseDir)},
		{Key: "project", Label: "项目与站点", Path: paths.ProjectDir, Exists: dirExists(paths.ProjectDir)},
		{Key: "nginx", Label: "Nginx 服务", Path: paths.NginxDir, Exists: dirExists(paths.NginxDir)},
	}

	return snap
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
