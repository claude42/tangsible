package uikit

import (
	"fmt"
	"time"

	"github.com/rivo/tview"
)

// AnimatePlaceholders gives every view in labels a busy-spinner placeholder
// (the same frames as the tree's active-task spinner, SpinnerAt) and
// keeps it turning for as long as the view still shows it. A view whose
// fetch has landed (any other text, including a cache hit set before the
// first tick) is dropped from the set; the ticker stops once none is left.
// All view access runs on the event loop, so no locking.
func AnimatePlaceholders(app *tview.Application, labels map[*tview.TextView]string) {
	start := time.Now()
	shown := map[*tview.TextView]string{}
	render := func(frame rune) {
		for v, label := range labels {
			if cur, ok := shown[v]; ok && v.GetText(false) != cur {
				delete(labels, v)
				delete(shown, v)
				continue
			}
			text := fmt.Sprintf("%c %s", frame, label)
			v.SetText(text)
			shown[v] = text
		}
	}
	frame := SpinnerAt(0)
	for v, label := range labels {
		text := fmt.Sprintf("%c %s", frame, label)
		v.SetText(text)
		shown[v] = text
	}
	go func() {
		ticker := time.NewTicker(SpinnerInterval)
		defer ticker.Stop()
		done := make(chan bool, 1)
		for range ticker.C {
			app.QueueUpdateDraw(func() {
				render(SpinnerAt(time.Since(start)))
				done <- len(labels) == 0
			})
			if <-done {
				return
			}
		}
	}()
}
