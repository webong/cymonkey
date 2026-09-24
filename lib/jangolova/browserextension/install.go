package browserextension

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var chromeID = regexp.MustCompile(`^[a-p]{32}$`)

// InstallCapability describes the routes available for a caller-owned package
// in the selected browser. Persistent installation may require browser action.
type InstallCapability struct {
	Browser                string   `json:"browser"`
	PersistentLocalInstall bool     `json:"persistentLocalInstall"`
	InstallDriver          string   `json:"installDriver,omitempty"`
	RequiresBrowserAction  bool     `json:"requiresBrowserAction"`
	SessionLoad            bool     `json:"sessionLoad"`
	SessionDriver          string   `json:"sessionDriver,omitempty"`
	Requires               []string `json:"requires,omitempty"`
	Reason                 string   `json:"reason,omitempty"`
}

func Capability(browserName string) InstallCapability {
	switch browserName {
	case "chrome", "chromium", "edge":
		if runtime.GOOS == "windows" {
			return InstallCapability{Browser: browserName, Reason: "the pipe-based extension session adapter is not implemented on Windows"}
		}
		reason := "Persistent installation requires the browser's Load unpacked action; DevTools-only loading lasts for the browser session"
		if browserName == "chrome" {
			reason += "; Chrome 136+ requires a custom user data directory for debugging-pipe verification"
		}
		return InstallCapability{Browser: browserName, PersistentLocalInstall: true, InstallDriver: "native-load-unpacked", RequiresBrowserAction: true, SessionLoad: true, SessionDriver: "cdp-pipe", Requires: []string{"executablePath", "userDataDir", "revision", "source", "destination"}, Reason: reason}
	default:
		return InstallCapability{Browser: browserName, Reason: "no direct local extension adapter is available for this browser"}
	}
}

// InstallResult describes a browser-native installation handoff. A staged
// extension is not reported as installed until the browser confirms it.
type InstallResult struct {
	Status     string       `json:"status"`
	Browser    string       `json:"browser"`
	ID         string       `json:"id,omitempty"`
	Source     string       `json:"source,omitempty"`
	Profile    string       `json:"profile,omitempty"`
	NextAction string       `json:"nextAction,omitempty"`
	Extension  *Description `json:"extension,omitempty"`
}

// StageForChromium stages a ZIP or directory for Chrome or Edge's documented
// Load unpacked flow. It does not bypass developer mode or browser consent.
func StageForChromium(browserName, source, destination string) (InstallResult, error) {
	return StageForChromiumWithRevision(browserName, source, destination, "")
}

// StageForChromiumWithRevision binds the staging request to a reviewed source
// revision returned by Inspect.
func StageForChromiumWithRevision(browserName, source, destination, expectedRevision string) (InstallResult, error) {
	page := ""
	switch browserName {
	case "chrome":
		page = "chrome://extensions"
	case "chromium":
		page = "chrome://extensions"
	case "edge":
		page = "edge://extensions"
	default:
		return InstallResult{}, errors.New("unpacked installation supports chrome, chromium, or edge")
	}
	description, err := StageWithRevision(source, destination, expectedRevision)
	if err != nil {
		return InstallResult{}, err
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{
		Status: "awaiting-browser-action", Browser: browserName, ID: description.ID,
		Source: absolute, Extension: &description,
		NextAction: "Open " + page + ", enable Developer Mode, select Load unpacked, and choose the staged directory. Confirm the extension and its permissions in the browser.",
	}, nil
}

// StageForChrome retains the simple Chrome entry point for host callers.
func StageForChrome(source, destination string) (InstallResult, error) {
	return StageForChromium("chrome", source, destination)
}

// RequestChromeWebStoreInstall writes Chrome's documented external-extension
// preference for a published Web Store ID on macOS. Chrome still presents its
// native enable/permission confirmation after it reads the preference.
// externalDirectory must be the intended profile's External Extensions folder.
func RequestChromeWebStoreInstall(id, externalDirectory string) (InstallResult, error) {
	if runtime.GOOS != "darwin" {
		return InstallResult{}, errors.New("Chrome external preference installation is supported on macOS only")
	}
	if !chromeID.MatchString(id) {
		return InstallResult{}, errors.New("valid Chrome Web Store extension ID is required")
	}
	if externalDirectory == "" {
		return InstallResult{}, errors.New("external extensions directory is required")
	}
	absolute, err := filepath.Abs(externalDirectory)
	if err != nil {
		return InstallResult{}, err
	}
	if !filepath.IsAbs(externalDirectory) || !strings.HasSuffix(filepath.ToSlash(filepath.Clean(absolute)), "/Library/Application Support/Google/Chrome/External Extensions") {
		return InstallResult{}, errors.New("external extensions directory must be a Chrome macOS External Extensions path")
	}
	// Refuse symlinked directories so the destination cannot be redirected.
	for current := absolute; current != filepath.Dir(current); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return InstallResult{}, err
		}
		// macOS exposes /var and /tmp as root-owned aliases under /private.
		// Reject symlinks deeper in a caller-selected profile path.
		if info.Mode()&os.ModeSymlink != 0 && filepath.Dir(current) != string(filepath.Separator) {
			return InstallResult{}, fmt.Errorf("external extensions path contains symlink %q", current)
		}
	}
	if err := os.MkdirAll(absolute, 0755); err != nil {
		return InstallResult{}, err
	}
	filePath := filepath.Join(absolute, id+".json")
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return InstallResult{}, err
	}
	content, _ := json.Marshal(map[string]string{"external_update_url": "https://clients2.google.com/service/update2/crx"})
	_, writeErr := file.Write(append(content, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		os.Remove(filePath)
		return InstallResult{}, writeErr
	}
	if closeErr != nil {
		os.Remove(filePath)
		return InstallResult{}, closeErr
	}
	return InstallResult{
		Status: "awaiting-browser-confirmation", Browser: "chrome", ID: id, Source: filePath,
		NextAction: "Restart Chrome, then review and enable the externally installed extension in Chrome's native extension UI.",
	}, nil
}
