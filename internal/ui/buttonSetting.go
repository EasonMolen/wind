package ui

import (
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
	bg        *canvas.Rectangle
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
) *pinnedIconButton {
	iconImage := canvas.NewImageFromResource(resource)
	iconImage.FillMode = canvas.ImageFillContain
	iconImage.ScaleMode = canvas.ImageScaleSmooth
	iconImage.SetMinSize(fyne.NewSize(pinnedIconImageWidth, pinnedIconImageHeight))

	var nameLabel *widget.Label
	if pinnedItemName != "" {
		nameLabel = widget.NewLabel(strings.TrimSuffix(pinnedItemName, filepath.Ext(pinnedItemName)))
		nameLabel.Alignment = fyne.TextAlignCenter
		nameLabel.Wrapping = fyne.TextWrapOff
		nameLabel.Truncation = fyne.TextTruncateClip
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
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *pinnedIconButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
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
		func() {
			if b.onTapped != nil {
				b.onTapped()
			}
		},
		// TappedSecondary（右键）
		func(pe *fyne.PointEvent) {
			ShowItemContextMenu(
				b.window,
				b.itemPath,
				b.openWith,
				func(oldPath, newPath string) {
					b.itemPath = newPath
					b.name = filepath.Base(newPath)
					b.nameLabel.SetText(b.name)
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

	return widget.NewSimpleRenderer(container.NewStack(b.bg, content, catcher))
}

func (b *pinnedIconButton) MinSize() fyne.Size {
	b.ExtendBaseWidget(b)
	iconSize := fyne.NewSize(pinnedIconImageWidth, pinnedButtonHeight)
	if b.nameLabel != nil {
		nameSize := b.nameLabel.MinSize()
		return fyne.NewSize(
			fyne.Max(iconSize.Width, nameSize.Width),
			iconSize.Height+nameSize.Height)
	}
	return iconSize

}

//func (b *pinnedIconButton) MinSize() fyne.Size {
//	b.ExtendBaseWidget(b)
//	if b.nameLabel == nil {
//		return fyne.NewSize(pinnedIconImageWidth, pinnedIconImageHeight)
//	}
//	return fyne.NewSize(pinnedIconImageWidth, pinnedIconRowHeight)
//}
