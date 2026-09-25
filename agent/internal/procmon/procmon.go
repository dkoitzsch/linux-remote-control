// Package procmon lists running processes with uptime, memory and a rough
// CPU percentage, and classifies each as a background system service
// (systemd system.slice) or not. Whether a process owns a GUI window is
// decided by the caller by merging in PIDs reported by rc-helper (X11 has
// no meaning to a privileged system daemon with no session access).
package procmon

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/model"
)

type sample struct {
	totalCPU float64
	at       time.Time
}

type Monitor struct {
	mu   sync.Mutex
	prev map[int32]sample
}

func New() *Monitor {
	return &Monitor{prev: make(map[int32]sample)}
}

// List returns a snapshot of all processes visible to the agent (it runs as
// root, so this includes every user's processes on the machine).
func (m *Monitor) List(ctx context.Context) ([]model.Process, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	m.mu.Lock()
	prev := m.prev
	m.mu.Unlock()
	next := make(map[int32]sample, len(procs))

	out := make([]model.Process, 0, len(procs))
	for _, p := range procs {
		pid := p.Pid

		times, err := p.TimesWithContext(ctx)
		if err != nil {
			// Process likely exited between listing and inspection; skip it.
			continue
		}
		total := times.User + times.System

		var cpuPercent float64
		if ps, ok := prev[pid]; ok {
			if dt := now.Sub(ps.at).Seconds(); dt > 0 {
				cpuPercent = ((total - ps.totalCPU) / dt) * 100
				if cpuPercent < 0 {
					cpuPercent = 0
				}
			}
		}
		next[pid] = sample{totalCPU: total, at: now}

		name, _ := p.NameWithContext(ctx)
		if name == "" {
			continue
		}
		ppid, _ := p.PpidWithContext(ctx)
		createMs, _ := p.CreateTimeWithContext(ctx)
		startTime := time.UnixMilli(createMs)
		exe, _ := p.ExeWithContext(ctx)
		cmdline, _ := p.CmdlineWithContext(ctx)
		username, _ := p.UsernameWithContext(ctx)

		var rss uint64
		if mi, err := p.MemoryInfoWithContext(ctx); err == nil && mi != nil {
			rss = mi.RSS
		}

		out = append(out, model.Process{
			PID:        pid,
			PPID:       ppid,
			Name:       name,
			Exe:        exe,
			Cmdline:    cmdline,
			Username:   username,
			StartTime:  startTime,
			UptimeSec:  int64(now.Sub(startTime).Seconds()),
			RSSBytes:   rss,
			CPUPercent: roundTo(cpuPercent, 1),
			IsService:  isSystemService(pid),
		})
	}

	m.mu.Lock()
	m.prev = next
	m.mu.Unlock()

	return out, nil
}

func roundTo(v float64, decimals int) float64 {
	mult := 1.0
	for i := 0; i < decimals; i++ {
		mult *= 10
	}
	return float64(int(v*mult+0.5)) / mult
}

// isSystemService reports whether pid's cgroup places it under systemd's
// system.slice as a .service unit (a background/system process), as opposed
// to a user session process (user.slice) which is where interactive
// programs -- GUI or not -- normally live.
func isSystemService(pid int32) bool {
	data, err := os.ReadFile("/proc/" + itoa(pid) + "/cgroup")
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "system.slice") && strings.Contains(s, ".service")
}

func itoa(pid int32) string {
	if pid == 0 {
		return "0"
	}
	neg := pid < 0
	if neg {
		pid = -pid
	}
	var buf [12]byte
	i := len(buf)
	for pid > 0 {
		i--
		buf[i] = byte('0' + pid%10)
		pid /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
