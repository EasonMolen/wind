package ui

import (
	"fmt"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type SearchFunc func(keyword string) []ResultItem
type OpenFunc func(item ResultItem)
type TogglePinFunc func(item ResultItem) bool

type GetPinDisplayNameFunc func(path string) (string, bool)

type Callbacks struct {
	Search            SearchFunc
	Open              OpenFunc
	TogglePin         TogglePinFunc
	GetPinDisplayName GetPinDisplayNameFunc
	Hide              func()
	Quit              func()
}

type WindowOptions struct {
	Title      string
	Width      float32
	Height     float32
	HideOnOpen bool
}

type MainWindow interface {
	SetCallbacks(callbacks Callbacks)
	Show()
	Hide()
	Toggle()
	IsVisible() bool
	FocusSearch()
	Window() fyne.Window
}

type mainWindow struct {
	window     fyne.Window
	callbacks  Callbacks
	entry      *widget.Entry
	list       *widget.List
	status     *widget.Label
	results    []ResultItem
	visible    atomic.Bool
	searchSeq  atomic.Uint64
	hideOnOpen bool
}

func NewMainWindow(app fyne.App, opts WindowOptions) MainWindow {
	if opts.Title == "" {
		opts.Title = "NewWind"
	}
	if opts.Width <= 0 {
		opts.Width = 760
	}
	if opts.Height <= 0 {
		opts.Height = 520
	}

	var window fyne.Window
	if drv, ok := app.Driver().(desktop.Driver); ok {
		window = drv.CreateSplashWindow()
	} else {
		window = app.NewWindow(opts.Title)
	}

	window.SetPadded(true)

	w := &mainWindow{
		window:     window,
		status:     widget.NewLabel("Press Enter to search"),
		hideOnOpen: opts.HideOnOpen,
	}

	w.entry = widget.NewEntry()
	w.entry.SetPlaceHolder("Search files, folders, or apps...")
	w.entry.OnSubmitted = w.submitSearch

	w.list = widget.NewList(
		func() int {
			return len(w.results)
		},
		func() fyne.CanvasObject {
			return newResultListItem()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(w.results) {
				return
			}

			item := w.results[id]
			row := obj.(*fyne.Container)
			textBox := row.Objects[0].(*fyne.Container)
			name := textBox.Objects[0].(*widget.Label)
			path := textBox.Objects[1].(*widget.Label)
			pinButton := row.Objects[2].(*widget.Button)

			name.SetText(displayName(item))
			path.SetText(item.FullPath)
			setPinButtonIcon(pinButton, w.isPinned(item.FullPath))
			pinButton.OnTapped = func() {
				if w.callbacks.TogglePin == nil {
					return
				}

				pinned := w.callbacks.TogglePin(item)
				setPinButtonIcon(pinButton, pinned)
				w.list.Refresh()
			}
		},
	)
	w.list.OnSelected = w.openSelected

	w.window.SetContent(container.NewBorder(w.entry, w.status, nil, nil, w.list))
	w.window.Resize(fyne.NewSize(opts.Width, opts.Height))
	w.window.CenterOnScreen()
	w.window.SetCloseIntercept(w.Hide)
	w.window.SetOnClosed(func() {
		w.visible.Store(false)
	})

	return w
}

func newResultListItem() fyne.CanvasObject {
	name := widget.NewLabel("")
	name.TextStyle.Bold = true

	path := widget.NewLabel("")
	path.Wrapping = fyne.TextWrapOff

	pinButton := widget.NewButtonWithIcon("", theme.ContentAddIcon(), nil)
	pinButton.Importance = widget.LowImportance

	textBox := container.NewVBox(name, path)
	return container.NewHBox(textBox, layout.NewSpacer(), pinButton)
}

func (w *mainWindow) SetCallbacks(callbacks Callbacks) {
	w.callbacks = callbacks
}

func (w *mainWindow) Show() {
	w.visible.Store(true)
	w.window.Show()
	w.window.RequestFocus()
	w.FocusSearch()
}

func (w *mainWindow) Hide() {
	w.visible.Store(false)
	w.window.Hide()
	if w.callbacks.Hide != nil {
		w.callbacks.Hide()
	}
}

func (w *mainWindow) Toggle() {
	if w.IsVisible() {
		w.Hide()
		return
	}
	w.Show()
}

func (w *mainWindow) IsVisible() bool {
	return w.visible.Load()
}

func (w *mainWindow) FocusSearch() {
	w.window.Canvas().Focus(w.entry)
}

func (w *mainWindow) Window() fyne.Window {
	return w.window
}

func (w *mainWindow) submitSearch(keyword string) {
	keyword = strings.TrimSpace(keyword)
	seq := w.searchSeq.Add(1)

	if keyword == "" {
		w.results = nil
		w.status.SetText("Press Enter to search")
		w.list.Refresh()
		return
	}

	if w.callbacks.Search == nil {
		w.status.SetText("Search callback is not configured")
		return
	}

	w.status.SetText(fmt.Sprintf("Searching: %s", keyword))

	go func() {
		results := w.callbacks.Search(keyword)
		fyne.Do(func() {
			if seq != w.searchSeq.Load() {
				return
			}

			w.results = results
			w.list.Refresh()
			w.status.SetText(fmt.Sprintf("Found %d result(s)", len(results)))
		})
	}()
}

func (w *mainWindow) openSelected(id widget.ListItemID) {
	if id < 0 || id >= len(w.results) {
		return
	}

	item := w.results[id]
	w.list.UnselectAll()

	if w.callbacks.Open != nil {
		w.callbacks.Open(item)
	}
	if w.hideOnOpen {
		w.Hide()
	}
}

func (w *mainWindow) isPinned(path string) bool {
	if w.callbacks.GetPinDisplayName == nil {
		return false
	}

	_, ok := w.callbacks.GetPinDisplayName(path)
	return ok
}

func displayName(item ResultItem) string {
	if item.FileName != "" {
		return item.FileName
	}
	if item.FullPath != "" {
		return item.FullPath
	}
	return "(unknown item)"
}

func setPinButtonIcon(button *widget.Button, pinned bool) {
	if pinned {
		button.SetIcon(theme.ContentRemoveIcon())
		return
	}
	button.SetIcon(theme.ContentAddIcon())
}
