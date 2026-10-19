package main

import (
	"fmt"

	"cymonkey/src/adapters/displayinteraction"
	"cymonkey/src/adapters/safarimcp"
	"cymonkey/src/adapters/webdriverclassic"
	"cymonkey/src/adapters/webpresentation"
	"cymonkey/src/internal/orchestrator"
)

func engineRegistry() (*orchestrator.Registry, error) {
	registry := orchestrator.NewRegistry()
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
