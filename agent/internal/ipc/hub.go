package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var ErrHelperNotConnected = errors.New("ipc: rc-helper is not connected")

var idCounter uint64

func nextID() string {
	n := atomic.AddUint64(&idCounter, 1)
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), n)
}

// Hub is the agentd-side owner of the Unix socket that rc-helper connects to.
// Exactly one helper (the logged-in child's session) is expected; a new
// connection replaces any previous one.
type Hub struct {
	token string

	// OnHelperConnect, if set, is called after a new rc-helper connection
	// completes its handshake, before frames are processed. Used by the
	// server to re-push state (e.g. an in-progress shutdown overlay) that
	// the helper would otherwise miss if it reconnected mid-countdown.
	OnHelperConnect func()

	mu        sync.Mutex
	conn      *Conn
	connected bool

	pendingMu sync.Mutex
	pending   map[string]chan Frame

	streamMu      sync.Mutex
	streamSubs    map[int]chan []byte
	nextSubID     int
	streamActive  bool
	streamFPS     int
	streamQuality int

	windowPIDsMu sync.Mutex
	windowPIDs   []int32
}

func NewHub(token string) *Hub {
	return &Hub{
		token:      token,
		pending:    make(map[string]chan Frame),
		streamSubs: make(map[int]chan []byte),
	}
}

// Serve accepts connections from rc-helper until ctx is cancelled or the listener errors.
func (h *Hub) Serve(ctx context.Context, l net.Listener) error {
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	for {
		nc, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go h.handleConn(nc)
	}
}

func (h *Hub) handleConn(nc net.Conn) {
	c := NewConn(nc)
	defer c.Close()

	f, err := c.Recv()
	if err != nil || f.Type != MsgHello {
		return
	}
	var hp HelloPayload
	_ = json.Unmarshal(f.Payload, &hp)
	if hp.Token == "" || hp.Token != h.token {
		_ = c.Send(Frame{Type: MsgHelloAck, Payload: JSON(HelloAckPayload{OK: false, Error: "bad token"})})
		log.Printf("ipc: rejected helper connection: bad token")
		return
	}
	if err := c.Send(Frame{Type: MsgHelloAck, Payload: JSON(HelloAckPayload{OK: true})}); err != nil {
		return
	}

	h.mu.Lock()
	if h.conn != nil {
		h.conn.Close()
	}
	h.conn = c
	h.connected = true
	restartStream := h.streamActive
	fps, q := h.streamFPS, h.streamQuality
	h.mu.Unlock()
	log.Printf("ipc: rc-helper connected")

	if restartStream {
		_ = c.Send(Frame{Type: MsgStreamStart, Payload: JSON(StreamStartPayload{FPS: fps, Quality: q})})
	}
	if h.OnHelperConnect != nil {
		h.OnHelperConnect()
	}

	defer func() {
		h.mu.Lock()
		if h.conn == c {
			h.connected = false
			h.conn = nil
		}
		h.mu.Unlock()
		log.Printf("ipc: rc-helper disconnected")
	}()

	for {
		f, err := c.Recv()
		if err != nil {
			return
		}
		switch f.Type {
		case MsgScreenshotData, MsgNotifyAck, MsgError:
			h.deliver(f.ID, f)
		case MsgStreamFrame:
			h.broadcastFrame(f.Payload)
		case MsgWindowPIDs:
			var wp WindowPIDsPayload
			if err := json.Unmarshal(f.Payload, &wp); err == nil {
				h.windowPIDsMu.Lock()
				h.windowPIDs = wp.PIDs
				h.windowPIDsMu.Unlock()
			}
		}
	}
}

func (h *Hub) await(id string) chan Frame {
	ch := make(chan Frame, 1)
	h.pendingMu.Lock()
	h.pending[id] = ch
	h.pendingMu.Unlock()
	return ch
}

func (h *Hub) cancelAwait(id string) {
	h.pendingMu.Lock()
	delete(h.pending, id)
	h.pendingMu.Unlock()
}

func (h *Hub) deliver(id string, f Frame) {
	h.pendingMu.Lock()
	ch, ok := h.pending[id]
	if ok {
		delete(h.pending, id)
	}
	h.pendingMu.Unlock()
	if ok {
		ch <- f
	}
}

func (h *Hub) activeConn() (*Conn, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conn, h.connected
}

func (h *Hub) Connected() bool {
	_, ok := h.activeConn()
	return ok
}

// RequestScreenshot asks the helper for a single JPEG frame of the child's screen.
func (h *Hub) RequestScreenshot(ctx context.Context) ([]byte, error) {
	c, ok := h.activeConn()
	if !ok {
		return nil, ErrHelperNotConnected
	}
	id := nextID()
	ch := h.await(id)
	defer h.cancelAwait(id)
	if err := c.Send(Frame{Type: MsgScreenshotRequest, ID: id}); err != nil {
		return nil, err
	}
	select {
	case f := <-ch:
		if f.Type == MsgError {
			var ep ErrorPayload
			_ = json.Unmarshal(f.Payload, &ep)
			return nil, errors.New(ep.Error)
		}
		return f.Payload, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Notify asks the helper to show a blocking "message understood" popup.
// It returns once the helper confirms the popup is displayed (not once the
// child clicks it).
func (h *Hub) Notify(ctx context.Context, message string) error {
	c, ok := h.activeConn()
	if !ok {
		return ErrHelperNotConnected
	}
	id := nextID()
	ch := h.await(id)
	defer h.cancelAwait(id)
	if err := c.Send(Frame{Type: MsgNotify, ID: id, Payload: JSON(NotifyPayload{Message: message})}); err != nil {
		return err
	}
	select {
	case f := <-ch:
		if f.Type == MsgError {
			var ep ErrorPayload
			_ = json.Unmarshal(f.Payload, &ep)
			return errors.New(ep.Error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hub) ShutdownOverlay(seconds int, message string) error {
	c, ok := h.activeConn()
	if !ok {
		return ErrHelperNotConnected
	}
	return c.Send(Frame{Type: MsgShutdownOverlay, Payload: JSON(ShutdownOverlayPayload{Seconds: seconds, Message: message})})
}

func (h *Hub) ShutdownCancel() error {
	c, ok := h.activeConn()
	if !ok {
		return ErrHelperNotConnected
	}
	return c.Send(Frame{Type: MsgShutdownCancel})
}

// SubscribeStream registers a new live-view subscriber and (re)starts the
// helper's capture loop at the given rate if it wasn't already running.
func (h *Hub) SubscribeStream(fps, quality int) (int, chan []byte) {
	h.streamMu.Lock()
	id := h.nextSubID
	h.nextSubID++
	ch := make(chan []byte, 2)
	h.streamSubs[id] = ch
	starting := !h.streamActive
	if starting {
		h.streamActive = true
		h.streamFPS = fps
		h.streamQuality = quality
	}
	h.streamMu.Unlock()

	if starting {
		if c, ok := h.activeConn(); ok {
			_ = c.Send(Frame{Type: MsgStreamStart, Payload: JSON(StreamStartPayload{FPS: fps, Quality: quality})})
		}
	}
	return id, ch
}

func (h *Hub) UnsubscribeStream(id int) {
	h.streamMu.Lock()
	delete(h.streamSubs, id)
	stopping := len(h.streamSubs) == 0 && h.streamActive
	if stopping {
		h.streamActive = false
	}
	h.streamMu.Unlock()

	if stopping {
		if c, ok := h.activeConn(); ok {
			_ = c.Send(Frame{Type: MsgStreamStop})
		}
	}
}

func (h *Hub) broadcastFrame(data []byte) {
	h.streamMu.Lock()
	defer h.streamMu.Unlock()
	for _, ch := range h.streamSubs {
		select {
		case ch <- data:
		default:
		}
	}
}

// WindowPIDs returns the most recently reported set of PIDs owning a mapped
// top-level X11 window (i.e. GUI applications), as seen by the helper.
func (h *Hub) WindowPIDs() []int32 {
	h.windowPIDsMu.Lock()
	defer h.windowPIDsMu.Unlock()
	out := make([]int32, len(h.windowPIDs))
	copy(out, h.windowPIDs)
	return out
}
