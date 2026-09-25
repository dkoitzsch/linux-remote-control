// Package screencap captures the X11 display rc-helper runs on and encodes
// it as JPEG. It relies on the process's DISPLAY/XAUTHORITY environment
// (set up by the systemd --user service / X11 session autostart) pointing
// at the child's actual session, not a headless display.
package screencap

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"

	"github.com/kbinani/screenshot"
)

// Capture grabs the primary display (screen index 0) and returns it as a
// JPEG at the given quality (1-100).
func Capture(quality int) ([]byte, error) {
	img, err := screenshot.CaptureDisplay(0)
	if err != nil {
		return nil, fmt.Errorf("screencap: capture: %w", err)
	}
	return encodeJPEG(img, quality)
}

func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("screencap: encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}
