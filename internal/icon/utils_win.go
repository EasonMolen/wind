package icon

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"sync"
	"syscall"
	"unsafe"
)

const (
	gdipStatusOK = 0
	sOK          = 0
)

// GUID matches the Windows GUID/CLSID binary layout.
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// GdiplusStartupInput is the native GDI+ startup configuration.
type GdiplusStartupInput struct {
	GdiplusVersion           uint32
	DebugEventCallback       uintptr
	SuppressBackgroundThread int32
	SuppressExternalCodecs   int32
}

var (
	moduser32   = syscall.NewLazyDLL("user32.dll")
	modgdiplus  = syscall.NewLazyDLL("gdiplus.dll")
	modole32    = syscall.NewLazyDLL("ole32.dll")
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")

	procDestroyIcon = moduser32.NewProc("DestroyIcon")

	procGdiplusStartup            = modgdiplus.NewProc("GdiplusStartup")
	procGdipCreateBitmapFromHICON = modgdiplus.NewProc("GdipCreateBitmapFromHICON")
	procGdipSaveImageToStream     = modgdiplus.NewProc("GdipSaveImageToStream")
	procGdipDisposeImage          = modgdiplus.NewProc("GdipDisposeImage")

	procCreateStreamOnHGlobal = modole32.NewProc("CreateStreamOnHGlobal")
	procGetHGlobalFromStream  = modole32.NewProc("GetHGlobalFromStream")

	procGlobalLock   = modkernel32.NewProc("GlobalLock")
	procGlobalUnlock = modkernel32.NewProc("GlobalUnlock")

	gdiplusOnce  sync.Once
	gdiplusToken uintptr
	gdiplusErr   error
)

// PNG encoder CLSID: {557CF406-1A04-11D3-9A73-0000F81EF32E}.
var pngEncoderCLSID = GUID{
	Data1: 0x557cf406,
	Data2: 0x1a04,
	Data3: 0x11d3,
	Data4: [8]byte{0x9a, 0x73, 0x00, 0x00, 0xf8, 0x1e, 0xf3, 0x2e},
}

// HIconToPNGBytes converts a Windows HICON into PNG bytes held by Go memory.
// The caller still owns hIcon and should release it with DestroyHIcon when appropriate.
func HIconToPNGBytes(hIcon syscall.Handle) ([]byte, error) {
	if hIcon == 0 {
		return nil, fmt.Errorf("icon: empty HICON")
	}

	if err := ensureGDIPlus(); err != nil {
		return nil, err
	}

	var gpBitmap uintptr
	status, _, _ := procGdipCreateBitmapFromHICON.Call(
		uintptr(hIcon),
		uintptr(unsafe.Pointer(&gpBitmap)),
	)
	if status != gdipStatusOK || gpBitmap == 0 {
		return nil, fmt.Errorf("icon: GDI+ failed to create bitmap from HICON, status: %d", status)
	}
	defer procGdipDisposeImage.Call(gpBitmap)

	return saveGpBitmapToPNG(gpBitmap)
}

// HIconToImage converts a Windows HICON into a decoded Go image.Image.
// The caller remains responsible for releasing hIcon.
func HIconToImage(hIcon syscall.Handle) (image.Image, error) {
	pngBytes, err := HIconToPNGBytes(hIcon)
	if err != nil {
		return nil, err
	}
	return imageFromPNGBytes(pngBytes)
}

// DestroyHIcon releases an HICON returned by Shell/User APIs.
// It is safe to call with a zero handle.
func DestroyHIcon(hIcon syscall.Handle) error {
	if hIcon == 0 {
		return nil
	}

	ret, _, callErr := procDestroyIcon.Call(uintptr(hIcon))
	if ret == 0 {
		if !errors.Is(callErr, syscall.Errno(0)) {
			return fmt.Errorf("icon: DestroyIcon failed: %w", callErr)
		}
		return fmt.Errorf("icon: DestroyIcon failed")
	}
	return nil
}

func ensureGDIPlus() error {
	gdiplusOnce.Do(func() {
		input := GdiplusStartupInput{GdiplusVersion: 1}
		status, _, callErr := procGdiplusStartup.Call(
			uintptr(unsafe.Pointer(&gdiplusToken)),
			uintptr(unsafe.Pointer(&input)),
			0,
		)
		if status != gdipStatusOK || gdiplusToken == 0 {
			if !errors.Is(callErr, syscall.Errno(0)) {
				gdiplusErr = fmt.Errorf("icon: GDI+ startup failed, status: %d: %w", status, callErr)
				return
			}
			gdiplusErr = fmt.Errorf("icon: GDI+ startup failed, status: %d", status)
			return
		}

	})

	return gdiplusErr
}

func imageFromPNGBytes(pngBytes []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("icon: failed to decode PNG image: %w", err)
	}
	return img, nil
}

func saveGpBitmapToPNG(gpBitmap uintptr) ([]byte, error) {
	var stream uintptr
	hresult, _, _ := procCreateStreamOnHGlobal.Call(0, 1, uintptr(unsafe.Pointer(&stream)))
	if hresult != sOK || stream == 0 {
		return nil, fmt.Errorf("icon: failed to create IStream, hresult: 0x%x", hresult)
	}
	defer releaseCOMObject(stream)

	status, _, _ := procGdipSaveImageToStream.Call(
		gpBitmap,
		stream,
		uintptr(unsafe.Pointer(&pngEncoderCLSID)),
		0,
	)
	if status != gdipStatusOK {
		return nil, fmt.Errorf("icon: failed to save bitmap to PNG stream, status: %d", status)
	}

	return copyHGlobalStreamBytes(stream)
}

func copyHGlobalStreamBytes(stream uintptr) ([]byte, error) {
	var hGlobal uintptr
	hresult, _, _ := procGetHGlobalFromStream.Call(stream, uintptr(unsafe.Pointer(&hGlobal)))
	if hresult != sOK || hGlobal == 0 {
		return nil, fmt.Errorf("icon: failed to get HGLOBAL from IStream, hresult: 0x%x", hresult)
	}

	ptr, _, _ := procGlobalLock.Call(hGlobal)
	if ptr == 0 {
		return nil, fmt.Errorf("icon: failed to lock PNG stream memory")
	}
	defer procGlobalUnlock.Call(hGlobal)

	size, err := streamSize(stream)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, fmt.Errorf("icon: empty PNG stream")
	}
	if size > uint64(maxGoSliceLen) {
		return nil, fmt.Errorf("icon: PNG stream too large: %d bytes", size)
	}

	buf := make([]byte, int(size))
	copy(buf, unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(size)))
	return buf, nil
}

func streamSize(stream uintptr) (uint64, error) {
	var zero int64
	var position uint64
	vtable := *(*uintptr)(unsafe.Pointer(stream))
	seekProc := *(*uintptr)(unsafe.Pointer(vtable + 5*unsafe.Sizeof(uintptr(0))))

	hresult, _, _ := syscall.SyscallN(
		seekProc,
		stream,
		uintptr(unsafe.Pointer(&zero)),
		2, // STREAM_SEEK_END
		uintptr(unsafe.Pointer(&position)),
	)
	if hresult != sOK {
		return 0, fmt.Errorf("icon: failed to seek PNG stream, hresult: 0x%x", hresult)
	}
	return position, nil
}

func releaseCOMObject(obj uintptr) {
	if obj == 0 {
		return
	}

	vtable := *(*uintptr)(unsafe.Pointer(obj))
	releaseProc := *(*uintptr)(unsafe.Pointer(vtable + 2*unsafe.Sizeof(uintptr(0))))
	syscall.SyscallN(releaseProc, obj)
}

const maxGoSliceLen = int(^uint(0) >> 1)
