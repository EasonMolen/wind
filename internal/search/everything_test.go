package search

import (
	"fmt"
	"testing"
)

func TestEverythingSearch(t *testing.T) {
	client := NewEverythingService(10)

	// 测试搜索一个常见的关键字，例如 ".go" 或某个文件夹名
	results, err := client.Search(".go")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	fmt.Printf("Found %d results:\n", len(results))
	for idx, result := range results {
		itemType := "File"
		if result.IsFolder {
			itemType = "Folder"
		}
		fmt.Printf("[%d] [%s] %s (Size: %d bytes, Modified: %v)\n",
			idx, itemType, result.FullPath, result.Size, result.ModifiedTime)
	}
}
