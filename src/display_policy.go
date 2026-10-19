package main

import (
	"cymonkey/src/internal/orchestrator"
	"cymonkey/src/internal/targetconn"
)

// displayPolicy is the host's endpoint and credential policy, offered to display
// engine packages through the display.host contract.
type displayPolicy struct{}

func (displayPolicy) ValidateEndpoint(endpoint orchestrator.TargetEndpoint) error {
	return targetconn.Validate(endpoint)
}

func (displayPolicy) WorkerEnvironment(endpoint orchestrator.TargetEndpoint, environment []string) ([]string, error) {
	return targetconn.NodeEnvironment(endpoint, environment)
}

func (displayPolicy) Redact(message string, target orchestrator.EngineTarget) string {
	return targetconn.RedactString(message, target)
}

// displayLaunchEnvironment gives a display engine package what it needs from the
// host's environment: the browser worker script, when one can be found. The
// package otherwise starts with ext's scrubbed environment.
func displayLaunchEnvironment() []string {
	worker, err := resolveBrowserWorker("browser")
	if err != nil {
		return nil
	}
	return []string{"CYMONKEY_WORKER=" + worker}
}
