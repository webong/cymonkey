//go:build darwin && cgo

package macoskeyboard

/*
#cgo LDFLAGS: -framework ApplicationServices -lproc
#include <ApplicationServices/ApplicationServices.h>
#include <libproc.h>
#include <sys/proc_info.h>
#include <stdint.h>

static int board_keyboard_trusted(void) {
    return AXIsProcessTrustedWithOptions(NULL) ? 1 : 0;
}

static int board_keyboard_identity(pid_t pid, uint64_t *seconds, uint64_t *microseconds) {
    struct proc_bsdinfo info;
    int size = proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info));
    if (size != sizeof(info)) return 0;
    *seconds = info.pbi_start_tvsec;
    *microseconds = info.pbi_start_tvusec;
    return 1;
}

static CGEventFlags board_keyboard_flags(uint64_t modifiers) {
    CGEventFlags flags = 0;
    if (modifiers & 1) flags |= kCGEventFlagMaskCommand;
    if (modifiers & 2) flags |= kCGEventFlagMaskControl;
    if (modifiers & 4) flags |= kCGEventFlagMaskAlternate;
    if (modifiers & 8) flags |= kCGEventFlagMaskShift;
    return flags;
}

static int board_keyboard_press(pid_t pid, CGKeyCode code, uint64_t modifiers) {
    CGEventRef down = CGEventCreateKeyboardEvent(NULL, code, true);
    CGEventRef up = CGEventCreateKeyboardEvent(NULL, code, false);
    if (down == NULL || up == NULL) {
        if (down != NULL) CFRelease(down);
        if (up != NULL) CFRelease(up);
        return 0;
    }
    CGEventFlags flags = board_keyboard_flags(modifiers);
    CGEventSetFlags(down, flags);
    CGEventSetFlags(up, flags);
    CGEventPostToPid(pid, down);
    CGEventPostToPid(pid, up);
    CFRelease(down);
    CFRelease(up);
    return 1;
}

static int board_keyboard_type_rune(pid_t pid, const UniChar *chars, UniCharCount count) {
    CGEventRef down = CGEventCreateKeyboardEvent(NULL, 0, true);
    CGEventRef up = CGEventCreateKeyboardEvent(NULL, 0, false);
    if (down == NULL || up == NULL) {
        if (down != NULL) CFRelease(down);
        if (up != NULL) CFRelease(up);
        return 0;
    }
    CGEventKeyboardSetUnicodeString(down, count, chars);
    CGEventKeyboardSetUnicodeString(up, count, chars);
    CGEventPostToPid(pid, down);
    CGEventPostToPid(pid, up);
    CFRelease(down);
    CFRelease(up);
    return 1;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

type nativeMacKeyboard struct{}

func newNativeKeyboard() (nativeKeyboard, error) { return nativeMacKeyboard{}, nil }

func (nativeMacKeyboard) trusted() bool { return C.board_keyboard_trusted() != 0 }

func (nativeMacKeyboard) identity(pid int) (processIdentity, error) {
	var seconds, microseconds C.uint64_t
	if C.board_keyboard_identity(C.pid_t(pid), &seconds, &microseconds) == 0 {
		return processIdentity{}, ErrTarget
	}
	return processIdentity{seconds: uint64(seconds), microseconds: uint64(microseconds)}, nil
}

func (nativeMacKeyboard) press(pid int, code uint16, modifiers uint64) error {
	if C.board_keyboard_press(C.pid_t(pid), C.CGKeyCode(code), C.uint64_t(modifiers)) == 0 {
		return errors.New("macOS could not create keyboard events")
	}
	return nil
}

func (nativeMacKeyboard) typeRune(pid int, chars []uint16) error {
	if len(chars) == 0 {
		return errors.New("keyboard rune is empty")
	}
	if C.board_keyboard_type_rune(C.pid_t(pid), (*C.UniChar)(unsafe.Pointer(&chars[0])), C.UniCharCount(len(chars))) == 0 {
		return errors.New("macOS could not create Unicode keyboard events")
	}
	return nil
}
