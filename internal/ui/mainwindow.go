package ui

import (
	"fmt"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type SearchFunc func(keyword string) []ResultItem
type OpenFunc func(item ResultItem)

type Callbacks struct {
	Search SearchFunc
	Open   OpenFunc
	Hide   func()
	Quit   func()
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

	w := &mainWindow{
		window:     app.NewWindow(opts.Title),
		status:     widget.NewLabel("输入关键字后按 Enter 搜索"),
		hideOnOpen: opts.HideOnOpen,
	}

	w.entry = widget.NewEntry()
	w.entry.SetPlaceHolder("搜索文件、目录或程序...")
	w.entry.OnSubmitted = w.submitSearch

	w.list = widget.NewList(
		func() int {
			return len(w.results)
		},
		func() fyne.CanvasObject {
			name := widget.NewLabel("")
			name.TextStyle.Bold = true
			path := widget.NewLabel("")
			path.Truncation = fyne.TextTruncateEllipsis
			return container.NewVBox(name, path)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(w.results) {
				return
			}

			item := w.results[id]
			box := obj.(*fyne.Container)
			name := box.Objects[0].(*widget.Label)
			path := box.Objects[1].(*widget.Label)

			name.SetText(displayName(item))
			path.SetText(item.FullPath)
		},
	)
	w.list.OnSelected = w.openSelected

	toolbar := container.NewHBox(
		widget.NewButton("搜索", func() { w.submitSearch(w.entry.Text) }),
		widget.NewButton("隐藏", w.Hide),
		widget.NewButton("退出", func() {
			if w.callbacks.Quit != nil {
				w.callbacks.Quit()
			}
		}),
	)

	w.window.SetContent(container.NewBorder(w.entry, container.NewVBox(w.status, toolbar), nil, nil, w.list))
	w.window.Resize(fyne.NewSize(opts.Width, opts.Height))
	w.window.CenterOnScreen()
	w.window.SetCloseIntercept(w.Hide)
	w.window.SetOnClosed(func() {
		w.visible.Store(false)
	})

	return w
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
		w.status.SetText("输入关键字后按 Enter 搜索")
		w.list.Refresh()
		return
	}

	if w.callbacks.Search == nil {
		w.status.SetText("搜索服务尚未初始化")
		return
	}

	w.status.SetText(fmt.Sprintf("正在搜索：%s", keyword))

	// 搜索服务可能会阻塞几秒，放到后台执行，避免卡住 Fyne UI 线程。
	go func() {
		results := w.callbacks.Search(keyword)
		fyne.Do(func() {
			if seq != w.searchSeq.Load() {
				return
			}

			w.results = results
			w.list.Refresh()
			w.status.SetText(fmt.Sprintf("找到 %d 个结果", len(results)))
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

func displayName(item ResultItem) string {
	if item.FileName != "" {
		return item.FileName
	}
	if item.FullPath != "" {
		return item.FullPath
	}
	return "(未知项目)"
}
