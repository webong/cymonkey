package browserextension

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf16"
)

var chromeID = regexp.MustCompile(`^[a-p]{32}$`)

const (
	chromeUpdateURL = "https://clients2.google.com/service/update2/crx"
	edgeUpdateURL   = "https://edge.microsoft.com/extensionwebstorebase/v1/crx"
)

// RequestStoreInstall registers a published extension for Chrome or Edge to
// discover at startup. Registration is not proof of installation. On Windows
// the system registry is used; on macOS and Linux a browser preferences file.
// store is "chrome" or "edge" and allows Edge to use either store.
func RequestStoreInstall(browser, store, id, externalDirectory string) (InstallResult, error) {
	return changeStoreInstall(runtime.GOOS, browser, store, id, externalDirectory, false)
}

// RemoveStoreInstall removes only an external request whose update URL matches
// the selected store. It does not remove an independently installed extension.
func RemoveStoreInstall(browser, store, id, externalDirectory string) (InstallResult, error) {
	return changeStoreInstall(runtime.GOOS, browser, store, id, externalDirectory, true)
}

func changeStoreInstall(goos, browser, store, id, directory string, remove bool) (InstallResult, error) {
	url, err := storeURL(browser, store, id)
	if err != nil {
		return InstallResult{}, err
	}
	var source string
	switch goos {
	case "windows":
		if directory != "" {
			return InstallResult{}, errors.New("--external-dir is for macOS and Linux; Windows uses the machine registry")
		}
		source, err = changeWindowsExternal(browser, id, url, remove)
	case "darwin", "linux":
		source, err = changeUnixExternal(goos, browser, id, url, directory, remove)
	default:
		return InstallResult{}, fmt.Errorf("external extension requests are unsupported on %s", goos)
	}
	if err != nil {
		return InstallResult{}, err
	}
	if remove {
		return InstallResult{Status: "request-removed", Browser: browser, ID: id, Source: source,
			NextAction: "Restart the browser and check its extensions page. Removing a request does not remove an extension installed independently."}, nil
	}
	status := "awaiting-browser-confirmation"
	next := "Restart the browser, then review and enable the extension in its extensions page."
	if goos == "linux" {
		status = "requested"
		next = "Restart the browser and verify the extension in its extensions page."
	}
	return InstallResult{Status: status, Browser: browser, ID: id, Source: source, NextAction: next}, nil
}

func storeURL(browser, store, id string) (string, error) {
	if !chromeID.MatchString(id) {
		return "", errors.New("a 32-character extension ID using a-p is required")
	}
	if browser != "chrome" && browser != "edge" {
		return "", errors.New("external store installation supports chrome or edge")
	}
	if store == "" {
		store = browser
	}
	if browser == "chrome" && store != "chrome" {
		return "", errors.New("Chrome external requests must use the Chrome Web Store")
	}
	switch store {
	case "chrome":
		return chromeUpdateURL, nil
	case "edge":
		return edgeUpdateURL, nil
	default:
		return "", errors.New("store must be chrome or edge")
	}
}

func defaultExternalDirectory(goos, browser string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch goos {
	case "darwin":
		if browser == "chrome" {
			return filepath.Join(home, "Library/Application Support/Google/Chrome/External Extensions"), nil
		}
		return filepath.Join(home, "Library/Application Support/Microsoft Edge/External Extensions"), nil
	case "linux":
		if browser == "chrome" {
			return "/opt/google/chrome/extensions", nil
		}
		return filepath.Join(home, ".config/microsoft-edge/External Extensions"), nil
	default:
		return "", errors.New("no external extensions directory for this platform")
	}
}

func allowedExternalDirectory(goos, browser, directory string) bool {
	clean := filepath.ToSlash(filepath.Clean(directory))
	defaultPath, err := defaultExternalDirectory(goos, browser)
	if err == nil && clean == filepath.ToSlash(filepath.Clean(defaultPath)) {
		return true
	}
	switch goos + ":" + browser {
	case "darwin:chrome":
		return clean == "/Library/Application Support/Google/Chrome/External Extensions"
	case "darwin:edge":
		return clean == "/Library/Application Support/Microsoft/Edge/External Extensions"
	case "linux:chrome":
		return clean == "/opt/google/chrome/extensions" || clean == "/usr/share/google-chrome/extensions"
	case "linux:edge":
		return clean == "/usr/share/microsoft-edge/extensions"
	}
	return false
}

func checkExternalPath(directory string) error {
	for current := directory; current != filepath.Dir(current); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		// macOS exposes /var and /tmp as aliases at the filesystem root.
		if info.Mode()&os.ModeSymlink != 0 && filepath.Dir(current) != string(filepath.Separator) {
			return fmt.Errorf("external extensions path contains symlink %q", current)
		}
	}
	return nil
}

func changeUnixExternal(goos, browser, id, url, directory string, remove bool) (string, error) {
	if directory == "" {
		var err error
		directory, err = defaultExternalDirectory(goos, browser)
		if err != nil {
			return "", err
		}
	}
	if !filepath.IsAbs(directory) || !allowedExternalDirectory(goos, browser, directory) {
		return "", fmt.Errorf("external extensions directory is not a documented %s %s path", browser, goos)
	}
	directory = filepath.Clean(directory)
	if err := checkExternalPath(directory); err != nil {
		return "", err
	}
	filePath := filepath.Join(directory, id+".json")
	if remove {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", err
		}
		var preference map[string]string
		if err := json.Unmarshal(content, &preference); err != nil {
			return "", err
		}
		if preference["external_update_url"] != url || len(preference) != 1 {
			return "", errors.New("external request differs from the generated store request; refusing to remove it")
		}
		return filePath, os.Remove(filePath)
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", err
	}
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	content, _ := json.Marshal(map[string]string{"external_update_url": url})
	_, writeErr := file.Write(append(content, '\n'))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(filePath)
		return "", errors.Join(writeErr, closeErr)
	}
	return filePath, nil
}

func changeWindowsExternal(browser, id, url string, remove bool) (string, error) {
	path, script := windowsExternalScript(browser, id, url, remove)
	encoded := encodePowerShell(script)
	output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Windows external extension registry update failed (run with administrator rights): %w: %s", err, strings.TrimSpace(string(output)))
	}
	return path, nil
}

func windowsExternalScript(browser, id, url string, remove bool) (string, string) {
	vendor := `Google\Chrome`
	if browser == "edge" {
		vendor = `Microsoft\Edge`
	}
	// Registry provider paths make the intended hive explicit. The documented
	// 64-bit location is Wow6432Node; 32-bit Windows uses the plain path.
	base := `Registry::HKEY_LOCAL_MACHINE\Software\`
	if strings.Contains(runtime.GOARCH, "64") {
		base += `Wow6432Node\`
	}
	path := base + vendor + `\Extensions\` + id
	script := "$ErrorActionPreference = 'Stop'\n$path = '" + path + "'\n$url = '" + url + "'\n"
	if remove {
		script += "if (-not (Test-Path -LiteralPath $path)) { throw 'External request does not exist' }\n" +
			"$item = Get-ItemProperty -LiteralPath $path\n" +
			"$actual = $item.update_url\n" +
			"if ($actual -ne $url) { throw 'External request has a different update URL' }\n" +
			"$extra = @($item.PSObject.Properties | Where-Object { $_.Name -notlike 'PS*' -and $_.Name -ne 'update_url' })\n" +
			"if ($extra.Count -gt 0) { throw 'External request has additional metadata; refusing to remove it' }\n" +
			"Remove-Item -LiteralPath $path -Force\n"
	} else {
		script += "if (Test-Path -LiteralPath $path) { throw 'External request already exists' }\n" +
			"New-Item -Path $path -Force | Out-Null\n" +
			"New-ItemProperty -LiteralPath $path -Name update_url -Value $url -PropertyType String -Force | Out-Null\n"
	}
	return path, script
}

func encodePowerShell(script string) string {
	runes := utf16.Encode([]rune(script))
	bytes := make([]byte, len(runes)*2)
	for i, value := range runes {
		bytes[2*i], bytes[2*i+1] = byte(value), byte(value>>8)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}
