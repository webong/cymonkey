package browserextension

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// BrowserTarget identifies a browser installation and one local profile.
// Discovery reads known local locations only; callers may always supply
// explicit executable and profile paths instead.
type BrowserTarget struct {
	ID               string `json:"id"`
	Browser          string `json:"browser"`
	Name             string `json:"name"`
	ExecutablePath   string `json:"executablePath"`
	ProfilePath      string `json:"profilePath,omitempty"`
	ProfileDirectory string `json:"profileDirectory,omitempty"`
	InstallMode      string `json:"installMode"`
	Reason           string `json:"reason,omitempty"`
}

func (target BrowserTarget) DevToolsTarget() DevToolsTarget {
	return DevToolsTarget{Browser: target.Browser, ExecutablePath: target.ExecutablePath,
		ProfilePath: target.ProfilePath, ProfileDirectory: target.ProfileDirectory}
}

// DiscoverTargets finds browsers and existing profiles on this machine.
// It never starts a browser or reads browsing history or cookies.
func DiscoverTargets() ([]BrowserTarget, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return discoverTargets(runtime.GOOS, home, nil), nil
}

// ResolveTarget requires an exact ID from a fresh local discovery result.
func ResolveTarget(id string) (BrowserTarget, error) {
	if id == "" {
		return BrowserTarget{}, errors.New("browser target ID is required")
	}
	targets, err := DiscoverTargets()
	if err != nil {
		return BrowserTarget{}, err
	}
	for _, target := range targets {
		if target.ID == id {
			return target, nil
		}
	}
	return BrowserTarget{}, fmt.Errorf("browser target %q is no longer available; run cymonkey browser targets again or supply explicit paths", id)
}

func discoverTargets(platform, home string, appRoots []string) []BrowserTarget {
	var targets []BrowserTarget
	var profilesRoot string
	var chromiumRoots map[string]string
	switch platform {
	case "darwin":
		if appRoots == nil {
			appRoots = []string{"/Applications", filepath.Join(home, "Applications"), "/System/Applications"}
		}
		support := filepath.Join(home, "Library", "Application Support")
		chromiumRoots = map[string]string{
			"chrome":   filepath.Join(support, "Google", "Chrome"),
			"chromium": filepath.Join(support, "Chromium"),
			"edge":     filepath.Join(support, "Microsoft Edge"),
		}
		profilesRoot = filepath.Join(support, "Firefox")
	case "linux":
		config := os.Getenv("CHROME_CONFIG_HOME")
		if config == "" {
			config = os.Getenv("XDG_CONFIG_HOME")
		}
		if config == "" {
			config = filepath.Join(home, ".config")
		}
		chromiumRoots = map[string]string{
			"chrome":   filepath.Join(config, "google-chrome"),
			"chromium": filepath.Join(config, "chromium"),
			"edge":     filepath.Join(config, "microsoft-edge"),
		}
		profilesRoot = filepath.Join(home, ".mozilla", "firefox")
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		roaming := os.Getenv("APPDATA")
		chromiumRoots = map[string]string{
			"chrome":   filepath.Join(local, "Google", "Chrome", "User Data"),
			"chromium": filepath.Join(local, "Chromium", "User Data"),
			"edge":     filepath.Join(local, "Microsoft", "Edge", "User Data"),
		}
		profilesRoot = filepath.Join(roaming, "Mozilla", "Firefox")
	default:
		return nil
	}
	for _, browser := range []string{"chrome", "chromium", "edge", "firefox", "safari"} {
		for _, executable := range browserExecutables(platform, appRoots, browser) {
			switch browser {
			case "safari":
				targets = append(targets, makeTarget(browser, executable, "", "", "Safari"))
			case "firefox":
				for _, profile := range firefoxProfiles(profilesRoot) {
					targets = append(targets, makeTarget(browser, executable, profile.path, "", "Firefox — "+profile.name))
				}
			default:
				for _, directory := range chromiumProfiles(chromiumRoots[browser]) {
					targets = append(targets, makeTarget(browser, executable, chromiumRoots[browser], directory, strings.ToUpper(browser[:1])+browser[1:]+" — "+directory))
				}
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Browser != targets[j].Browser {
			return targets[i].Browser < targets[j].Browser
		}
		if targets[i].ProfilePath != targets[j].ProfilePath {
			return targets[i].ProfilePath < targets[j].ProfilePath
		}
		return targets[i].ExecutablePath < targets[j].ExecutablePath
	})
	return targets
}

func makeTarget(browser, executable, profile, directory, name string) BrowserTarget {
	key := strings.Join([]string{browser, executable, profile, directory}, "\x00")
	sum := sha256.Sum256([]byte(key))
	target := BrowserTarget{ID: browser + ":" + hex.EncodeToString(sum[:12]), Browser: browser,
		Name: name, ExecutablePath: executable, ProfilePath: profile, ProfileDirectory: directory,
		InstallMode: "native"}
	if browser == "safari" {
		target.InstallMode = "app-handoff"
		target.Reason = "Safari profile enablement is selected in Safari Settings"
	} else if browser == "chrome" && isDefaultChromeUserDataDir(profile) {
		target.InstallMode = "manual-stage"
		target.Reason = "Chrome does not allow debugging-pipe verification in its default user data directory"
	} else if !Capability(browser).PersistentLocalInstall {
		target.InstallMode = "unsupported"
		target.Reason = Capability(browser).Reason
	}
	return target
}

func browserExecutables(platform string, appRoots []string, browser string) []string {
	var candidates []string
	switch platform {
	case "darwin":
		apps := map[string][]string{
			"chrome":   {"Google Chrome.app/Contents/MacOS/Google Chrome", "Chrome.app/Contents/MacOS/Google Chrome"},
			"chromium": {"Chromium.app/Contents/MacOS/Chromium"},
			"edge":     {"Microsoft Edge.app/Contents/MacOS/Microsoft Edge"},
			"firefox":  {"Firefox.app/Contents/MacOS/firefox"},
			"safari":   {"Safari.app/Contents/MacOS/Safari"},
		}
		for _, root := range appRoots {
			for _, suffix := range apps[browser] {
				candidates = append(candidates, filepath.Join(root, suffix))
			}
		}
	case "linux":
		commands := map[string][]string{"chrome": {"google-chrome", "google-chrome-stable"}, "chromium": {"chromium", "chromium-browser"}, "edge": {"microsoft-edge", "microsoft-edge-stable"}, "firefox": {"firefox"}}
		for _, name := range commands[browser] {
			if path, err := exec.LookPath(name); err == nil {
				candidates = append(candidates, path)
			}
		}
	case "windows":
		roots := []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")}
		suffixes := map[string][]string{
			"chrome":   {"Google/Chrome/Application/chrome.exe"},
			"chromium": {"Chromium/Application/chrome.exe"},
			"edge":     {"Microsoft/Edge/Application/msedge.exe"},
			"firefox":  {"Mozilla Firefox/firefox.exe"},
		}
		for _, root := range roots {
			if root == "" {
				continue
			}
			for _, suffix := range suffixes[browser] {
				candidates = append(candidates, filepath.Join(root, filepath.FromSlash(suffix)))
			}
		}
	}
	seen := map[string]bool{}
	var found []string
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
			absolute = resolved
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.Mode().IsRegular() || (platform != "windows" && info.Mode()&0111 == 0) || seen[absolute] {
			continue
		}
		seen[absolute] = true
		found = append(found, absolute)
	}
	return found
}

func chromiumProfiles(root string) []string {
	if !filepath.IsAbs(root) {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var profiles []string
	for _, entry := range entries {
		if entry.IsDir() && (entry.Name() == "Default" || strings.HasPrefix(entry.Name(), "Profile ")) {
			profiles = append(profiles, entry.Name())
		}
	}
	sort.Strings(profiles)
	return profiles
}

type namedProfile struct{ name, path string }

func firefoxProfiles(root string) []namedProfile {
	if !filepath.IsAbs(root) {
		return nil
	}
	seen := map[string]bool{}
	var profiles []namedProfile
	file, err := os.Open(filepath.Join(root, "profiles.ini"))
	if err == nil {
		defer file.Close()
		section := ""
		values := map[string]string{}
		add := func() {
			if !strings.HasPrefix(section, "Profile") || values["Path"] == "" {
				return
			}
			path := values["Path"]
			if values["IsRelative"] != "0" {
				path = filepath.Join(root, filepath.FromSlash(path))
			}
			appendFirefoxProfile(&profiles, seen, path, values["Name"])
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				add()
				section = strings.Trim(line, "[]")
				values = map[string]string{}
				continue
			}
			if key, value, ok := strings.Cut(line, "="); ok {
				values[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
		add()
	}
	entries, _ := os.ReadDir(filepath.Join(root, "Profiles"))
	for _, entry := range entries {
		if entry.IsDir() {
			appendFirefoxProfile(&profiles, seen, filepath.Join(root, "Profiles", entry.Name()), entry.Name())
		}
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].path < profiles[j].path })
	return profiles
}

func appendFirefoxProfile(profiles *[]namedProfile, seen map[string]bool, path, name string) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() || seen[absolute] {
		return
	}
	if name == "" {
		name = filepath.Base(absolute)
	}
	seen[absolute] = true
	*profiles = append(*profiles, namedProfile{name: name, path: absolute})
}
