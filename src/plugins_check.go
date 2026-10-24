package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
)

// pluginRole is one thing the host needs a package for: the contract the
// package serves, the commands that stop working without it, and the role name
// scripts/install-packages.sh takes to build and install it.
type pluginRole struct {
	role     string
	contract string
	needed   string
}

var pluginRoles = []pluginRole{
	{"display", displayPluginContract, "cymy display and browser commands"},
	{"device", devicePluginContract, "cymy device sessions and device inventory"},
	{"content", contentServiceContract, "cymy content"},
	{"console", consoleServiceContract, "cymy console"},
	{"gateway", gatewayServiceContract, "cymy device gateway"},
	{"mobile", mobileTargetContract, "POST /v1/device-targets (mobile apps)"},
	{"providers", contentPluginContract, "content models: object detection, LLM interpretation (optional)"},
}

// checkPlugins reports, for each role, the installed packages that serve it and
// how to install one when none does. Packages are read from their manifests;
// nothing is launched or verified.
func checkPlugins(installed []store.Installed, stdout io.Writer) {
	table := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	missing := 0
	for _, role := range pluginRoles {
		var names []string
		for _, item := range installed {
			if item.Supports(role.contract, pluginContractVersion) {
				names = append(names, item.Name())
			}
		}
		sort.Strings(names)
		status := "installed: " + strings.Join(names, ", ")
		if len(names) == 0 {
			status = "MISSING"
			missing++
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", role.role, role.contract, status, role.needed)
	}
	table.Flush()
	if missing > 0 {
		fmt.Fprintf(stdout, "\n%d missing; build and install with: scripts/install-packages.sh <role>...\n", missing)
	}
}
