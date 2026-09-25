// Command rc-overlay shows one fullscreen, always-on-top, non-dismissable
// GUI window: either a message that stays up until the child clicks
// "Verstanden", or a shutdown countdown. It is spawned as a short-lived
// child process by rc-helper for each request -- that keeps Fyne's
// main-thread/OpenGL requirements isolated to a single-purpose process
// instead of fighting rc-helper's own socket event loop for the main
// goroutine.
//
// Exit code 0 means the window ran its course (button clicked, or countdown
// reached zero). rc-helper kills this process (SIGTERM) to dismiss a
// shutdown countdown early on cancellation.
package main

import (
	"flag"
	"fmt"
	"image/color"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: rc-overlay notify --message TEXT | rc-overlay shutdown --seconds N --message TEXT")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "notify":
		fs := flag.NewFlagSet("notify", flag.ExitOnError)
		message := fs.String("message", "", "message to display")
		fs.Parse(os.Args[2:])
		runNotify(*message)
	case "shutdown":
		fs := flag.NewFlagSet("shutdown", flag.ExitOnError)
		seconds := fs.Int("seconds", 60, "countdown length in seconds")
		message := fs.String("message", "", "message to display above the countdown")
		fs.Parse(os.Args[2:])
		runShutdown(*seconds, *message)
	default:
		fmt.Fprintln(os.Stderr, "unknown subcommand:", os.Args[1])
		os.Exit(2)
	}
}

func fullscreenNonDismissableWindow(a fyne.App, title string) fyne.Window {
	w := a.NewWindow(title)
	w.SetFullScreen(true)
	w.SetPadded(false)
	w.SetCloseIntercept(func() {
		// Ignore close attempts (window manager X button, Alt+F4): the
		// window only goes away via the explicit action it's showing.
	})
	return w
}

func background(c color.Color) *canvas.Rectangle {
	r := canvas.NewRectangle(c)
	return r
}

func runNotify(message string) {
	if message == "" {
		message = "Nachricht vom Elternportal."
	}
	a := app.New()
	w := fullscreenNonDismissableWindow(a, "Nachricht")

	text := canvas.NewText(message, color.White)
	text.TextSize = 32
	text.Alignment = fyne.TextAlignCenter
	text.TextStyle = fyne.TextStyle{Bold: true}
	wrapped := widget.NewLabel(message)
	wrapped.Wrapping = fyne.TextWrapWord
	wrapped.Alignment = fyne.TextAlignCenter
	wrapped.TextStyle = fyne.TextStyle{Bold: true}

	button := widget.NewButton("Verstanden", func() { a.Quit() })
	button.Importance = widget.HighImportance

	content := container.NewCenter(container.NewVBox(
		layout.NewSpacer(),
		wrapped,
		widget.NewLabel(""),
		container.NewCenter(button),
		layout.NewSpacer(),
	))

	bg := background(color.NRGBA{R: 0x1b, G: 0x3a, B: 0x6b, A: 0xff})
	w.SetContent(container.NewStack(bg, content))
	w.ShowAndRun()
}

func runShutdown(seconds int, message string) {
	if seconds < 0 {
		seconds = 0
	}
	a := app.New()
	w := fullscreenNonDismissableWindow(a, "Herunterfahren geplant")

	headline := widget.NewLabel(message)
	headline.Wrapping = fyne.TextWrapWord
	headline.Alignment = fyne.TextAlignCenter
	headline.TextStyle = fyne.TextStyle{Bold: true}

	countdown := canvas.NewText(formatCountdown(seconds), color.White)
	countdown.TextSize = 64
	countdown.Alignment = fyne.TextAlignCenter
	countdown.TextStyle = fyne.TextStyle{Bold: true}

	content := container.NewCenter(container.NewVBox(
		layout.NewSpacer(),
		headline,
		widget.NewLabel(""),
		countdown,
		layout.NewSpacer(),
	))
	bg := background(color.NRGBA{R: 0x7a, G: 0x1a, B: 0x1a, A: 0xff})
	w.SetContent(container.NewStack(bg, content))

	go func() {
		remaining := seconds
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			remaining--
			if remaining <= 0 {
				fyne.Do(func() { countdown.Text = "Wird jetzt heruntergefahren…"; countdown.Refresh() })
				time.Sleep(1500 * time.Millisecond)
				a.Quit()
				return
			}
			r := remaining
			fyne.Do(func() { countdown.Text = formatCountdown(r); countdown.Refresh() })
		}
	}()

	w.ShowAndRun()
}

func formatCountdown(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
