package displayaction

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// keyCommands translates one atomic press, never a persistent key-down state.
// Modifier names are explicit: Control, Alt, Shift, Meta (macOS Command).
func keyCommands(key, platform string) (string, error) {
	parts := strings.Split(key, "+")
	modifiers := []string{}
	prefix := ""
	seen := map[string]bool{}
	for _, part := range parts[:len(parts)-1] {
		name := strings.ToLower(part)
		if seen[name] {
			return "", errors.New("duplicate key modifier")
		}
		seen[name] = true
		switch name {
		case "control":
			modifiers = append(modifiers, "control down")
			prefix += "^"
		case "alt":
			modifiers = append(modifiers, "option down")
			prefix += "%"
		case "shift":
			modifiers = append(modifiers, "shift down")
			prefix += "+"
		case "meta":
			if platform != "darwin" {
				return "", errors.New("Meta shortcuts are not supported by this Windows input backend")
			}
			modifiers = append(modifiers, "command down")
		default:
			return "", errors.New("unsupported key modifier")
		}
	}
	base := parts[len(parts)-1]
	codes := map[string]int{"Enter": 36, "Tab": 48, "Space": 49, "Backspace": 51, "Escape": 53, "Delete": 117, "Home": 115, "End": 119, "PageUp": 116, "PageDown": 121, "ArrowLeft": 123, "ArrowRight": 124, "ArrowDown": 125, "ArrowUp": 126, "F1": 122, "F2": 120, "F3": 99, "F4": 118, "F5": 96, "F6": 97, "F7": 98, "F8": 100, "F9": 101, "F10": 109, "F11": 103, "F12": 111}
	windows := map[string]string{"Enter": "ENTER", "Tab": "TAB", "Space": " ", "Backspace": "BACKSPACE", "Escape": "ESC", "Delete": "DELETE", "Home": "HOME", "End": "END", "PageUp": "PGUP", "PageDown": "PGDN", "ArrowLeft": "LEFT", "ArrowRight": "RIGHT", "ArrowDown": "DOWN", "ArrowUp": "UP"}
	var command string
	if code, ok := codes[base]; ok {
		if platform == "darwin" {
			command = fmt.Sprintf("key code %d", code)
		} else {
			name := windows[base]
			if name == "" {
				name = base
			}
			if base == "Space" {
				command = prefix + " "
			} else {
				command = prefix + "{" + name + "}"
			}
		}
	} else {
		if base == "Plus" {
			base = "+"
		}
		if utf8.RuneCountInString(base) != 1 || strings.ContainsAny(base, "\x00\r\n") {
			return "", errors.New("unsupported named key")
		}
		if platform == "darwin" {
			command = "keystroke " + appleScriptString(base)
		} else {
			command = prefix + windowsLiteral(base)
		}
	}
	if platform == "darwin" && len(modifiers) > 0 {
		command += " using {" + strings.Join(modifiers, ", ") + "}"
	}
	return command, nil
}
func windowsLiteral(text string) string {
	var out strings.Builder
	for _, r := range text {
		switch r {
		case '+', '^', '%', '~', '(', ')', '[', ']', '{', '}':
			out.WriteString("{" + string(r) + "}")
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
