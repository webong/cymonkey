package browserextension

import (
	"errors"
	"path/filepath"
	"runtime"
)

// InstallCapability describes the routes available for a caller-owned package
// in the selected browser. Persistent installation may require browser action.
type InstallCapability struct {
	Browser                string   `json:"browser"`
	ExternalStoreRequest   bool     `json:"externalStoreRequest"`
	ExternalStoreDriver    string   `json:"externalStoreDriver,omitempty"`
	SupportedStores        []string `json:"supportedStores,omitempty"`
	ManagedPolicy          bool     `json:"managedPolicy"`
	ManagedPolicyDriver    string   `json:"managedPolicyDriver,omitempty"`
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
		storeCapability := InstallCapability{Browser: browserName}
		if browserName == "chrome" || browserName == "edge" {
			storeCapability.ExternalStoreRequest = true
			storeCapability.SupportedStores = []string{browserName}
			if browserName == "edge" {
				storeCapability.SupportedStores = []string{"edge", "chrome"}
			}
			if runtime.GOOS == "windows" {
				storeCapability.ExternalStoreDriver = "powershell-registry"
				storeCapability.ManagedPolicy = true
				storeCapability.ManagedPolicyDriver = "powershell-registry"
			} else if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
				storeCapability.ExternalStoreDriver = "external-preferences-json"
				if runtime.GOOS == "linux" && browserName == "chrome" {
					storeCapability.ManagedPolicy = true
					storeCapability.ManagedPolicyDriver = "managed-json"
				}
			} else {
				storeCapability.ExternalStoreRequest = false
			}
		}
		if runtime.GOOS == "windows" {
			storeCapability.Reason = "the pipe-based extension session adapter is not implemented on Windows"
			return storeCapability
		}
		reason := "Persistent installation requires the browser's Load unpacked action; DevTools-only loading lasts for the browser session"
		if browserName == "chrome" {
			reason += "; Chrome 136+ requires a custom user data directory for debugging-pipe verification"
		}
		storeCapability.PersistentLocalInstall = true
		storeCapability.InstallDriver = "native-load-unpacked"
		storeCapability.RequiresBrowserAction = true
		storeCapability.SessionLoad = true
		storeCapability.SessionDriver = "cdp-pipe"
		storeCapability.Requires = []string{"executablePath", "userDataDir", "revision", "source", "destination"}
		storeCapability.Reason = reason
		return storeCapability
	case "firefox":
		return InstallCapability{Browser: browserName, PersistentLocalInstall: true, InstallDriver: "webdriver-bidi-signed-xpi", RequiresBrowserAction: false,
			Requires: []string{"executablePath", "profilePath", "source", "revision"},
			Reason:   "Firefox accepts a permanently installed XPI only when its signature is valid; Jangolova verifies the active extension after restart"}
	case "safari":
		if runtime.GOOS != "darwin" {
			return InstallCapability{Browser: browserName, Reason: "Safari WebExtension installation requires macOS and a containing app"}
		}
		return InstallCapability{Browser: browserName, PersistentLocalInstall: true, InstallDriver: "signed-containing-app", RequiresBrowserAction: true,
			Requires: []string{"source", "revision"},
			Reason:   "The caller supplies a signed macOS app containing a Safari WebExtension; Safari requires the user to enable it in Settings"}
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

// RequestChromeWebStoreInstall keeps the original Chrome entry point for callers.
// New callers can select Chrome or Edge with RequestStoreInstall.
func RequestChromeWebStoreInstall(id, externalDirectory string) (InstallResult, error) {
	return RequestStoreInstall("chrome", "chrome", id, externalDirectory)
}
