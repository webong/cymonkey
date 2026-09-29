package main

import "testing"

func TestHelperConfigValidation(t *testing.T) {
	config := helperConfig{AllowedExecutableNames: []string{"NOTEPAD.EXE", "notepad.exe"}, Viewer: &viewerPolicy{
		Enabled: true, AllowCapture: true, AllowInput: true, BlockedKeys: []string{" Alt+F4 ", "alt+f4"},
	}}
	if err := config.validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if got, want := len(config.AllowedExecutableNames), 1; got != want {
		t.Fatalf("allowed executable count = %d, want %d", got, want)
	}
	if !config.allowsExecutable("notepad.exe") || config.allowsExecutable("cmd.exe") {
		t.Fatal("executable allowlist is not enforced")
	}
}

func TestHelperConfigRejectsUnsafeExecutable(t *testing.T) {
	config := helperConfig{AllowedExecutableNames: []string{`C:\\Windows\\System32\\notepad.exe`}}
	if err := config.validate(); err == nil {
		t.Fatal("expected executable path to be rejected")
	}
}
