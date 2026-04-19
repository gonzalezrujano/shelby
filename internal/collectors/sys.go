package collectors

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"

	"shelby/internal/engine"
)

// SysStat reads a system metric selected by Step.Source.
//   - "cpu"             → percent (sampled 200ms), load1/5/15
//   - "memory" | "mem"  → percent, used, free, available, total
//   - "host" | "system" → hostname, os, platform, uptime, procs
//   - "/..." (path)     → disk usage at path (percentage_used, used, free, total)
type SysStat struct{}

func (SysStat) Execute(ctx context.Context, s engine.Step, rc *engine.RunContext) (engine.Output, error) {
	src := strings.TrimSpace(s.Source)
	var (
		data map[string]any
		err  error
	)
	switch {
	case src == "cpu":
		data, err = readCPU(ctx)
	case src == "memory" || src == "mem":
		data, err = readMem(ctx)
	case src == "host" || src == "system":
		data, err = readHost(ctx)
	case strings.HasPrefix(src, "/"):
		data, err = readDisk(ctx, src)
	default:
		err = fmt.Errorf("sys_stat: unknown source %q (use cpu|memory|host|<path>)", src)
	}
	if err != nil {
		return engine.Output{OK: false, Error: err.Error()}, err
	}
	return engine.Output{OK: true, Data: data}, nil
}

func readCPU(ctx context.Context) (map[string]any, error) {
	pct, err := cpu.PercentWithContext(ctx, 200*time.Millisecond, false)
	if err != nil {
		return nil, err
	}
	data := map[string]any{}
	if len(pct) > 0 {
		data["percent"] = pct[0]
	}
	if la, err := load.AvgWithContext(ctx); err == nil && la != nil {
		data["load1"] = la.Load1
		data["load5"] = la.Load5
		data["load15"] = la.Load15
	}
	return data, nil
}

func readMem(ctx context.Context) (map[string]any, error) {
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"percent":   v.UsedPercent,
		"used":      v.Used,
		"free":      v.Free,
		"available": v.Available,
		"total":     v.Total,
	}, nil
}

func readHost(ctx context.Context) (map[string]any, error) {
	h, err := host.InfoWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"hostname":         h.Hostname,
		"os":               h.OS,
		"platform":         h.Platform,
		"platform_version": h.PlatformVersion,
		"uptime":           h.Uptime,
		"procs":            h.Procs,
	}, nil
}

func readDisk(ctx context.Context, path string) (map[string]any, error) {
	u, err := disk.UsageWithContext(ctx, path)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"path":            u.Path,
		"percentage_used": u.UsedPercent,
		"used":            u.Used,
		"free":            u.Free,
		"total":           u.Total,
	}, nil
}
