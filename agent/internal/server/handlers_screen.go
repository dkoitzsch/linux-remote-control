package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/ipc"
)

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	jpeg, err := s.Hub.RequestScreenshot(ctx)
	if err != nil {
		if errors.Is(err, ipc.ErrHelperNotConnected) {
			http.Error(w, "rc-helper is not connected (is the child logged in?)", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "screenshot failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(jpeg)
}

const (
	defaultStreamFPS     = 2
	defaultStreamQuality = 60
	maxStreamFPS         = 10
)

// handleStream serves an MJPEG (multipart/x-mixed-replace) stream that a
// plain <img> tag can consume directly, sourced from rc-helper's periodic
// screen captures. This doubles as both "screenshot" and "video stream"
// requirements with a single mechanism.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !s.Hub.Connected() {
		http.Error(w, "rc-helper is not connected (is the child logged in?)", http.StatusServiceUnavailable)
		return
	}

	fps := defaultStreamFPS
	if v := r.URL.Query().Get("fps"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxStreamFPS {
			fps = n
		}
	}
	quality := defaultStreamQuality
	if v := r.URL.Query().Get("quality"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 10 && n <= 95 {
			quality = n
		}
	}

	const boundary = "rcframe"
	w.Header().Set("Content-Type", fmt.Sprintf("multipart/x-mixed-replace; boundary=%s", boundary))
	w.Header().Set("Cache-Control", "no-store")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	id, frames := s.Hub.SubscribeStream(fps, quality)
	defer s.Hub.UnsubscribeStream(id)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-frames:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", boundary, len(frame)); err != nil {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			if _, err := w.Write([]byte("\r\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
