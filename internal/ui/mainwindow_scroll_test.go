package ui

import "testing"

func TestPinnedScrollMaxOffset(t *testing.T) {
	tests := []struct {
		name     string
		content  float32
		viewport float32
		want     float32
	}{
		{name: "content fits", content: 400, viewport: 400, want: 0},
		{name: "viewport is wider", content: 400, viewport: 480, want: 0},
		{name: "content overflows", content: 960, viewport: 400, want: 560},
	}

	for _, test := range tests {
		if got := pinnedScrollMaxOffset(test.content, test.viewport); got != test.want {
			t.Errorf("%s: pinnedScrollMaxOffset(%v, %v) = %v, want %v", test.name, test.content, test.viewport, got, test.want)
		}
	}
}
