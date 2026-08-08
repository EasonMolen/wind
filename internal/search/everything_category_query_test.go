package search

import "testing"

func TestBuildCategorySearchQuery(t *testing.T) {
	tests := []struct {
		name     string
		keyword  string
		category string
		want     string
		wantErr  bool
	}{
		{name: "all results", keyword: "report", want: "report"},
		{name: "category with keyword", keyword: "report", category: "document", want: "report ext:doc;docx;xls;xlsx;ppt;pptx;pdf;txt;rtf;odt;ods;odp"},
		{name: "category without keyword", category: "folder", want: "folder:"},
		{name: "unknown category", keyword: "report", category: "missing", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCategorySearchQuery(tt.keyword, tt.category)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildCategorySearchQuery returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("buildCategorySearchQuery = %q, want %q", got, tt.want)
			}
		})
	}
}
