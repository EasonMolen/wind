//go:build windows

package icon

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	png "image/png"
	"syscall"
	"unsafe"
)

const (
	BI_RGB          = 0
	DIB_RGB_COLORS  = 0
	DI_NORMAL       = 0x0003
	defaultIconSize = 256
)

type BITMAP struct {
	Type       int32
	Width      int32
	Height     int32
	WidthBytes int32
	Planes     uint16
	BitsPixel  uint16
	Bits       uintptr
}

type BITMAPINFOHEADER struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type RGBQUAD struct {
	Blue     byte
	Green    byte
	Red      byte
	Reserved byte
}

type BITMAPINFO struct {
	Header BITMAPINFOHEADER
	Colors [1]RGBQUAD
}

type ICONINFO struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

// GUID matches the Windows GUID/CLSID binary layout.
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	moduser32 = syscall.NewLazyDLL("user32.dll")
	modgdi32  = syscall.NewLazyDLL("gdi32.dll")

	procDestroyIcon  = moduser32.NewProc("DestroyIcon")
	procDrawIconEx   = moduser32.NewProc("DrawIconEx")
	procGetIconInfo  = moduser32.NewProc("GetIconInfo")
	procCreateDC     = modgdi32.NewProc("CreateCompatibleDC")
	procCreateDIB    = modgdi32.NewProc("CreateDIBSection")
	procDeleteDC     = modgdi32.NewProc("DeleteDC")
	procDeleteObject = modgdi32.NewProc("DeleteObject")
	procSelectObject = modgdi32.NewProc("SelectObject")
	procGetObjectW   = modgdi32.NewProc("GetObjectW")
	procGdiFlush     = modgdi32.NewProc("GdiFlush")
)

// HIconToPNGBytes 只保留透明导出路径，避免 GDI+ 兜底带来的黑底问题。
func HIconToPNGBytes(hIcon syscall.Handle) ([]byte, error) {
	if hIcon == 0 {
		return nil, fmt.Errorf("icon: empty HICON")
	}
	return hiconToPNGBytesTransparent(hIcon)
}

func HIconToImage(hIcon syscall.Handle) (image.Image, error) {
	pngBytes, err := HIconToPNGBytes(hIcon)
	if err != nil {
		return nil, err
	}
	return imageFromPNGBytes(pngBytes)
}

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

func hiconToPNGBytesTransparent(hIcon syscall.Handle) ([]byte, error) {
	width, height, err := iconCanvasSize(hIcon)
	if err != nil {
		return nil, err
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("icon: invalid icon size %dx%d", width, height)
	}

	hdc, _, _ := procCreateDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("icon: failed to create compatible DC")
	}
	defer procDeleteDC.Call(hdc)

	var bits unsafe.Pointer
	bmi := bitmapInfoForSize(width, height)
	hBitmap, _, _ := procCreateDIB.Call(
		hdc,
		uintptr(unsafe.Pointer(&bmi)),
		DIB_RGB_COLORS,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if hBitmap == 0 || bits == nil {
		return nil, fmt.Errorf("icon: failed to create DIB section")
	}
	defer procDeleteObject.Call(hBitmap)

	oldObj, _, _ := procSelectObject.Call(hdc, hBitmap)
	defer procSelectObject.Call(hdc, oldObj)

	buf := unsafe.Slice((*byte)(bits), width*height*4)
	for i := range buf {
		buf[i] = 0
	}

	drawRet, _, _ := procDrawIconEx.Call(
		hdc,
		0,
		0,
		uintptr(hIcon),
		uintptr(width),
		uintptr(height),
		0,
		0,
		DI_NORMAL,
	)
	if drawRet == 0 {
		return nil, fmt.Errorf("icon: DrawIconEx failed")
	}
	procGdiFlush.Call()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcRow := buf[y*width*4 : (y+1)*width*4]
		dstRow := img.Pix[y*img.Stride : y*img.Stride+width*4]
		for x := 0; x < width; x++ {
			si := x * 4
			di := x * 4
			// DIB 内存是 BGRA，这里转成 Go 的 RGBA。
			dstRow[di+0] = srcRow[si+2]
			dstRow[di+1] = srcRow[si+1]
			dstRow[di+2] = srcRow[si+0]
			dstRow[di+3] = srcRow[si+3]
		}
	}

	var out bytes.Buffer
	if err = png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("icon: failed to encode transparent PNG: %w", err)
	}
	return out.Bytes(), nil
}

func iconCanvasSize(hIcon syscall.Handle) (int, int, error) {
	var info ICONINFO
	r1, _, _ := procGetIconInfo.Call(uintptr(hIcon), uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		return 0, 0, fmt.Errorf("icon: GetIconInfo failed")
	}
	if info.HbmMask != 0 {
		defer procDeleteObject.Call(info.HbmMask)
	}
	if info.HbmColor != 0 {
		defer procDeleteObject.Call(info.HbmColor)
	}

	var bmp BITMAP
	if info.HbmColor != 0 {
		r2, _, _ := procGetObjectW.Call(
			info.HbmColor,
			uintptr(unsafe.Sizeof(bmp)),
			uintptr(unsafe.Pointer(&bmp)),
		)
		if r2 != 0 && bmp.Width > 0 && bmp.Height > 0 {
			return int(bmp.Width), int(bmp.Height), nil
		}
	}
	if info.HbmMask != 0 {
		r2, _, _ := procGetObjectW.Call(
			info.HbmMask,
			uintptr(unsafe.Sizeof(bmp)),
			uintptr(unsafe.Pointer(&bmp)),
		)
		if r2 != 0 && bmp.Width > 0 && bmp.Height > 0 {
			return int(bmp.Width), int(bmp.Height / 2), nil
		}
	}
	return defaultIconSize, defaultIconSize, nil
}

func bitmapInfoForSize(width, height int) BITMAPINFO {
	return BITMAPINFO{
		Header: BITMAPINFOHEADER{
			Size:        uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
			Width:       int32(width),
			Height:      int32(-height),
			Planes:      1,
			BitCount:    32,
			Compression: BI_RGB,
		},
	}
}

func imageFromPNGBytes(pngBytes []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("icon: failed to decode PNG image: %w", err)
	}
	return img, nil
}

func releaseCOMObject(obj uintptr) {
	if obj == 0 {
		return
	}

	vtable := *(*uintptr)(unsafe.Pointer(obj))
	releaseProc := *(*uintptr)(unsafe.Pointer(vtable + 2*unsafe.Sizeof(uintptr(0))))
	syscall.SyscallN(releaseProc, obj)
}
