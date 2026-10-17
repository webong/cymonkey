package main

import (
	"fmt"

	displaylib "board/display"
	"cymonkey/src/adapters/displayinteraction"
	"cymonkey/src/adapters/safarimcp"
	"cymonkey/src/adapters/webdriverclassic"
	"cymonkey/src/adapters/webpresentation"
	"cymonkey/src/internal/orchestrator"
)

func engineRegistry() (*orchestrator.Registry, error) {
	registry := orchestrator.NewRegistry()
	playwright, err := displaylib.WithDriver("playwright", displayLibraryServices())
	if err != nil {
		return nil, fmt.Errorf("configure Cymonkey Playwright control plane: %w", err)
	}
	if err := registry.RegisterEngine("playwright", wrapDisplayLibrary(playwright)); err != nil {
		return nil, fmt.Errorf("register Playwright control-plane engine: %w", err)
	}
	puppeteer, err := displaylib.WithDriver("puppeteer", displayLibraryServices())
	if err != nil {
		return nil, fmt.Errorf("configure Cymonkey Puppeteer control plane: %w", err)
	}
	if err := registry.RegisterEngine("puppeteer", wrapDisplayLibrary(puppeteer)); err != nil {
		return nil, fmt.Errorf("register Puppeteer control-plane engine: %w", err)
	}
	if err := registry.RegisterEngine("cymonkey", wrapDisplayLibrary(displaylib.Adapter{Host: displayLibraryServices()})); err != nil {
		return nil, fmt.Errorf("register Cymonkey augmented-browsing engine: %w", err)
	}
	if err := registry.RegisterEngine("webdriver-classic", webdriverclassic.Generic()); err != nil {
		return nil, fmt.Errorf("register WebDriver Classic interaction engine: %w", err)
	}
	if err := registry.RegisterEngine("webkit-webdriver", webdriverclassic.WebKit()); err != nil {
		return nil, fmt.Errorf("register WebKit WebDriver interaction engine: %w", err)
	}
	if err := registry.RegisterEngine("safari-mcp", safarimcp.Adapter{}); err != nil {
		return nil, fmt.Errorf("register Safari MCP interaction engine: %w", err)
	}
	if err := registry.RegisterEngine("web-presentation", webpresentation.Adapter{}); err != nil {
		return nil, fmt.Errorf("register web presentation interaction engine: %w", err)
	}
	if err := registry.RegisterEngine("display-interaction", displayinteraction.Adapter{}); err != nil {
		return nil, fmt.Errorf("register display interaction engine: %w", err)
	}
	if err := registerDisplayPlugins(registry); err != nil {
		return nil, err
	}
	return registry, nil
}
