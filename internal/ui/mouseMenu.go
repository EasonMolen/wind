package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

type mouseMenuItemWidget struct {
	widget.BaseWidget
	content  *fyne.Container
	itemPath string      // 当前项的路径，用于复制
	window   fyne.Window // 当前项的路径，用于复制
}

func newMouseMenuItemWidget(content *fyne.Container, win fyne.Window) *mouseMenuItemWidget {
	r := &mouseMenuItemWidget{
		content: content,
		window:  win,
	}
	r.ExtendBaseWidget(r)
	return r
}

// CreateRenderer 渲染内部包裹的内容
func (m *mouseMenuItemWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(m.content)
}

// TappedSecondary 捕获右键点击事件
func (m *mouseMenuItemWidget) TappedSecondary(pe *fyne.PointEvent) {
	// 创建右键菜单
	menu := fyne.NewMenu("",
		fyne.NewMenuItem("Copy Path", func() {
			// 点击复制时，将路径写入剪贴板
			m.window.Clipboard().SetContent(m.itemPath)
		}))

	// 在鼠标位置弹出菜单
	widget.ShowPopUpMenuAtPosition(menu, m.window.Canvas(), pe.AbsolutePosition)
}
