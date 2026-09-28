package ui

import (
	"bytes"
	"context"
	"image/png"
	"math"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"wind/internal/icon"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type SearchFunc func(keyword string) ([]ResultItem, error)
type CategorySearchFunc func(keyword, category string) ([]ResultItem, error)
type OpenFunc func(itemFullPath string)
type OpenWithFunc func(useAppName string, args ...string)
type TogglePinFunc func(item ResultItem) bool
type GetPinDisplayNameFunc func(path string) (string, bool)
type GetPinnedItemsFunc func() []ResultItem
type MovePinnedItemFunc func(path string, targetIndex int) bool
type PinnedIconsNumFunc func() int

type UpdateConfigFunc func(oldPath, newPath string) error
type GetSettingsFunc func() Settings
type SaveSettingsFunc func(Settings) error
type CheckUpdatesFunc func(context.Context) (UpdateStatus, error)

type SearchCategory struct {
	ID    string
	Label string
}

type Callbacks struct {
	Search            SearchFunc
	CategorySearch    CategorySearchFunc
	Open              OpenFunc
	OpenWith          OpenWithFunc
	TogglePin         TogglePinFunc
	GetPinDisplayName GetPinDisplayNameFunc
	GetPinnedItems    GetPinnedItemsFunc
	MovePinnedItem    MovePinnedItemFunc
	PinnedIconsNum    PinnedIconsNumFunc
	UpdateConfig      UpdateConfigFunc
	GetSettings       GetSettingsFunc
	SaveSettings      SaveSettingsFunc
	CheckUpdates      CheckUpdatesFunc
	Hide              func()
	Quit              func()
}

type WindowOptions struct {
	Title         string
	Width         float32
	Height        float32
	HideOnOpen    bool
	Categories    []SearchCategory
	ShowCharacter bool
}

type MainWindow interface {
	SetCallbacks(callbacks Callbacks)
	Show()
	Hide()
	Toggle()
	IsVisible() bool
	FocusSearch()
	ApplySettings(Settings)
	Window() fyne.Window
}

type mainWindow struct {
	ctx                        context.Context
	window                     fyne.Window
	callbacks                  Callbacks
	entry                      *widget.Entry
	iconEngine                 *icon.Engine
	pinnedIcons                *fyne.Container
	pinnedScroll               *container.Scroll
	updatePinnedScrollControls func()
	pinnedDropIndicator        *canvas.Rectangle
	pinnedItems                []ResultItem
	pinnedPanel                *fyne.Container
	categoryList               *widget.List
	categories                 []SearchCategory
	suppressCategorySelect     bool
	list                       *widget.List
	status                     *widget.Label
	results                    []ResultItem
	keyword                    string
	activeCategory             string
	visible                    atomic.Bool
	searchSeq                  atomic.Uint64
	searchDispatchTimer        *time.Timer
	searchDispatchTicket       uint64
	pendingSearchKeyword       string
	pendingSearchCategory      string
	lastSearchDispatchTime     time.Time // 最后一次搜索发送的时间
	hideOnOpen                 bool
	suppressOpenOnSelect       bool
	showCharacter              bool
	Width                      float32
	Height                     float32
}

const (
	searchCoalesceDelay = 50 * time.Millisecond

	pinnedIconSourceSize  = icon.SizePlugin
	pinnedIconImageHeight = 40
	pinnedButtonHeight    = 52
	pinnedIconImageWidth  = 48
	pinnedIconLabelWidth  = 80
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
		ctx:           ctx,
		window:        window,
		status:        widget.NewLabel(defaultSearchStatus),
		hideOnOpen:    opts.HideOnOpen,
		categories:    normalizeCategories(opts.Categories),
		Width:         opts.Width,
		Height:        opts.Height,
		showCharacter: opts.ShowCharacter,
	}

	w.entry = widget.NewEntry()
	w.entry.SetPlaceHolder(placeholder)
	w.entry.Resize(fyne.NewSize(760, 48))

	w.entry.OnChanged = func(s string) {
		w.submitSearch(s)
	}

	//w.entry.OnSubmitted = w.submitSearch

	w.iconEngine = ie

	// 固定的图标
	w.pinnedIcons = container.NewHBox()
	w.pinnedDropIndicator = canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
	w.pinnedDropIndicator.Hide()

	pinnedScroll := container.NewHScroll(w.pinnedIcons)
	w.pinnedScroll = pinnedScroll
	pinnedContent := container.NewStack(pinnedScroll, w.pinnedDropIndicator)
	//pinnedScroll.SetMinSize(fyne.NewSize(0, pinnedIconImageHeight))

	var leftBtn, rightBtn *widget.Button

	updateArrowButtons := func() {
		maxOffset := pinnedScrollMaxOffset(
			pinnedScroll.Content.Size().Width,
			pinnedScroll.Size().Width,
		)
		if maxOffset == 0 {
			leftBtn.Disable()
			rightBtn.Disable()
			return
		}

		if pinnedScroll.Offset.X <= 0 {
			leftBtn.Disable()
		} else {
			leftBtn.Enable()
		}
		if pinnedScroll.Offset.X >= maxOffset {
			rightBtn.Disable()
		} else {
			rightBtn.Enable()
		}
	}

	// 创建左箭头按钮
	leftBtn = widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		scrollStep := pinnedScroll.Size().Width
		newOffset := pinnedScroll.Offset.X - scrollStep
		if newOffset < 0 {
			newOffset = 0
		}
		pinnedScroll.ScrollToOffset(fyne.NewPos(newOffset, pinnedScroll.Offset.Y))
		updateArrowButtons()
	})

	// 创建右箭头按钮
	rightBtn = widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		scrollStep := pinnedScroll.Size().Width
		maxOffset := pinnedScrollMaxOffset(pinnedScroll.Content.Size().Width, pinnedScroll.Size().Width)
		newOffset := pinnedScroll.Offset.X + scrollStep
		if newOffset > maxOffset {
			newOffset = maxOffset
		}
		pinnedScroll.ScrollToOffset(fyne.NewPos(newOffset, pinnedScroll.Offset.Y))
		updateArrowButtons()
	})

	// 初始禁用按钮（updateArrowButtons 会决定最终状态）
	leftBtn.Disable()
	rightBtn.Disable()

	// 监听滚动事件（鼠标拖拽、滚轮等）更新按钮状态
	pinnedScroll.OnScrolled = func(position fyne.Position) {
		updateArrowButtons()
	}
	w.updatePinnedScrollControls = updateArrowButtons

	// 构建最终面板：左右箭头 + 滚动区域
	w.pinnedPanel = container.NewBorder(
		nil,
		nil,
		leftBtn,
		rightBtn,
		pinnedContent,
	)

	// 初始更新按钮状态
	updateArrowButtons()
	w.pinnedPanel.Hide()

	// 展示分类列表
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

	// 添充列表中的内容
	w.list = widget.NewList(
		func() int {
			return len(w.results)
		},
		func() fyne.CanvasObject {
			return w.newResultListItem()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(w.results) {
				return
			}

			item := w.results[id]
			menuItem := obj.(*mouseMenuItemWidget)
			menuItem.item = &item
			menuItem.OnRename = func(oldPath, newPath string) {
				targetIndex := -1

				// 找到和重命名的选项的id
				for i := range w.results {
					if w.results[i].FullPath == oldPath {
						targetIndex = i
						w.results[i].FullPath = newPath
						w.results[i].FileName = filepath.Base(newPath)
						break
					}
				}

				// 反馈与选中
				if targetIndex != -1 {
					w.suppressOpenOnSelect = true
					w.list.Select(targetIndex)
					w.status.SetText("已重命名为: " + filepath.Base(newPath))
				} else {
					w.status.SetText("重命名成功，但列表中未找到原项")
				}
			}
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

	// 组织固定图标下方"分类提示"下方布局
	categoryPanel := container.NewBorder(
		widget.NewLabel("分类"),
		nil,
		nil,
		nil,
		w.categoryList,
	)

	// 组织固定图标下方"搜索结果"布局
	resultsPanel := container.NewHSplit(categoryPanel, w.list)
	resultsPanel.SetOffset(categoryPanelRatio)

	settingsButton := widget.NewButtonWithIcon("", theme.SettingsIcon(), w.showSettings)
	settingsButton.Importance = widget.LowImportance
	searchBar := container.NewBorder(nil, nil, nil, settingsButton, w.entry)
	top := container.NewVBox(searchBar, w.pinnedPanel)
	w.window.SetContent(container.NewBorder(top, w.status, nil, nil, resultsPanel))
	w.window.Resize(fyne.NewSize(opts.Width, opts.Height))
	//w.window.CenterOnScreen()
	w.window.SetCloseIntercept(w.Hide)
	w.window.SetOnClosed(func() {
		w.visible.Store(false)
	})

	app.Lifecycle().SetOnExitedForeground(func() {
		if w.IsVisible() {
			w.Hide()
		}
	})

	return w
}

func (w *mainWindow) newResultListItem() fyne.CanvasObject {
	name := widget.NewLabel("")
	name.TextStyle.Bold = true

	path := widget.NewLabel("")
	path.Wrapping = fyne.TextWrapOff

	pinButton := widget.NewButtonWithIcon("", theme.ContentAddIcon(), nil)
	pinButton.Importance = widget.LowImportance

	textBox := container.NewVBox(name, path)
	content := container.NewHBox(textBox, layout.NewSpacer(), pinButton)
	return newMouseMenuItemWidget(content, w.window, w.callbacks.OpenWith)
}

func (w *mainWindow) SetCallbacks(callbacks Callbacks) {
	w.callbacks = callbacks
	w.refreshPinnedItems()
}

//func (w *mainWindow) Show() {
//	w.visible.Store(true)
//	w.window.Show()
//	w.window.RequestFocus()
//	w.FocusSearch()
//}

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

func (w *mainWindow) ApplySettings(settings Settings) {
	w.hideOnOpen = settings.HideOnOpen
	w.showCharacter = settings.ShowCharacter
	w.refreshPinnedItems()
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
		fileName := ""
		if w.showCharacter {
			fileName = i.FileName
		}
		button := newPinnedIconButton(
			fileName,
			w.itemIconResource(i),
			&i,
			i.FullPath,
			w.window,
			func(useAppName string, args ...string) {
				if w.callbacks.OpenWith != nil {
					w.callbacks.OpenWith("explorer", "/select,", i.FullPath)
				}
				if w.hideOnOpen {
					w.Hide()
				}
			},
			func(it ResultItem) bool {
				if w.callbacks.TogglePin == nil {
					return false
				}
				pinned := w.callbacks.TogglePin(it)

				w.refreshPinnedItems()
				w.list.Refresh()
				return pinned
			},
			func(oldPath, newPath string) {
				// 更新配置文件中的路径信息
				_ = w.callbacks.UpdateConfig(oldPath, newPath)
				// 固定项重命名成功后的处理
				w.refreshPinnedItems()
				w.list.Refresh()
			},
			func() {
				w.openItem(i)
			},
			func(steps int) {
				w.movePinnedItem(i.FullPath, steps)
			},
			func(dragX float32) {
				w.showPinnedDropIndicator(i.FullPath, dragX)
			},
			w.hidePinnedDropIndicator,
		)
		objects = append(objects, button)
	}

	w.pinnedIcons.Objects = objects
	w.pinnedIcons.Refresh()
	w.hidePinnedDropIndicator()

	if len(objects) == 0 {
		w.pinnedPanel.Hide()
	} else {
		w.pinnedPanel.Show()
	}
	w.pinnedPanel.Refresh()
	w.refreshPinnedScrollControls()
}

func pinnedScrollMaxOffset(contentWidth, viewportWidth float32) float32 {
	if contentWidth <= viewportWidth {
		return 0
	}
	return contentWidth - viewportWidth
}

func (w *mainWindow) refreshPinnedScrollControls() {
	if w.updatePinnedScrollControls == nil {
		return
	}
	// HBox 尺寸会在当前刷新周期的布局阶段更新；延迟到下一次 UI 刷新后再读取，
	// 才能得到新增、删除固定项后的真实内容宽度。
	fyne.Do(w.updatePinnedScrollControls)
}

func (w *mainWindow) showPinnedDropIndicator(path string, dragX float32) {
	if w.pinnedDropIndicator == nil || w.pinnedScroll == nil || len(w.pinnedItems) == 0 {
		return
	}

	sourceIndex := -1
	for i, item := range w.pinnedItems {
		if item.FullPath == path {
			sourceIndex = i
			break
		}
	}
	if sourceIndex < 0 || sourceIndex >= len(w.pinnedIcons.Objects) {
		return
	}

	width := w.pinnedIcons.Objects[sourceIndex].Size().Width
	if width <= 0 {
		return
	}
	steps := int(math.Round(float64(dragX / width)))
	targetIndex := sourceIndex + steps
	if targetIndex < 0 {
		targetIndex = 0
	}
	if targetIndex >= len(w.pinnedItems) {
		targetIndex = len(w.pinnedItems) - 1
	}

	// 向右移动时落点在线目标项右侧；向左移动时落点在线目标项左侧。
	boundaryIndex := targetIndex
	if targetIndex > sourceIndex {
		boundaryIndex++
	}
	boundaryX := float32(0)
	if boundaryIndex >= len(w.pinnedIcons.Objects) {
		last := w.pinnedIcons.Objects[len(w.pinnedIcons.Objects)-1]
		boundaryX = last.Position().X + last.Size().Width
	} else {
		boundaryX = w.pinnedIcons.Objects[boundaryIndex].Position().X
	}

	visibleX := boundaryX - w.pinnedScroll.Offset.X
	visibleX = fyne.Max(0, fyne.Min(visibleX, w.pinnedScroll.Size().Width))
	height := w.pinnedScroll.Size().Height
	if height <= 0 {
		return
	}
	w.pinnedDropIndicator.Move(fyne.NewPos(visibleX-1, 0))
	w.pinnedDropIndicator.Resize(fyne.NewSize(3, height))
	w.pinnedDropIndicator.Show()
	w.pinnedDropIndicator.Refresh()
}

func (w *mainWindow) hidePinnedDropIndicator() {
	if w.pinnedDropIndicator != nil {
		w.pinnedDropIndicator.Hide()
	}
}

func (w *mainWindow) movePinnedItem(path string, steps int) {
	if steps == 0 || w.callbacks.MovePinnedItem == nil {
		return
	}

	sourceIndex := -1
	for i, item := range w.pinnedItems {
		if item.FullPath == path {
			sourceIndex = i
			break
		}
	}
	if sourceIndex < 0 {
		return
	}

	targetIndex := sourceIndex + steps
	if targetIndex < 0 {
		targetIndex = 0
	}
	if targetIndex >= len(w.pinnedItems) {
		targetIndex = len(w.pinnedItems) - 1
	}
	if targetIndex == sourceIndex {
		return
	}
	if !w.callbacks.MovePinnedItem(path, targetIndex) {
		w.status.SetText("固定图标排序保存失败")
		return
	}

	w.refreshPinnedItems()
	w.list.Refresh()
	w.status.SetText("固定图标顺序已更新")
}

func (w *mainWindow) openSelected(id widget.ListItemID) {
	// 如果标志位为 true，立刻返回，不执行任何打开文件操作
	if w.suppressOpenOnSelect {
		w.suppressOpenOnSelect = false
		return
	}

	if id < 0 || id >= len(w.results) {
		return
	}

	item := w.results[id]
	w.list.UnselectAll()
	w.openItem(item)
}

func (w *mainWindow) openItem(item ResultItem) {
	if w.callbacks.Open != nil {
		w.callbacks.Open(item.FullPath)
	}
	if w.hideOnOpen {
		w.Hide()
	}
}

func (w *mainWindow) openWithItem(item ResultItem, appName string) {
	if w.callbacks.OpenWith != nil {
		w.callbacks.OpenWith(item.FullPath, appName)
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
