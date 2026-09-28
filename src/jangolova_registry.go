package main

import (
	"fmt"

	"cymonkey/src/adapters/displayinteraction"
	"cymonkey/src/adapters/safarimcp"
	"cymonkey/src/adapters/webdriverclassic"
	"cymonkey/src/adapters/webpresentation"
	"cymonkey/src/internal/orchestrator"
	cymonkey "jangolova"
)

func engineRegistry() (*orchestrator.Registry, error) {
	registry := orchestrator.NewRegistry()
	playwright, err := cymonkey.WithDriver("playwright", jangolovaServices())
	if err != nil {
		return nil, fmt.Errorf("configure Cymonkey Playwright control plane: %w", err)
	}
	if err := registry.RegisterEngine("playwright", wrapJangolova(playwright)); err != nil {
		return nil, fmt.Errorf("register Playwright control-plane engine: %w", err)
	}
	puppeteer, err := cymonkey.WithDriver("puppeteer", jangolovaServices())
	if err != nil {
		return nil, fmt.Errorf("configure Cymonkey Puppeteer control plane: %w", err)
	}
	if err := registry.RegisterEngine("puppeteer", wrapJangolova(puppeteer)); err != nil {
		return nil, fmt.Errorf("register Puppeteer control-plane engine: %w", err)
	}
	if err := registry.RegisterEngine("cymonkey", wrapJangolova(cymonkey.Adapter{Host: jangolovaServices()})); err != nil {
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
	if err := registerJangolovaPlugins(registry); err != nil {
		return nil, err
	}
	return registry, nil
}
