package browserextension

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Run with CYMONKEY_BROWSER_BIN set to a local Chromium-family executable.
// This checks pipe-only session loading in an isolated profile, including its
// non-persistence after that browser session closes.
func TestRunWithDevToolsLive(t *testing.T) {
	executable := os.Getenv("CYMONKEY_BROWSER_BIN")
	if executable == "" {
		t.Skip("set CYMONKEY_BROWSER_BIN for a live browser installation test")
	}
	source := fixture(t)
	prepared, err := Inspect(source)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	profile := filepath.Join(root, "profile")
	ready := make(chan InstallResult, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunWithDevTools(ctx, DevToolsTarget{
			Browser: "chrome", ExecutablePath: executable,
			ProfilePath: profile, Headless: true,
		}, source, filepath.Join(root, "staged"), prepared.Revision, func(result InstallResult) error {
			ready <- result
			return nil
		})
	}()
	var result InstallResult
	select {
	case result = <-ready:
	case err := <-done:
		t.Fatalf("browser session failed before loading extension: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if result.Status != "activated" || result.ID == "" || result.Extension == nil || result.Extension.Revision != prepared.Revision {
		t.Fatalf("unexpected browser session: %+v", result)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// A new ordinary launch of the same profile must not be mistaken for an
	// installed extension merely because the prior session loaded it.
	restartContext, stopRestart := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopRestart()
	const verifyScript = `import puppeteer from 'puppeteer-core';
const browser = await puppeteer.launch({executablePath: process.argv[1], userDataDir: process.argv[2], headless: true, pipe: true, enableExtensions: true, args: ['--enable-unsafe-extension-debugging']});
try {
  const page = await browser.newPage();
  await page.goto('chrome://extensions/');
  await new Promise(resolve => setTimeout(resolve, 500));
  const names = await page.evaluate(() => {
    const out = [];
    const visit = node => {
      if (node.nodeType === Node.TEXT_NODE && node.textContent?.trim()) out.push(node.textContent.trim());
      if (node.shadowRoot) visit(node.shadowRoot);
      for (const child of node.childNodes || []) visit(child);
    };
    visit(document);
    return out.join(' ');
  });
  console.log(JSON.stringify({cdpIds: [...(await browser.extensions()).keys()], pageText: names}));
} finally { await browser.close(); }`
	command := exec.CommandContext(restartContext, "node", "--input-type=module", "-e", verifyScript, executable, profile)
	command.Dir = filepath.Join("..", "..", "..")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("reopen selected profile: %v: %s", err, output)
	}
	if bytes.Contains(output, []byte(result.ID)) || bytes.Contains(output, []byte(prepared.Name)) {
		t.Fatalf("session-only extension unexpectedly survived browser restart: %s", output)
	}
}
