// Package ipc implements the small length-prefixed protocol spoken over the
// Unix domain socket between rc-agentd (privileged system daemon, has no
// access to the child's graphical session) and rc-helper (runs inside the
// child's X11 user session, so it can take screenshots and show overlays).
//
// Frame wire format:
//
//	[1 byte type][4 bytes BE id length][id bytes][4 bytes BE payload length][payload bytes]
//
// For all types except *Data/*Frame the payload is JSON. For image-carrying
// types the payload is raw JPEG bytes.
package ipc

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

type Type byte

const (
	MsgHello             Type = 1  // helper -> agentd: {"token": "..."}
	MsgHelloAck          Type = 2  // agentd -> helper: {"ok": true}
	MsgScreenshotRequest Type = 3  // agentd -> helper: {}
	MsgScreenshotData    Type = 4  // helper -> agentd: raw JPEG bytes
	MsgStreamStart       Type = 5  // agentd -> helper: {"fps": 2, "quality": 60}
	MsgStreamStop        Type = 6  // agentd -> helper: {}
	MsgStreamFrame       Type = 7  // helper -> agentd: raw JPEG bytes
	MsgNotify            Type = 8  // agentd -> helper: {"message": "..."}
	MsgNotifyAck         Type = 9  // helper -> agentd: {}
	MsgShutdownOverlay   Type = 10 // agentd -> helper: {"seconds": 300, "message": "..."}
	MsgShutdownCancel    Type = 11 // agentd -> helper: {}
	MsgWindowPIDs        Type = 12 // helper -> agentd: {"pids": [123, 456]}
	MsgError             Type = 13 // either direction: {"error": "..."}
)

const maxPayload = 64 << 20 // 64MiB safety cap (screenshots are small, this just bounds a malformed frame)

// Frame is one decoded protocol message.
type Frame struct {
	Type    Type
	ID      string
	Payload []byte
}

// WriteFrame encodes and writes a single frame to w.
func WriteFrame(w io.Writer, f Frame) error {
	if len(f.ID) > 0xFFFF {
		return fmt.Errorf("ipc: id too long")
	}
	if len(f.Payload) > maxPayload {
		return fmt.Errorf("ipc: payload too large")
	}
	buf := make([]byte, 0, 1+4+len(f.ID)+4+len(f.Payload))
	buf = append(buf, byte(f.Type))
	var idLen [4]byte
	binary.BigEndian.PutUint32(idLen[:], uint32(len(f.ID)))
	buf = append(buf, idLen[:]...)
	buf = append(buf, f.ID...)
	var payLen [4]byte
	binary.BigEndian.PutUint32(payLen[:], uint32(len(f.Payload)))
	buf = append(buf, payLen[:]...)
	buf = append(buf, f.Payload...)
	_, err := w.Write(buf)
	return err
}

// ReadFrame reads and decodes a single frame from r.
func ReadFrame(r *bufio.Reader) (Frame, error) {
	var f Frame
	tb, err := r.ReadByte()
	if err != nil {
		return f, err
	}
	f.Type = Type(tb)

	var idLenB [4]byte
	if _, err := io.ReadFull(r, idLenB[:]); err != nil {
		return f, err
	}
	idLen := binary.BigEndian.Uint32(idLenB[:])
	if idLen > 0xFFFF {
		return f, fmt.Errorf("ipc: id length %d out of range", idLen)
	}
	if idLen > 0 {
		idBuf := make([]byte, idLen)
		if _, err := io.ReadFull(r, idBuf); err != nil {
			return f, err
		}
		f.ID = string(idBuf)
	}

	var payLenB [4]byte
	if _, err := io.ReadFull(r, payLenB[:]); err != nil {
		return f, err
	}
	payLen := binary.BigEndian.Uint32(payLenB[:])
	if payLen > maxPayload {
		return f, fmt.Errorf("ipc: payload length %d exceeds max", payLen)
	}
	if payLen > 0 {
		f.Payload = make([]byte, payLen)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return f, err
		}
	}
	return f, nil
}

// JSON marshals v and returns it as a payload.
func JSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

type HelloPayload struct {
	Token string `json:"token"`
}

type HelloAckPayload struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type StreamStartPayload struct {
	FPS     int `json:"fps"`
	Quality int `json:"quality"`
}

type NotifyPayload struct {
	Message string `json:"message"`
}

type ShutdownOverlayPayload struct {
	Seconds int    `json:"seconds"`
	Message string `json:"message"`
}

type WindowPIDsPayload struct {
	PIDs []int32 `json:"pids"`
}

type ErrorPayload struct {
	Error string `json:"error"`
}
