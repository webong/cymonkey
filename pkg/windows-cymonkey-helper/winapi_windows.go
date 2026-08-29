//go:build windows

package main

import (
	"errors"
	"image"
	"image/png"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procEnumWindows                = user32.NewProc("EnumWindows")
	procIsWindowVisible            = user32.NewProc("IsWindowVisible")
	procGetWindowTextLengthW       = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW             = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessID   = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowRect              = user32.NewProc("GetWindowRect")
	procSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	procSetCursorPos               = user32.NewProc("SetCursorPos")
	procMouseEvent                 = user32.NewProc("mouse_event")
	procGetDC                      = user32.NewProc("GetDC")
	procReleaseDC                  = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC         = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC                   = gdi32.NewProc("DeleteDC")
	procCreateCompatibleBitmap     = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject               = gdi32.NewProc("SelectObject")
	procDeleteObject               = gdi32.NewProc("DeleteObject")
	procBitBlt                     = gdi32.NewProc("BitBlt")
	procGetDIBits                  = gdi32.NewProc("GetDIBits")
	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procSendInput                  = user32.NewProc("SendInput")
)

var errUnavailable = errors.New("native desktop operation unavailable")

const (
	processQueryLimitedInformation = 0x1000
	srccopy                        = 0x00CC0020
	dibRGBColors                   = 0
	mouseeventfLeftDown            = 0x0002
	mouseeventfLeftUp              = 0x0004
	mouseeventfRightDown           = 0x0008
	mouseeventfRightUp             = 0x0010
	mouseeventfMiddleDown          = 0x0020
	mouseeventfMiddleUp            = 0x0040
	mouseeventfWheel               = 0x0800
	inputKeyboard                  = 1
	keyeventfKeyUp                 = 0x0002
	keyeventfUnicode               = 0x0004
)

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) width() int  { return int(r.Right - r.Left) }
func (r rect) height() int { return int(r.Bottom - r.Top) }

type bitmapInfoHeader struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}
type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}
type keyboardInput struct {
	WVk, WScan  uint16
	Flags, Time uint32
	ExtraInfo   uintptr
}
type input struct {
	Type uint32
	Ki   keyboardInput
}

type nativeWindow struct {
	hwnd              uintptr
	pid               uint32
	executable, title string
	bounds            rect
}

func enumerateWindows(config helperConfig) ([]nativeWindow, error) {
	var values []nativeWindow
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		title := windowText(hwnd)
		if strings.TrimSpace(title) == "" {
			return 1
		}
		var pid uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 {
			return 1
		}
		executable, err := processExecutable(pid)
		if err != nil || !config.allowsExecutable(executable) {
			return 1
		}
		var bounds rect
		if result, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds))); result == 0 || bounds.width() <= 0 || bounds.height() <= 0 {
			return 1
		}
		values = append(values, nativeWindow{hwnd: hwnd, pid: pid, executable: executable, title: title, bounds: bounds})
		return 1
	})
	if result, _, _ := procEnumWindows.Call(callback, 0); result == 0 {
		return nil, errUnavailable
	}
	return values, nil
}

func processExecutable(pid uint32) (string, error) {
	handle, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 {
		return "", errUnavailable
	}
	defer procCloseHandle.Call(handle)
	buffer := make([]uint16, 32768)
	length := uint32(len(buffer))
	result, _, _ := procQueryFullProcessImageNameW.Call(handle, 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&length)))
	if result == 0 {
		return "", errUnavailable
	}
	return strings.ToLower(filepath.Base(syscall.UTF16ToString(buffer[:length]))), nil
}

func windowText(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if length == 0 {
		return ""
	}
	buffer := make([]uint16, int(length)+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return syscall.UTF16ToString(buffer)
}

func (w nativeWindow) appSurface() windowsSurface {
	return windowsSurface{ID: "windows-app:" + w.id(), Domain: "viewer", Runtime: "windows-app", Kind: "window", Label: w.title, Properties: map[string]any{"processId": w.pid, "executable": w.executable, "bounds": map[string]int{"x": int(w.bounds.Left), "y": int(w.bounds.Top), "width": w.bounds.width(), "height": w.bounds.height()}}}
}
func (w nativeWindow) viewerSurface() windowsSurface {
	return windowsSurface{ID: "windows-viewer:" + w.id(), Domain: "viewer", Runtime: "windows-viewer", Kind: "viewport", Label: w.title, Properties: map[string]any{"processId": w.pid, "executable": w.executable, "bounds": map[string]int{"x": 0, "y": 0, "width": w.bounds.width(), "height": w.bounds.height()}}}
}
func (w nativeWindow) viewerDescription() map[string]any {
	return map[string]any{"surfaceId": w.viewerSurface().ID, "width": w.bounds.width(), "height": w.bounds.height(), "coordinateSpace": "window-local", "focused": false}
}
func (w nativeWindow) id() string {
	return strconv.FormatUint(uint64(w.pid), 10) + "-" + strconv.FormatUint(uint64(w.hwnd), 10)
}

func resolveWindow(config helperConfig, prefix, surfaceID string) (nativeWindow, error) {
	if !strings.HasPrefix(surfaceID, prefix) {
		return nativeWindow{}, errors.New("surface belongs to another runtime")
	}
	id := strings.TrimPrefix(surfaceID, prefix)
	for _, window := range mustWindows(config) {
		if window.id() == id {
			return window, nil
		}
	}
	return nativeWindow{}, errors.New("desktop window reference is unavailable or stale")
}
func mustWindows(config helperConfig) []nativeWindow {
	values, _ := enumerateWindows(config)
	return values
}

func (w nativeWindow) activate() error {
	if result, _, _ := procSetForegroundWindow.Call(w.hwnd); result == 0 {
		return errUnavailable
	}
	return nil
}
func (w nativeWindow) currentBounds() (rect, error) {
	var bounds rect
	if result, _, _ := procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&bounds))); result == 0 || bounds.width() <= 0 || bounds.height() <= 0 {
		return rect{}, errUnavailable
	}
	return bounds, nil
}
func (w nativeWindow) validateLocalPoint(x, y int) error {
	bounds, err := w.currentBounds()
	if err != nil {
		return err
	}
	if x < 0 || y < 0 || x >= bounds.width() || y >= bounds.height() {
		return errors.New("coordinates exceed viewer surface bounds")
	}
	return nil
}
func (w nativeWindow) movePointer(x, y int) error {
	bounds, err := w.currentBounds()
	if err != nil {
		return err
	}
	if result, _, _ := procSetCursorPos.Call(uintptr(int(bounds.Left)+x), uintptr(int(bounds.Top)+y)); result == 0 {
		return errUnavailable
	}
	return nil
}
func (w nativeWindow) drag(sx, sy, ex, ey int) error {
	if err := w.movePointer(sx, sy); err != nil {
		return err
	}
	procMouseEvent.Call(mouseeventfLeftDown, 0, 0, 0, 0)
	if err := w.movePointer(ex, ey); err != nil {
		return err
	}
	procMouseEvent.Call(mouseeventfLeftUp, 0, 0, 0, 0)
	return nil
}

func click(button string) error {
	switch strings.ToLower(strings.TrimSpace(button)) {
	case "", "left":
		procMouseEvent.Call(mouseeventfLeftDown, 0, 0, 0, 0)
		procMouseEvent.Call(mouseeventfLeftUp, 0, 0, 0, 0)
	case "right":
		procMouseEvent.Call(mouseeventfRightDown, 0, 0, 0, 0)
		procMouseEvent.Call(mouseeventfRightUp, 0, 0, 0, 0)
	case "middle":
		procMouseEvent.Call(mouseeventfMiddleDown, 0, 0, 0, 0)
		procMouseEvent.Call(mouseeventfMiddleUp, 0, 0, 0, 0)
	default:
		return errors.New("unsupported pointer button")
	}
	return nil
}
func scroll(delta int) error {
	if delta == 0 {
		return nil
	}
	procMouseEvent.Call(mouseeventfWheel, 0, 0, uintptr(int32(delta)), 0)
	return nil
}

func typeText(text string) error {
	for _, runeValue := range []rune(text) {
		if err := sendKeyboard(input{Type: inputKeyboard, Ki: keyboardInput{WScan: uint16(runeValue), Flags: keyeventfUnicode}}); err != nil {
			return err
		}
		if err := sendKeyboard(input{Type: inputKeyboard, Ki: keyboardInput{WScan: uint16(runeValue), Flags: keyeventfUnicode | keyeventfKeyUp}}); err != nil {
			return err
		}
	}
	return nil
}
func pressKey(key string) error {
	virtualKeys := map[string]uint16{"enter": 0x0d, "tab": 0x09, "escape": 0x1b, "space": 0x20, "left": 0x25, "up": 0x26, "right": 0x27, "down": 0x28, "backspace": 0x08, "delete": 0x2e}
	code, ok := virtualKeys[key]
	if !ok {
		return errors.New("unsupported keyboard key")
	}
	if err := sendKeyboard(input{Type: inputKeyboard, Ki: keyboardInput{WVk: code}}); err != nil {
		return err
	}
	return sendKeyboard(input{Type: inputKeyboard, Ki: keyboardInput{WVk: code, Flags: keyeventfKeyUp}})
}
func sendKeyboard(value input) error {
	result, _, _ := procSendInput.Call(1, uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
	if result != 1 {
		return errUnavailable
	}
	return nil
}

func (w nativeWindow) capture() (frameCapture, error) {
	bounds, err := w.currentBounds()
	if err != nil {
		return frameCapture{}, err
	}
	width, height := bounds.width(), bounds.height()
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return frameCapture{}, errUnavailable
	}
	defer procReleaseDC.Call(0, screenDC)
	memoryDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memoryDC == 0 {
		return frameCapture{}, errUnavailable
	}
	defer procDeleteDC.Call(memoryDC)
	bitmap, _, _ := procCreateCompatibleBitmap.Call(screenDC, uintptr(width), uintptr(height))
	if bitmap == 0 {
		return frameCapture{}, errUnavailable
	}
	defer procDeleteObject.Call(bitmap)
	previous, _, _ := procSelectObject.Call(memoryDC, bitmap)
	if previous == 0 {
		return frameCapture{}, errUnavailable
	}
	defer procSelectObject.Call(memoryDC, previous)
	if result, _, _ := procBitBlt.Call(memoryDC, 0, 0, uintptr(width), uintptr(height), screenDC, uintptr(bounds.Left), uintptr(bounds.Top), srccopy); result == 0 {
		return frameCapture{}, errUnavailable
	}
	pixels := make([]byte, width*height*4)
	info := bitmapInfo{Header: bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32, Compression: 0}}
	if result, _, _ := procGetDIBits.Call(memoryDC, bitmap, 0, uintptr(height), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&info)), dibRGBColors); result == 0 {
		return frameCapture{}, errUnavailable
	}
	for offset := 0; offset < len(pixels); offset += 4 {
		pixels[offset], pixels[offset+2] = pixels[offset+2], pixels[offset]
	}
	return encodePNG(width, height, pixels)
}

func encodeRGBAAsPNG(output io.Writer, width, height int, rgba []byte) error {
	return png.Encode(output, &image.RGBA{Pix: rgba, Stride: width * 4, Rect: image.Rect(0, 0, width, height)})
}
