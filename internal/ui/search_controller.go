package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"wind/internal/search"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// Search interaction and scheduling lives separately from window construction.
func (w *mainWindow) submitSearch(keyword string) {
	trimmed := strings.TrimSpace(keyword)
	w.keyword = trimmed
	w.setSelectedCategory("")
	if trimmed == "" {
		w.clearSearchResults()
		return
	}
	w.scheduleSearch(trimmed, "", false)
}

func (w *mainWindow) selectCategory(id widget.ListItemID) {
	if w.suppressCategorySelect || id < 0 || id >= len(w.categories) {
		return
	}
	w.scheduleSearch(strings.TrimSpace(w.entry.Text), w.categories[id].ID, true)
}

func (w *mainWindow) scheduleSearch(keyword, category string, immediate bool) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		w.clearSearchResults()
		return
	}
	w.pendingSearchKeyword, w.pendingSearchCategory = keyword, category
	if immediate || w.lastSearchDispatchTime.IsZero() || time.Since(w.lastSearchDispatchTime) >= searchCoalesceDelay {
		w.cancelScheduledSearch()
		w.executeSearch(keyword, category)
		return
	}

	delay := searchCoalesceDelay - time.Since(w.lastSearchDispatchTime)
	w.searchDispatchTicket++
	ticket := w.searchDispatchTicket
	if w.searchDispatchTimer != nil {
		w.searchDispatchTimer.Stop()
	}
	w.searchDispatchTimer = time.AfterFunc(delay, func() {
		fyne.Do(func() {
			if ticket != w.searchDispatchTicket {
				return
			}
			w.searchDispatchTimer = nil
			w.executeSearch(w.pendingSearchKeyword, w.pendingSearchCategory)
		})
	})
}

func (w *mainWindow) cancelScheduledSearch() {
	w.searchDispatchTicket++
	if w.searchDispatchTimer != nil {
		w.searchDispatchTimer.Stop()
		w.searchDispatchTimer = nil
	}
	w.pendingSearchKeyword, w.pendingSearchCategory = "", ""
}

func (w *mainWindow) clearSearchResults() {
	w.cancelScheduledSearch()
	w.searchSeq.Add(1)
	w.results = nil
	w.list.UnselectAll()
	w.list.Refresh()
	w.status.SetText(defaultSearchStatus)
}

func (w *mainWindow) executeSearch(keyword, category string) {
	keyword = strings.TrimSpace(keyword)
	seq := w.searchSeq.Add(1)
	w.lastSearchDispatchTime = time.Now()
	w.keyword, w.activeCategory = keyword, category
	w.list.UnselectAll()

	if category == "" {
		if w.callbacks.Search == nil {
			w.status.SetText(searchNotConfigured)
			return
		}
		if w.keyword == "" {
			w.clearSearchResults()
			return
		}
		w.status.SetText(fmt.Sprintf("正在搜索: %s", w.keyword))
		searchKeyword := w.keyword
		go func() {
			results, err := w.callbacks.Search(searchKeyword)
			fyne.Do(func() { w.applySearchResults(seq, category, results, err) })
		}()
		return
	}

	if w.callbacks.CategorySearch == nil {
		w.status.SetText(categoryNotConfigured)
		return
	}
	w.status.SetText(w.searchStatusText(category))
	searchKeyword := w.keyword
	go func() {
		results, err := w.callbacks.CategorySearch(searchKeyword, category)
		fyne.Do(func() { w.applySearchResults(seq, category, results, err) })
	}()
}

func (w *mainWindow) applySearchResults(seq uint64, category string, results []ResultItem, err error) {
	if seq != w.searchSeq.Load() {
		return
	}
	w.results = results
	w.list.Refresh()
	if err != nil {
		switch {
		case errors.Is(err, search.ErrDatabaseLoading):
			w.status.SetText("Everything 正在建立索引，完成后即可搜索")
		case errors.Is(err, context.DeadlineExceeded):
			w.status.SetText("Everything 响应超时，请稍后重试")
		case errors.Is(err, search.ErrIPCUnavailable):
			w.status.SetText("正在启动 Everything 搜索服务，请稍后重试")
		default:
			w.status.SetText("搜索失败: " + err.Error())
		}
		return
	}
	w.status.SetText(w.resultStatusText(category, len(results)))
}

func (w *mainWindow) searchStatusText(category string) string {
	label := w.categoryLabel(category)
	if w.keyword == "" {
		return fmt.Sprintf("在 %s 中搜索", label)
	}
	return fmt.Sprintf("在 %s 中搜索: %s", label, w.keyword)
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
