package ui

import (
	"math"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type pinnedIconButton struct {
	widget.BaseWidget
	icon      *canvas.Image
	name      string
	nameLabel *widget.Label

	item      *ResultItem
	itemPath  string                        // 当前项路径
	window    fyne.Window                   // 用于弹窗和剪贴板
	openWith  OpenWithFunc                  // 资源管理器打开
	onRename  func(oldPath, newPath string) // 重命名成功回调
	onTapped  func()                        // 左键点击回调
	togglePin TogglePinFunc                 // 切换固定图标
	onMove    func(steps int)               // 水平拖拽后的排序回调
	onDrag    func(dragX float32)           // 拖拽过程中的落点提示
	onDragEnd func()                        // 拖拽结束时隐藏落点提示
	bg        *canvas.Rectangle
	dragX     float32
	dragged   bool
}

func newPinnedIconButton(
	pinnedItemName string,
	resource fyne.Resource,
	item *ResultItem,
	itemPath string,
	win fyne.Window,
	openWith OpenWithFunc,
	togglePin TogglePinFunc,
	onRename func(oldPath, newPath string),
	onTapped func(),
	onMove func(steps int),
	onDrag func(dragX float32),
	onDragEnd func(),
) *pinnedIconButton {
	iconImage := canvas.NewImageFromResource(resource)
	iconImage.FillMode = canvas.ImageFillContain
	iconImage.ScaleMode = canvas.ImageScaleSmooth
	iconImage.SetMinSize(fyne.NewSize(pinnedIconImageWidth, pinnedIconImageHeight))

	var nameLabel *widget.Label
	if pinnedItemName != "" {
		nameLabel = widget.NewLabel(compactPinnedLabel(pinnedItemName))
		nameLabel.Alignment = fyne.TextAlignCenter
		nameLabel.Wrapping = fyne.TextWrapOff
		nameLabel.Truncation = fyne.TextTruncateEllipsis
		nameLabel.TextStyle = fyne.TextStyle{Symbol: true}
	}

	b := &pinnedIconButton{
		icon:      iconImage,
		name:      pinnedItemName,
		nameLabel: nameLabel,
		item:      item,
		itemPath:  itemPath,
		window:    win,
		openWith:  openWith,
		onRename:  onRename,
		onTapped:  onTapped,
		togglePin: togglePin,
		onMove:    onMove,
		onDrag:    onDrag,
		onDragEnd: onDragEnd,
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *pinnedIconButton) Tapped(*fyne.PointEvent) {
	b.handleTapped()
}

func (b *pinnedIconButton) TappedSecondary(*fyne.PointEvent) {}

func (b *pinnedIconButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(theme.Color(theme.ColorNameBackground))

	objects := []fyne.CanvasObject{container.NewCenter(b.icon)}
	if b.nameLabel != nil {
		objects = append(objects, b.nameLabel)
	}
	content := container.NewVBox(objects...)

	catcher := newHoverCatcher(
		// MouseIn
		func() {
			b.bg.FillColor = theme.Color(theme.ColorNameHover)
			b.bg.Refresh()
		},
		// MouseOut
		func() {
			b.bg.FillColor = theme.Color(theme.ColorNameBackground)
			b.bg.Refresh()
		},
		// Tapped（左键）
		b.handleTapped,
		// TappedSecondary（右键）
		func(pe *fyne.PointEvent) {
			ShowItemContextMenu(
				b.window,
				b.itemPath,
				b.openWith,
				func(oldPath, newPath string) {
					b.itemPath = newPath
					b.name = filepath.Base(newPath)
					if b.nameLabel != nil {
						b.nameLabel.SetText(compactPinnedLabel(b.name))
					}
					if b.onRename != nil {
						b.onRename(oldPath, newPath)
					}
				},
				b.togglePin,
				pe.AbsolutePosition,
				b.item,
			)
		},
	)
	catcher.onDragged = b.Dragged
	catcher.onDragEnd = b.DragEnd

	return widget.NewSimpleRenderer(container.NewStack(b.bg, content, catcher))
}

func (b *pinnedIconButton) handleTapped() {
	if b.dragged {
		b.dragged = false // 释放拖拽时 Fyne 仍可能派发一次点击，忽略它。
		return
	}
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *pinnedIconButton) Dragged(event *fyne.DragEvent) {
	b.dragX += event.Dragged.DX
	if b.onDrag != nil {
		b.onDrag(b.dragX)
	}
}

func (b *pinnedIconButton) DragEnd() {
	if b.onDragEnd != nil {
		b.onDragEnd()
	}

	width := b.Size().Width
	if width <= 0 {
		b.dragX = 0
		return
	}

	steps := int(math.Round(float64(b.dragX / width)))
	b.dragX = 0
	if steps == 0 {
		return
	}
	b.dragged = true
	if b.onMove != nil {
		b.onMove(steps)
	}
}

func (b *pinnedIconButton) MinSize() fyne.Size {
	b.ExtendBaseWidget(b)
	iconSize := fyne.NewSize(pinnedIconImageWidth, pinnedButtonHeight)
	if b.nameLabel != nil {
		return fyne.NewSize(
			pinnedIconLabelWidth,
			iconSize.Height+b.nameLabel.MinSize().Height)
	}
	return iconSize

}

// compactPinnedLabel 让固定项既能保持同宽，又保留足够的识别信息。
// 可执行文件去除常见启动扩展名；普通文件保留扩展名，例如“毕业论文…pdf”。
func compactPinnedLabel(fileName string) string {
	const maxRunes = 10
	name := strings.TrimSpace(fileName)
	if name == "" {
		return ""
	}

	extension := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if isExecutableExtension(extension) || extension == "" {
		return compactRunes(base, maxRunes)
	}

	baseRunes := []rune(base)
	extensionRunes := []rune(extension)
	if len(baseRunes)+len(extensionRunes) <= maxRunes {
		return name
	}
	prefixLength := maxRunes - len(extensionRunes) - 1
	if containsWideRunes(baseRunes) && prefixLength > 3 {
		// CJK 字符的实际绘制宽度接近两倍英文；限制前缀以确保 .pdf 等类型仍可见。
		prefixLength = 3
	}
	if prefixLength < 1 {
		return compactRunes(name, maxRunes)
	}
	if prefixLength > len(baseRunes) {
		prefixLength = len(baseRunes)
	}
	return string(baseRunes[:prefixLength]) + "…" + extension
}

func containsWideRunes(runes []rune) bool {
	for _, r := range runes {
		if r > 127 {
			return true
		}
	}
	return false
}

func compactRunes(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}

func isExecutableExtension(extension string) bool {
	switch extension {
	case ".exe", ".com", ".bat", ".cmd", ".lnk", ".url":
		return true
	default:
		return false
	}
}

//func (b *pinnedIconButton) MinSize() fyne.Size {
//	b.ExtendBaseWidget(b)
//	if b.nameLabel == nil {
//		return fyne.NewSize(pinnedIconImageWidth, pinnedIconImageHeight)
//	}
//	return fyne.NewSize(pinnedIconImageWidth, pinnedIconRowHeight)
//}
