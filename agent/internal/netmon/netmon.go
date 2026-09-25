// Package netmon attributes network traffic to individual processes,
// nethogs-style: it periodically maps each open local TCP/UDP port to the
// PID that owns it (via /proc/net/{tcp,udp}[6] socket inodes and /proc/*/fd
// symlinks), then sniffs traffic with libpcap and adds each packet's
// on-wire length to whichever local port(s) it touches.
//
// This needs CAP_NET_RAW (or root) and libpcap; the agent already runs as
// root as a systemd system service, so that's satisfied in normal
// deployment. If pcap is unavailable, New returns an error and the caller
// is expected to run without per-process network stats rather than fail
// the whole agent.
package netmon

import (
	"bufio"
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

type Stats struct {
	BytesSent  uint64
	BytesRecv  uint64
	LastActive time.Time
}

type Monitor struct {
	handle *pcap.Handle

	mu    sync.Mutex
	stats map[int32]*Stats

	portMu  sync.RWMutex
	tcpPort map[uint16]int32
	udpPort map[uint16]int32

	refreshInterval time.Duration
}

// New opens a live capture on all interfaces ("any", Linux-only pseudo
// device). Returns an error if pcap/libpcap isn't usable (e.g. missing
// permissions), which callers should treat as "network stats unavailable"
// rather than fatal.
func New() (*Monitor, error) {
	handle, err := pcap.OpenLive("any", 128, false, 500*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if err := handle.SetBPFFilter("tcp or udp"); err != nil {
		handle.Close()
		return nil, err
	}
	return &Monitor{
		handle:          handle,
		stats:           make(map[int32]*Stats),
		tcpPort:         make(map[uint16]int32),
		udpPort:         make(map[uint16]int32),
		refreshInterval: 2 * time.Second,
	}, nil
}

// Run drives the packet capture and periodic /proc port->PID refresh until
// ctx is cancelled. Call it in its own goroutine.
func (m *Monitor) Run(ctx context.Context) {
	go m.refreshLoop(ctx)
	m.captureLoop(ctx)
}

func (m *Monitor) Close() {
	if m.handle != nil {
		m.handle.Close()
	}
}

func (m *Monitor) refreshLoop(ctx context.Context) {
	m.refreshPortMap()
	t := time.NewTicker(m.refreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.refreshPortMap()
		}
	}
}

func (m *Monitor) captureLoop(ctx context.Context) {
	src := gopacket.NewPacketSource(m.handle, m.handle.LinkType())
	packets := src.Packets()
	for {
		select {
		case <-ctx.Done():
			return
		case pkt, ok := <-packets:
			if !ok {
				return
			}
			m.handlePacket(pkt)
		}
	}
}

func (m *Monitor) handlePacket(pkt gopacket.Packet) {
	length := uint64(pkt.Metadata().CaptureInfo.Length)
	if length == 0 {
		return
	}

	var srcPort, dstPort uint16
	var proto string
	if tcp := pkt.Layer(layers.LayerTypeTCP); tcp != nil {
		t := tcp.(*layers.TCP)
		srcPort, dstPort = uint16(t.SrcPort), uint16(t.DstPort)
		proto = "tcp"
	} else if udp := pkt.Layer(layers.LayerTypeUDP); udp != nil {
		u := udp.(*layers.UDP)
		srcPort, dstPort = uint16(u.SrcPort), uint16(u.DstPort)
		proto = "udp"
	} else {
		return
	}

	m.portMu.RLock()
	var portMap map[uint16]int32
	if proto == "tcp" {
		portMap = m.tcpPort
	} else {
		portMap = m.udpPort
	}
	sendPID, sendOK := portMap[srcPort]
	recvPID, recvOK := portMap[dstPort]
	m.portMu.RUnlock()

	now := time.Now()
	if sendOK {
		m.addStats(sendPID, length, 0, now)
	}
	if recvOK {
		m.addStats(recvPID, 0, length, now)
	}
}

func (m *Monitor) addStats(pid int32, sent, recv uint64, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.stats[pid]
	if !ok {
		s = &Stats{}
		m.stats[pid] = s
	}
	s.BytesSent += sent
	s.BytesRecv += recv
	s.LastActive = at
}

// Snapshot returns a copy of all currently known per-PID stats.
func (m *Monitor) Snapshot() map[int32]Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int32]Stats, len(m.stats))
	for pid, s := range m.stats {
		out[pid] = *s
	}
	return out
}

// refreshPortMap rebuilds the local-port -> PID maps from /proc.
func (m *Monitor) refreshPortMap() {
	inodeToPID, err := scanFDInodes()
	if err != nil {
		log.Printf("netmon: scanFDInodes: %v", err)
		return
	}

	tcpPorts := make(map[uint16]uint64)
	udpPorts := make(map[uint16]uint64)
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		parseProcNet(f, tcpPorts)
	}
	for _, f := range []string{"/proc/net/udp", "/proc/net/udp6"} {
		parseProcNet(f, udpPorts)
	}

	tcpPortToPID := make(map[uint16]int32, len(tcpPorts))
	for port, inode := range tcpPorts {
		if pid, ok := inodeToPID[inode]; ok {
			tcpPortToPID[port] = pid
		}
	}
	udpPortToPID := make(map[uint16]int32, len(udpPorts))
	for port, inode := range udpPorts {
		if pid, ok := inodeToPID[inode]; ok {
			udpPortToPID[port] = pid
		}
	}

	m.portMu.Lock()
	m.tcpPort = tcpPortToPID
	m.udpPort = udpPortToPID
	m.portMu.Unlock()
}

// scanFDInodes walks /proc/<pid>/fd/* to build a socket-inode -> PID map.
func scanFDInodes() (map[uint64]int32, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	out := make(map[uint64]int32)
	for _, e := range entries {
		pid64, err := strconv.ParseInt(e.Name(), 10, 32)
		if err != nil {
			continue // not a PID directory
		}
		pid := int32(pid64)
		fdDir := filepath.Join("/proc", e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // process exited or no permission
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inodeStr := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			inode, err := strconv.ParseUint(inodeStr, 10, 64)
			if err != nil {
				continue
			}
			out[inode] = pid
		}
	}
	return out, nil
}

// parseProcNet parses /proc/net/{tcp,udp}[6] lines, filling port -> inode.
// Line format (whitespace separated):
//
//	sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode ...
//	local_address is hex IP:PORT, e.g. 0100007F:1F90
func parseProcNet(path string, out map[uint16]uint64) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue // header line
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}
		localAddr := fields[1]
		colon := strings.LastIndexByte(localAddr, ':')
		if colon < 0 {
			continue
		}
		portHex := localAddr[colon+1:]
		port, err := strconv.ParseUint(portHex, 16, 16)
		if err != nil {
			continue
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil || inode == 0 {
			continue
		}
		out[uint16(port)] = inode
	}
}
