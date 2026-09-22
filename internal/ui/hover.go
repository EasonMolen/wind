package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// 透明 hover 捕获层：实现 desktop.Hoverable 和 fyne.Tappable
type hoverCatcher struct {
	widget.BaseWidget
	onIn        func()
	onOut       func()
	onTapped    func()
	onSecondary func(*fyne.PointEvent)
}

func newHoverCatcher(onIn, onOut func(), onTapped func(), onSecondary func(event *fyne.PointEvent)) *hoverCatcher {
	h := &hoverCatcher{onIn: onIn, onOut: onOut, onTapped: onTapped, onSecondary: onSecondary}
	h.ExtendBaseWidget(h)
	return h
}

func (h *hoverCatcher) CreateRenderer() fyne.WidgetRenderer {
	// 用一个几乎全透明的矩形，只为占位和接收事件
	r := canvas.NewRectangle(color.Transparent)
	return widget.NewSimpleRenderer(r)
}

func (h *hoverCatcher) MouseIn(*desktop.MouseEvent) {
	if h.onIn != nil {
		h.onIn()
	}
}
func (h *hoverCatcher) MouseOut() {
	if h.onOut != nil {
		h.onOut()
	}
}

func (h *hoverCatcher) MouseMoved(*desktop.MouseEvent) {
	if h.onIn != nil {
		h.onIn()
	}
}

func (h *hoverCatcher) Tapped(*fyne.PointEvent) {
	if h.onTapped != nil {
		h.onTapped()
	}
}

func (h *hoverCatcher) TappedSecondary(pe *fyne.PointEvent) {
	if h.onSecondary != nil {
		h.onSecondary(pe)
	}

}

var _ desktop.Hoverable = (*hoverCatcher)(nil)
var _ fyne.Tappable = (*hoverCatcher)(nil)
var _ fyne.SecondaryTappable = (*hoverCatcher)(nil)
