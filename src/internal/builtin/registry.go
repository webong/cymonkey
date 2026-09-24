// Package builtin registers the provider engines available to the Cymonkey
// host. Target runtimes are deliberately absent from this registry.
package builtin

import (
	"fmt"

	cymonkey "jangolova"
	"cymonkey/src/adapters/displayinteraction"
	"cymonkey/src/adapters/safarimcp"
	"cymonkey/src/adapters/webdriverclassic"
	"cymonkey/src/adapters/webpresentation"
	"cymonkey/src/internal/hostbinding"
	"cymonkey/src/internal/orchestrator"
)

func EngineRegistry() (*orchestrator.Registry, error) {
	registry := orchestrator.NewRegistry()
	playwright, err := cymonkey.WithDriver("playwright", hostbinding.Services())
	if err != nil {
		return nil, fmt.Errorf("configure Cymonkey Playwright control plane: %w", err)
	}
	if err := registry.RegisterEngine("playwright", hostbinding.Wrap(playwright)); err != nil {
		return nil, fmt.Errorf("register Playwright control-plane engine: %w", err)
	}
	puppeteer, err := cymonkey.WithDriver("puppeteer", hostbinding.Services())
	if err != nil {
		return nil, fmt.Errorf("configure Cymonkey Puppeteer control plane: %w", err)
	}
	if err := registry.RegisterEngine("puppeteer", hostbinding.Wrap(puppeteer)); err != nil {
		return nil, fmt.Errorf("register Puppeteer control-plane engine: %w", err)
	}
	if err := registry.RegisterEngine("cymonkey", hostbinding.Wrap(cymonkey.Adapter{Host: hostbinding.Services()})); err != nil {
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
	return registry, nil
}
