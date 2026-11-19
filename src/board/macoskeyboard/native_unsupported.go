//go:build !darwin || !cgo

package macoskeyboard

func newNativeKeyboard() (nativeKeyboard, error) { return nil, ErrUnsupported }
