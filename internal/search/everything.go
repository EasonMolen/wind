package search

/*
#cgo CFLAGS: -I${SRCDIR}/sdk/include -D_WIN32_WINNT=0x0600
#cgo LDFLAGS: -L${SRCDIR}/sdk/lib -lEverything64

#include <windows.h>
#include "Everything.h"
*/
import "C"
import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"
)

// ErrIPCUnavailable 表示 Everything 的 SDK DLL 已加载，但后台 Everything
// 进程尚未准备好接受 IPC 查询。
var ErrIPCUnavailable = errors.New("everything IPC is unavailable")

// ErrDatabaseLoading 表示 Everything 已响应 IPC，但其数据库仍在加载。
var ErrDatabaseLoading = errors.New("everything database is loading")

// EverythingClient 封装 Everything 客户端（线程安全）
type EverythingClient struct {
	mu  sync.Mutex
	sem chan struct{} // 信号量：限制并发执行 CGO 调用的最大线程数，防止 Everything 卡死导致 OS 线程耗尽
	*FilterEngine
}

var (
	instance *EverythingClient
	once     sync.Once
)

type EverythingService interface {
	Probe() error
	Search(keyword string) ([]ResultSearch, error)

	CategorySearch(keyword, category string) ([]ResultSearch, error)
	SetMaxResults(maxResults int)
}

// Probe checks whether Everything can answer IPC requests and whether its
// database is ready. Everything_IsDBLoaded distinguishes database loading
// from a missing Everything IPC endpoint through Everything_GetLastError.
func (e *EverythingClient) Probe() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if C.Everything_IsDBLoaded() != C.FALSE {
		return nil
	}

	errCode := C.Everything_GetLastError()
	switch errCode {
	case C.EVERYTHING_OK:
		return ErrDatabaseLoading
	case C.EVERYTHING_ERROR_IPC:
		return fmt.Errorf("%w (error code: %d)", ErrIPCUnavailable, int(errCode))
	default:
		return fmt.Errorf("everything readiness probe failed, error code: %d", int(errCode))
	}
}

func NewEverythingService(maxResults int) EverythingService {
	once.Do(func() {
		instance = &EverythingClient{
			sem: make(chan struct{}, 10),
		}
	})
	instance.SetMaxResults(maxResults)
	return instance
}

func (e *EverythingClient) SetMaxResults(maxResults int) {
	if maxResults <= 0 {
		maxResults = 50
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.FilterEngine = NewFilterEngine(maxResults)
}

// ResultSearch 对应 Everything 搜索出来的完整元数据
type ResultSearch struct {
	FullPath     string    // 文件或文件夹的完整路径
	FileName     string    // 纯文件名
	Path         string    // 纯目录路径
	Size         int64     // 文件大小（字节，如果是文件夹则通常为0）
	ModifiedTime time.Time // 修改时间
	CreatedTime  time.Time // 创建时间
	IsFolder     bool      // 是否是文件夹
	IsPathEntry  bool      // 是否来自用户/系统 PATH（仅用于排序，不展示）
	// IsApplicationEntry marks an App Paths registration or a Start Menu shortcut.
	// These entries must survive the generic system-directory noise filter.
	IsApplicationEntry bool
}

func (e *EverythingClient) Search(keyword string) ([]ResultSearch, error) {
	ctx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()
	return e.searchWithContext(ctx, keyword)
}

// searchWithContext 具备 Context 超时控制与线程泄露保护的查询接口
func (e *EverythingClient) searchWithContext(ctx context.Context, keyword string) ([]ResultSearch, error) {
	// 检查 Context 是否已逾期或取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 信号量控制：获取 CGO 执行令牌。如果达到了最大阈值，支持超时等待。
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	case <-ctx.Done():
		return nil, fmt.Errorf("search canceled while waiting for concurrency token: %w", ctx.Err())
	}

	// 必须使用容量为 1 的缓冲通道！
	// 防止 ctx 超时退出后，后台运行的 Goroutine 写入无缓冲 channel 导致永久挂起（协程泄露）
	type queryResult struct {
		data []ResultSearch
		err  error
	}
	resultCh := make(chan queryResult, 1)

	// 后台发起同步 CGO 阻塞调用
	go func() {
		// 必须加锁：Everything SDK 的状态是全局单例的
		e.mu.Lock()
		defer e.mu.Unlock()

		// 设置搜索的关键字
		utf16Chars := utf16.Encode([]rune(keyword + "\x00"))
		C.Everything_SetSearchW((*C.WCHAR)(unsafe.Pointer(&utf16Chars[0])))

		// 搜索基础配置：默认大小写不敏感、路径不敏感、不使用正则
		C.Everything_SetMatchCase(C.BOOL(0))
		C.Everything_SetMatchPath(C.BOOL(0))
		C.Everything_SetRegex(C.BOOL(0))

		// 设置结果数量限制
		// 先取得足够大的候选池，再由 FilterEngine 过滤、排序并截断。
		// 以前直接限制为 UI 的 50 条，C 盘结果很容易被系统和缓存目录占满，
		// 后续过滤后甚至会没有可展示的 C 盘内容。
		C.Everything_SetMax(C.DWORD(e.candidateLimit()))

		// 请求全部信息标志位：文件名、路径、全路径、大小、修改时间、创建时间
		requestFlags := C.EVERYTHING_REQUEST_FILE_NAME |
			C.EVERYTHING_REQUEST_PATH |
			C.EVERYTHING_REQUEST_FULL_PATH_AND_FILE_NAME |
			C.EVERYTHING_REQUEST_SIZE |
			C.EVERYTHING_REQUEST_DATE_MODIFIED |
			C.EVERYTHING_REQUEST_DATE_CREATED
		C.Everything_SetRequestFlags(C.DWORD(requestFlags))

		// 执行查询 (TRUE 代表同步阻塞等待 IPC 返回)
		if C.Everything_QueryW(C.BOOL(1)) == C.FALSE {
			errCode := C.Everything_GetLastError()
			if errCode == C.EVERYTHING_ERROR_IPC {
				resultCh <- queryResult{err: fmt.Errorf("%w (error code: %d)", ErrIPCUnavailable, int(errCode))}
				return
			}
			// 【已修复】原 string(rune(errCode)) 会转成非打印字符，改用 fmt.Errorf
			resultCh <- queryResult{err: fmt.Errorf("everything query failed, error code: %d", int(errCode))}
			return
		}

		// 获取结果总数
		numResults := C.Everything_GetNumResults()
		results := make([]ResultSearch, 0, numResults)

		var buf [32768]C.WCHAR

		// 遍历所有结果，文件和文件夹会同时被返回
		for i := C.DWORD(0); i < numResults; i++ {
			// 获取完整路径
			C.Everything_GetResultFullPathNameW(i, &buf[0], 32768)
			fullPath := wcharToString(&buf[0])

			// 直接获取文件名指针并转换
			fileNamePtr := C.Everything_GetResultFileNameW(i)
			fileName := wcharToString(fileNamePtr)

			// 直接获取路径指针并转换
			pathPtr := C.Everything_GetResultPathW(i)
			pathStr := wcharToString(pathPtr)

			// 判断文件夹
			isFolder := C.Everything_IsFolderResult(i) != 0

			// --- 文件大小 ---
			var cSize C.LARGE_INTEGER
			var fileSize int64
			if C.Everything_GetResultSize(i, &cSize) != 0 {
				fileSize = int64(*((*int64)(unsafe.Pointer(&cSize))))
			}

			// --- 修改时间 ---
			var cModTime C.FILETIME
			var modTime time.Time
			if C.Everything_GetResultDateModified(i, &cModTime) != 0 {
				modTime = fileTimeToTime(cModTime)
			}

			// --- 创建时间 ---
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

		resultCh <- queryResult{data: results, err: nil}
	}()

	// Select 监听查询结果或上游 Context 超时
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("everything search timeout or canceled: %w", ctx.Err())
	case res := <-resultCh:
		if res.err != nil {
			return nil, res.err
		}
		return e.filter(append(res.data, searchSupplementalLaunchers(keyword)...), keyword), nil
	}
}

func (e *EverythingClient) filter(results []ResultSearch, keyword string) []ResultSearch {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.FilterEngine.Filter(results, keyword)
}

func (e *EverythingClient) candidateLimit() int {
	const (
		defaultDisplayLimit = 50
		candidateMultiplier = 20
		maxCandidateLimit   = 1000
	)
	limit := e.maxResults
	if limit <= 0 {
		limit = defaultDisplayLimit
	}
	limit *= candidateMultiplier
	if limit > maxCandidateLimit {
		return maxCandidateLimit
	}
	return limit
}

// 辅助函数：C.WCHAR 指针转 Go string
func wcharToString(p *C.WCHAR) string {
	if p == nil {
		return ""
	}
	pLen := 0
	for {
		seg := *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + uintptr(pLen)*2))
		if seg == 0 {
			break
		}
		pLen++
	}
	if pLen == 0 {
		return ""
	}
	slice := unsafe.Slice((*uint16)(unsafe.Pointer(p)), pLen)
	return string(utf16.Decode(slice))
}

// 辅助函数：Windows FILETIME 转 Go time.Time
func fileTimeToTime(ft C.FILETIME) time.Time {
	low := uint64(ft.dwLowDateTime)
	high := uint64(ft.dwHighDateTime)
	ns100 := (high << 32) | low
	if ns100 == 0 {
		return time.Time{}
	}

	const windowsToUnixEpochOffset = 116444736000000000
	if ns100 <= windowsToUnixEpochOffset {
		return time.Time{}
	}

	// 换算为总纳秒（适用于 2262 年以前的时间）
	unixNs := int64((ns100 - windowsToUnixEpochOffset) * 100)
	return time.Unix(0, unixNs)
}
