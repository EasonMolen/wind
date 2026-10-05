package ui

import (
	"runtime"
	"wind/internal/window"
)

// Show displays the window at its configured normalized position. Windows
// placement uses the active monitor's work area; other drivers center it.
func (w *mainWindow) Show() {
	w.visible.Store(true)
	w.window.Show()

	if runtime.GOOS == "windows" {
		if err := window.MoveWindowToPosition(w.window, -1, w.PositionX, w.PositionY); err != nil {
			w.window.CenterOnScreen()
		}
	} else {
		w.window.CenterOnScreen()
	}

	w.window.RequestFocus()
	w.FocusSearch()
}
