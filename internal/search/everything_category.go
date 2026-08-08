package search

/*
//#cgo CFLAGS: -I${SRCDIR}/sdk/include -D_WIN32_WINNT=0x0600
#cgo CFLAGS: -I./sdk/include
#cgo LDFLAGS: -L../../ -lEverything64

#include <windows.h>
#include "Everything.h"
*/
import "C"
import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"
)

type CategoryDefinition struct {
	ID    string
	Label string
	Query string
}

var categoryDefinitions = []CategoryDefinition{
	{ID: "document", Label: "文档", Query: "ext:doc;docx;xls;xlsx;ppt;pptx;pdf;txt;rtf;odt;ods;odp"},
	{ID: "executable", Label: "可执行", Query: "ext:exe;msi;bat;cmd;com;scr"},
	{ID: "compressedPackage", Label: "压缩包", Query: "ext:zip;rar;7z;tar;gz;bz2;xz;iso"},
	{ID: "image", Label: "图片", Query: "ext:jpg;jpeg;png;gif;bmp;svg;webp;ico;tiff"},
	{ID: "video", Label: "视频", Query: "ext:mp4;avi;mkv;mov;wmv;flv;webm;m4v"},
	{ID: "audio", Label: "音频", Query: "ext:mp3;wav;flac;aac;ogg;wma;m4a"},
	{ID: "code", Label: "代码", Query: "ext:go;rs;py;js;ts;java;c;cpp;h;html;css;json;xml;yaml;toml;sh;pl;rb;php"},
	{ID: "shortcut", Label: "快捷方式", Query: "ext:lnk;url"},
	{ID: "folder", Label: "文件夹", Query: "folder:"},
}

func CategoryDefinitions() []CategoryDefinition {
	copied := make([]CategoryDefinition, len(categoryDefinitions))
	copy(copied, categoryDefinitions)
	return copied
}

func (e *EverythingClient) CategorySearch(keyword, category string, maxResults int) ([]ResultSearch, error) {
	ctx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()
	return e.categorySearchWithContext(ctx, keyword, category, maxResults)
}

func (e *EverythingClient) categorySearchWithContext(ctx context.Context, keyword string, category string, maxResults int) ([]ResultSearch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	searchQuery, err := buildCategorySearchQuery(keyword, category)
	if err != nil {
		return nil, err
	}

	if searchQuery == "" {
		return nil, nil
	}

	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	case <-ctx.Done():
		return nil, fmt.Errorf("category search canceled while waiting for concurrency token: %w", ctx.Err())
	}

	type queryResult struct {
		data []ResultSearch
		err  error
	}

	resultCh := make(chan queryResult, 1)

	go func() {
		e.mu.Lock()
		defer e.mu.Unlock()

		utf16Chars := utf16.Encode([]rune(searchQuery + "\x00"))
		C.Everything_SetSearchW((*C.WCHAR)(unsafe.Pointer(&utf16Chars[0])))

		C.Everything_SetMatchCase(C.BOOL(0))
		C.Everything_SetMatchPath(C.BOOL(0))
		C.Everything_SetRegex(C.BOOL(0))

		if maxResults > 0 {
			C.Everything_SetMax(C.DWORD(maxResults))
		} else {
			C.Everything_SetMax(C.DWORD(200))
		}

		requestFlags := C.EVERYTHING_REQUEST_FILE_NAME |
			C.EVERYTHING_REQUEST_PATH |
			C.EVERYTHING_REQUEST_FULL_PATH_AND_FILE_NAME |
			C.EVERYTHING_REQUEST_SIZE |
			C.EVERYTHING_REQUEST_DATE_MODIFIED |
			C.EVERYTHING_REQUEST_DATE_CREATED
		C.Everything_SetRequestFlags(C.DWORD(requestFlags))

		if C.Everything_QueryW(C.BOOL(1)) == C.FALSE {
			errCode := C.Everything_GetLastError()
			resultCh <- queryResult{err: fmt.Errorf("everything query failed, error code: %d", int(errCode))}
			return
		}

		numResults := C.Everything_GetNumResults()
		results := make([]ResultSearch, 0, numResults)

		var buf [32768]C.WCHAR

		for i := C.DWORD(0); i < numResults; i++ {
			C.Everything_GetResultFullPathNameW(i, &buf[0], 32768)
			fullPath := wcharToString(&buf[0])

			fileNamePtr := C.Everything_GetResultFileNameW(i)
			fileName := wcharToString(fileNamePtr)

			pathPtr := C.Everything_GetResultPathW(i)
			pathStr := wcharToString(pathPtr)

			isFolder := C.Everything_IsFolderResult(i) != 0

			var cSize C.LARGE_INTEGER
			var fileSize int64
			if C.Everything_GetResultSize(i, &cSize) != 0 {
				fileSize = int64(*((*int64)(unsafe.Pointer(&cSize))))
			}

			var cModTime C.FILETIME
			var modTime time.Time
			if C.Everything_GetResultDateModified(i, &cModTime) != 0 {
				modTime = fileTimeToTime(cModTime)
			}

			var cCreateTime C.FILETIME
			var createTime time.Time
			if C.Everything_GetResultDateCreated(i, &cCreateTime) != 0 {
				createTime = fileTimeToTime(cCreateTime)
			}

			results = append(results, ResultSearch{
				FullPath:     fullPath,
				FileName:     fileName,
				Path:         pathStr,
				Size:         fileSize,
				ModifiedTime: modTime,
				CreatedTime:  createTime,
				IsFolder:     isFolder,
			})
		}

		resultCh <- queryResult{data: results}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("everything search timeout or canceled: %w", ctx.Err())
	case res := <-resultCh:
		return res.data, res.err
	}
}

func buildCategorySearchQuery(keyword, category string) (string, error) {
	trimmedKeyword := strings.TrimSpace(keyword)

	// 用户未输入关键词：返回空查询，不视为错误
	if trimmedKeyword == "" {
		return "", nil
	}

	// 无分类筛选：直接返回关键词
	if category == "" {
		return trimmedKeyword, nil
	}

	// 遍历分类定义
	for _, def := range categoryDefinitions {
		if def.ID != category {
			continue
		}
		//if trimmedKeyword == "" {
		//	return def.Query, nil
		//}
		return fmt.Sprintf("%s %s", trimmedKeyword, def.Query), nil
	}

	// 只有“传入的分类ID在系统中找不到”才属于真正的程序错误
	return "", fmt.Errorf("unknown category: %s", category)
}
