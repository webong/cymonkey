package browserextension

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var safariBundleID = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
var safariCDHash = regexp.MustCompile(`(?m)^CDHash=([0-9a-fA-F]+)$`)

// PackageSafariProject converts caller-owned WebExtension files to a caller-owned
// Xcode project. The caller remains responsible for building and signing its app.
func PackageSafariProject(ctx context.Context, source, projectLocation, expectedRevision, bundleID, appName string) (InstallResult, error) {
	if runtime.GOOS != "darwin" {
		return InstallResult{}, errors.New("Safari packaging requires macOS and Xcode")
	}
	if !filepath.IsAbs(projectLocation) || !safariBundleID.MatchString(bundleID) || strings.TrimSpace(appName) == "" || filepath.Base(appName) != appName || appName == "." || appName == ".." {
		return InstallResult{}, errors.New("Safari packaging requires an absolute project location, bundle ID, and app name")
	}
	if sourcePath, err := filepath.Abs(source); err == nil && (projectLocation == sourcePath || strings.HasPrefix(projectLocation, sourcePath+string(filepath.Separator))) {
		return InstallResult{}, errors.New("Safari project location must be outside the extension source")
	}
	if expectedRevision == "" {
		return InstallResult{}, errors.New("Safari packaging requires a prepared revision")
	}
	if _, err := os.Stat(projectLocation); err == nil {
		return InstallResult{}, errors.New("Safari project location already exists")
	} else if !os.IsNotExist(err) {
		return InstallResult{}, err
	}
	description, err := Inspect(source)
	if err != nil {
		return InstallResult{}, err
	}
	if description.Revision != expectedRevision {
		return InstallResult{}, errors.New("Safari extension changed since inspection")
	}
	staged, err := os.MkdirTemp("", "jangolova-safari-extension-")
	if err != nil {
		return InstallResult{}, err
	}
	defer os.RemoveAll(staged)
	if _, err := StageWithRevision(source, filepath.Join(staged, "extension"), expectedRevision); err != nil {
		return InstallResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(projectLocation), 0755); err != nil {
		return InstallResult{}, err
	}
	tool := "safari-web-extension-packager"
	if _, err := exec.CommandContext(ctx, "xcrun", "-f", tool).Output(); err != nil {
		tool = "safari-web-extension-converter"
	}
	command := exec.CommandContext(ctx, "xcrun", tool,
		"--project-location", projectLocation, "--app-name", appName,
		"--bundle-identifier", bundleID, "--macos-only", "--copy-resources",
		"--no-open", "--no-prompt", filepath.Join(staged, "extension"))
	if output, err := command.CombinedOutput(); err != nil {
		return InstallResult{}, fmt.Errorf("Safari project conversion failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if current, err := Inspect(source); err != nil || current.Revision != expectedRevision {
		return InstallResult{}, errors.New("Safari extension changed during conversion")
	}
	return InstallResult{Status: "packaged", Browser: "safari", Source: projectLocation,
		Extension: &description, NextAction: "Build and sign the generated macOS app with your Apple developer identity, then use extension prepare and extension install with the built .app."}, nil
}

// InspectSafariApp reads a signed app's native Safari web extension identity.
// A CDHash binds the install request to the reviewed signed app bundle.
func InspectSafariApp(ctx context.Context, appPath string) (Description, error) {
	if runtime.GOOS != "darwin" {
		return Description{}, errors.New("Safari app inspection requires macOS")
	}
	if !filepath.IsAbs(appPath) || !strings.EqualFold(filepath.Ext(appPath), ".app") {
		return Description{}, errors.New("Safari source must be an absolute .app path")
	}
	info, err := os.Lstat(appPath)
	if err != nil {
		return Description{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Description{}, errors.New("Safari app must be a directory, not a symlink")
	}
	if output, err := exec.CommandContext(ctx, "codesign", "--verify", "--deep", "--strict", appPath).CombinedOutput(); err != nil {
		return Description{}, fmt.Errorf("Safari app signature verification failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	output, err := exec.CommandContext(ctx, "codesign", "-dv", "--verbose=4", appPath).CombinedOutput()
	if err != nil {
		return Description{}, err
	}
	match := safariCDHash.FindSubmatch(output)
	if len(match) != 2 {
		return Description{}, errors.New("Safari app has no code-signing hash")
	}
	plugins := filepath.Join(appPath, "Contents", "PlugIns")
	entries, err := os.ReadDir(plugins)
	if err != nil {
		return Description{}, err
	}
	var extensionID string
	for _, entry := range entries {
		if !entry.IsDir() || filepath.Ext(entry.Name()) != ".appex" {
			continue
		}
		plist := filepath.Join(plugins, entry.Name(), "Contents", "Info.plist")
		point, err := safariPlist(ctx, plist, "NSExtension.NSExtensionPointIdentifier")
		if err != nil || point != "com.apple.Safari.web-extension" {
			continue
		}
		if extensionID != "" {
			return Description{}, errors.New("Safari app contains more than one web extension; select a single-extension app")
		}
		extensionID, err = safariPlist(ctx, plist, "CFBundleIdentifier")
		if err != nil {
			return Description{}, err
		}
	}
	if extensionID == "" {
		return Description{}, errors.New("Safari app does not contain a Safari web extension")
	}
	name, err := safariPlist(ctx, filepath.Join(appPath, "Contents", "Info.plist"), "CFBundleName")
	if err != nil {
		name = filepath.Base(appPath)
	}
	return Description{Name: name, ID: extensionID, Revision: "cdhash:" + strings.ToLower(string(match[1]))}, nil
}

func safariPlist(ctx context.Context, path, key string) (string, error) {
	output, err := exec.CommandContext(ctx, "plutil", "-extract", key, "raw", "-o", "-", path).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// InstallSafariApp registers and runs the caller's signed containing app.
// Safari's checkbox and per-profile permissions are controlled by the user.
func InstallSafariApp(ctx context.Context, appPath, expectedRevision string) (InstallResult, error) {
	if expectedRevision == "" {
		return InstallResult{}, errors.New("Safari installation requires a prepared app revision")
	}
	description, err := InspectSafariApp(ctx, appPath)
	if err != nil {
		return InstallResult{}, err
	}
	if description.Revision != expectedRevision {
		return InstallResult{}, errors.New("Safari app changed since inspection")
	}
	if output, err := exec.CommandContext(ctx, "open", appPath).CombinedOutput(); err != nil {
		return InstallResult{}, fmt.Errorf("launch Safari extension app: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return InstallResult{Status: "awaiting-browser-action", Browser: "safari", ID: description.ID,
		Source: appPath, Extension: &description,
		NextAction: "Open Safari > Settings > Extensions, enable this extension, then allow it for the intended Safari profile and websites. Safari controls those permissions."}, nil
}
