package ui

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"path/filepath"
	"strings"
	"sync/atomic"
	"wind/internal/icon"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type SearchFunc func(keyword string) []ResultItem
type CategorySearchFunc func(keyword, category string) []ResultItem
type OpenFunc func(item ResultItem)
type TogglePinFunc func(item ResultItem) bool
type GetPinDisplayNameFunc func(path string) (string, bool)
type GetPinnedItemsFunc func() []ResultItem
type PinnedIconsNumFunc func() int

type SearchCategory struct {
	ID    string
	Label string
}

type Callbacks struct {
	Search            SearchFunc
	CategorySearch    CategorySearchFunc
	Open              OpenFunc
	TogglePin         TogglePinFunc
	GetPinDisplayName GetPinDisplayNameFunc
	GetPinnedItems    GetPinnedItemsFunc
	PinnedIconsNum    PinnedIconsNumFunc
	Hide              func()
	Quit              func()
}

type WindowOptions struct {
	Title      string
	Width      float32
	Height     float32
	HideOnOpen bool
	Categories []SearchCategory
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
	ctx                    context.Context
	window                 fyne.Window
	callbacks              Callbacks
	entry                  *widget.Entry
	iconEngine             *icon.Engine
	pinnedIcons            *fyne.Container
	pinnedItems            []ResultItem
	pinnedPanel            *fyne.Container
	categoryList           *widget.List
	categories             []SearchCategory
	suppressCategorySelect bool
	list                   *widget.List
	status                 *widget.Label
	results                []ResultItem
	keyword                string
	activeCategory         string
	visible                atomic.Bool
	searchSeq              atomic.Uint64
	hideOnOpen             bool
}

const (
	pinnedIconSourceSize  = icon.SizePlugin
	pinnedIconImageHeight = 40
	pinnedIconImageWeight = 48
	pinnedIconRowHeight   = 48
	categoryPanelRatio    = 0.22
	defaultSearchStatus   = "按下回车搜索"
	categoryNotConfigured = "分类搜索回调没有配置"
	searchNotConfigured   = "搜索回调没有配置"
	placeholder           = "搜索文件,文件夹,应用..."
)

func NewMainWindow(ctx context.Context, app fyne.App, ie *icon.Engine, opts WindowOptions) MainWindow {
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
		ctx:        ctx,
		window:     window,
		status:     widget.NewLabel(defaultSearchStatus),
		hideOnOpen: opts.HideOnOpen,
		categories: normalizeCategories(opts.Categories),
	}

	w.entry = widget.NewEntry()
	w.entry.SetPlaceHolder(placeholder)
	w.entry.OnSubmitted = w.submitSearch

	w.iconEngine = ie
	w.pinnedIcons = container.NewHBox()

	pinnedScroll := container.NewHScroll(w.pinnedIcons)
	pinnedScroll.SetMinSize(fyne.NewSize(0, pinnedIconRowHeight))

	w.pinnedPanel = container.NewVBox(pinnedScroll)
	w.pinnedPanel.Hide()

	w.categoryList = widget.NewList(
		func() int {
			return len(w.categories)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(w.categories) {
				return
			}
			obj.(*widget.Label).SetText(w.categories[id].Label)
		},
	)
	w.categoryList.OnSelected = w.selectCategory
	w.setSelectedCategory("")

	w.list = widget.NewList(
		func() int {
			return len(w.results)
		},
		func() fyne.CanvasObject {
			return newResultListItem(w.window)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(w.results) {
				return
			}

			item := w.results[id]
			menuItem := obj.(*mouseMenuItemWidget)
			menuItem.itemPath = item.FullPath
			menuItem.OnTapped = func() {
				w.list.Select(id)
			}

			row := menuItem.content
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
				w.refreshPinnedItems()
				w.list.Refresh()
			}
		},
	)
	w.list.OnSelected = w.openSelected

	categoryPanel := container.NewBorder(
		widget.NewLabel("分类"),
		nil,
		nil,
		nil,
		w.categoryList,
	)
	resultsPanel := container.NewHSplit(categoryPanel, w.list)
	resultsPanel.SetOffset(categoryPanelRatio)

	top := container.NewVBox(w.entry, w.pinnedPanel)
	w.window.SetContent(container.NewBorder(top, w.status, nil, nil, resultsPanel))
	w.window.Resize(fyne.NewSize(opts.Width, opts.Height))
	w.window.CenterOnScreen()
	w.window.SetCloseIntercept(w.Hide)
	w.window.SetOnClosed(func() {
		w.visible.Store(false)
	})

	return w
}

func newResultListItem(win fyne.Window) fyne.CanvasObject {
	name := widget.NewLabel("")
	name.TextStyle.Bold = true

	path := widget.NewLabel("")
	path.Wrapping = fyne.TextWrapOff

	pinButton := widget.NewButtonWithIcon("", theme.ContentAddIcon(), nil)
	pinButton.Importance = widget.LowImportance

	textBox := container.NewVBox(name, path)
	content := container.NewHBox(textBox, layout.NewSpacer(), pinButton)
	return newMouseMenuItemWidget(content, win)
}

func (w *mainWindow) SetCallbacks(callbacks Callbacks) {
	w.callbacks = callbacks
	w.refreshPinnedItems()
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

func (w *mainWindow) refreshPinnedItems() {
	if w.pinnedIcons == nil || w.pinnedPanel == nil {
		return
	}

	if w.callbacks.GetPinnedItems == nil {
		w.pinnedItems = nil
		w.pinnedIcons.Objects = nil
		w.pinnedIcons.Refresh()
		w.pinnedPanel.Hide()
		w.pinnedPanel.Refresh()
		return
	}

	w.pinnedItems = w.callbacks.GetPinnedItems()
	objects := make([]fyne.CanvasObject, 0, len(w.pinnedItems))
	for _, item := range w.pinnedItems {
		i := item
		button := newPinnedIconButton(w.itemIconResource(i), func() {
			w.openItem(i)
		})
		objects = append(objects, button)
	}

	w.pinnedIcons.Objects = objects
	w.pinnedIcons.Refresh()

	if len(objects) == 0 {
		w.pinnedPanel.Hide()
	} else {
		w.pinnedPanel.Show()
	}
	w.pinnedPanel.Refresh()
}

func (w *mainWindow) submitSearch(keyword string) {
	trimmed := strings.TrimSpace(keyword)
	w.keyword = trimmed
	w.setSelectedCategory("")

	if trimmed == "" {
		w.results = nil
		w.list.UnselectAll()
		w.list.Refresh()
		w.status.SetText(defaultSearchStatus)
		return
	}

	w.executeSearch(trimmed, "")
}

func (w *mainWindow) selectCategory(id widget.ListItemID) {
	if w.suppressCategorySelect {
		return
	}
	if id < 0 || id >= len(w.categories) {
		return
	}

	w.executeSearch(strings.TrimSpace(w.entry.Text), w.categories[id].ID)
}

func (w *mainWindow) executeSearch(keyword, category string) {
	seq := w.searchSeq.Add(1)
	w.keyword = strings.TrimSpace(keyword)
	w.activeCategory = category
	w.list.UnselectAll()

	if category == "" {
		if w.callbacks.Search == nil {
			w.status.SetText(searchNotConfigured)
			return
		}

		if w.keyword == "" {
			w.results = nil
			w.list.Refresh()
			w.status.SetText(defaultSearchStatus)
			return
		}

		w.status.SetText(fmt.Sprintf("正在搜索: %s", w.keyword))
		go func() {
			results := w.callbacks.Search(w.keyword)
			fyne.Do(func() {
				w.applySearchResults(seq, category, results)
			})
		}()
		return
	}

	if w.callbacks.CategorySearch == nil {
		w.status.SetText(categoryNotConfigured)
		return
	}

	w.status.SetText(w.searchStatusText(category))
	go func() {
		results := w.callbacks.CategorySearch(w.keyword, category)
		fyne.Do(func() {
			w.applySearchResults(seq, category, results)
		})
	}()
}

func (w *mainWindow) applySearchResults(seq uint64, category string, results []ResultItem) {
	if seq != w.searchSeq.Load() {
		return
	}

	w.results = results
	w.list.Refresh()
	w.status.SetText(w.resultStatusText(category, len(results)))
}

func (w *mainWindow) searchStatusText(category string) string {
	categoryLabel := w.categoryLabel(category)
	if w.keyword == "" {
		return fmt.Sprintf("在 %s 中搜索", categoryLabel)
	}
	return fmt.Sprintf("在 %s 中搜索: %s", categoryLabel, w.keyword)
}

func (w *mainWindow) resultStatusText(category string, count int) string {
	if category == "" {
		return fmt.Sprintf("找到 %d 个结果", count)
	}
	return fmt.Sprintf("在 %s 中, 找到 %d 个结果", w.categoryLabel(category), count)
}

func (w *mainWindow) categoryLabel(category string) string {
	for _, item := range w.categories {
		if item.ID == category {
			return item.Label
		}
	}
	return category
}

func (w *mainWindow) setSelectedCategory(category string) {
	w.activeCategory = category
	index := 0
	for i, item := range w.categories {
		if item.ID == category {
			index = i
			break
		}
	}

	w.suppressCategorySelect = true
	w.categoryList.Select(index)
	w.suppressCategorySelect = false
}

func (w *mainWindow) openSelected(id widget.ListItemID) {
	if id < 0 || id >= len(w.results) {
		return
	}

	item := w.results[id]
	w.list.UnselectAll()
	w.openItem(item)
}

func (w *mainWindow) openItem(item ResultItem) {
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

func (w *mainWindow) itemIconResource(item ResultItem) fyne.Resource {
	if w.iconEngine == nil || item.FullPath == "" {
		return w.defaultItemIcon(item)
	}

	iconRes, err := w.iconEngine.Get(w.ctx, item.FullPath, pinnedIconSourceSize)
	if err != nil {
		ext := filepath.Ext(item.FullPath)
		if ext != "" {
			iconRes, err = w.iconEngine.GetByExtension(w.ctx, ext, pinnedIconSourceSize)
		}
	}
	if err != nil || iconRes == nil {
		return w.defaultItemIcon(item)
	}

	if len(iconRes.PNGBytes) > 0 {
		return fyne.NewStaticResource(filepath.Base(item.FullPath), iconRes.PNGBytes)
	}
	if iconRes.Image == nil {
		return w.defaultItemIcon(item)
	}

	var buf bytes.Buffer
	if err = png.Encode(&buf, iconRes.Image); err != nil {
		return w.defaultItemIcon(item)
	}
	return fyne.NewStaticResource(filepath.Base(item.FullPath), buf.Bytes())
}

func (w *mainWindow) defaultItemIcon(item ResultItem) fyne.Resource {
	if item.IsFolder {
		return theme.FolderIcon()
	}
	return theme.FileIcon()
}

type pinnedIconButton struct {
	widget.BaseWidget
	icon     *canvas.Image
	onTapped func()
}

func newPinnedIconButton(resource fyne.Resource, onTapped func()) *pinnedIconButton {
	iconImage := canvas.NewImageFromResource(resource)
	iconImage.FillMode = canvas.ImageFillContain
	iconImage.ScaleMode = canvas.ImageScaleSmooth
	iconImage.SetMinSize(fyne.NewSize(pinnedIconImageWeight, pinnedIconImageHeight))

	button := &pinnedIconButton{
		icon:     iconImage,
		onTapped: onTapped,
	}
	button.ExtendBaseWidget(button)
	return button
}

func (b *pinnedIconButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *pinnedIconButton) TappedSecondary(*fyne.PointEvent) {}

func (b *pinnedIconButton) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewCenter(b.icon))
}

func normalizeCategories(categories []SearchCategory) []SearchCategory {
	if len(categories) == 0 {
		return []SearchCategory{{ID: "", Label: "全部"}}
	}

	normalized := make([]SearchCategory, 0, len(categories)+1)
	hasAll := false
	for _, category := range categories {
		if strings.TrimSpace(category.Label) == "" {
			continue
		}
		if category.ID == "" {
			hasAll = true
		}
		normalized = append(normalized, category)
	}

	if len(normalized) == 0 {
		return []SearchCategory{{ID: "", Label: "全部"}}
	}
	if hasAll {
		return normalized
	}

	return append([]SearchCategory{{ID: "", Label: "全部"}}, normalized...)
}
