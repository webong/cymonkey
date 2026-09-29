package browserextension

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ActiveEndpoint reads an existing Chromium debugging marker. Its presence
// does not establish profile isolation within the browser process.
func ActiveEndpoint(target BrowserTarget) (string, string, error) {
	switch target.Browser {
	case "chrome", "chromium", "edge":
	default:
		return "", "", errors.New("this browser requires an endpoint from its target provider")
	}
	if target.ProfilePath == "" || !filepath.IsAbs(target.ProfilePath) {
		return "", "", errors.New("a browser user data directory is required to discover its debugging endpoint")
	}
	data, err := os.ReadFile(filepath.Join(target.ProfilePath, "DevToolsActivePort"))
	if err != nil {
		return "", "", errors.New("no active CDP endpoint was found for this browser; supply endpoint=cdp=URL from the target provider")
	}
	if len(data) > 4096 {
		return "", "", errors.New("browser debugging marker is invalid")
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(strings.TrimSpace(lines[1]), "/devtools/browser/") {
		return "", "", errors.New("browser debugging marker is invalid")
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || port < 1 || port > 65535 {
		return "", "", errors.New("browser debugging marker has an invalid port")
	}
	return "cdp", fmt.Sprintf("http://127.0.0.1:%d", port), nil
}
