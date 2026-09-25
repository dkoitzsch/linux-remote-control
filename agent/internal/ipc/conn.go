package ipc

import (
	"bufio"
	"net"
	"sync"
)

// Conn wraps a net.Conn with framed, concurrency-safe writes and buffered reads.
// Reads are expected to happen from a single goroutine (see Recv).
type Conn struct {
	nc      net.Conn
	r       *bufio.Reader
	writeMu sync.Mutex
}

func NewConn(nc net.Conn) *Conn {
	return &Conn{nc: nc, r: bufio.NewReader(nc)}
}

func (c *Conn) Send(f Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return WriteFrame(c.nc, f)
}

func (c *Conn) Recv() (Frame, error) {
	return ReadFrame(c.r)
}

func (c *Conn) Close() error {
	return c.nc.Close()
}
