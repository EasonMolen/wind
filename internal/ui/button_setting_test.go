package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestPinnedIconDragMovesByWholeIconWidths(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	var got int
	button := newPinnedIconButton(
		"tool.exe", theme.FileIcon(), &ResultItem{}, `C:\tool.exe`, nil,
		nil, nil, nil, nil,
		func(steps int) { got = steps },
		nil, nil,
	)
	button.Resize(fyne.NewSize(pinnedIconImageWidth, pinnedButtonHeight))
	button.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(pinnedIconImageWidth*1.2, 0)})
	button.DragEnd()

	if got != 1 {
		t.Fatalf("drag steps = %d, want 1", got)
	}
}

func TestCompactPinnedLabelKeepsUsefulNameAndFileType(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "goland64.exe", want: "goland64"},
		{name: "a_very_long_pdf_document_name.pdf", want: "a_ver….pdf"},
		{name: "非常长的毕业论文最终提交版本.pdf", want: "非常长….pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compactPinnedLabel(tt.name); got != tt.want {
				t.Fatalf("compactPinnedLabel(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
