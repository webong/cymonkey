//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const protocolVersion = "cymonkey/v1alpha1"

func main() {
	environment := os.Environ()
	lookup := func(name string) string {
		for _, pair := range environment {
			if key, value, ok := strings.Cut(pair, "="); ok && key == name {
				return value
			}
		}
		return ""
	}
	if err := run(lookup); err != nil {
		// Never include the endpoint, token, native handle, or target-window
		// title in output from this helper.
		_, _ = fmt.Fprintln(os.Stderr, "Cymonkey Windows helper stopped:", safeMessage(err))
	}
}

func run(lookup func(string) string) error {
	endpoint, err := newControlEndpoint(lookup("JANGOLOVA_CYMONKEY_CONTROL_URL"), lookup("JANGOLOVA_CYMONKEY_CONTROL_TOKEN"))
	if err != nil {
		return err
	}
	if lookup("JANGOLOVA_CYMONKEY_PROTOCOL") != protocolVersion {
		return errors.New("required Cymonkey protocol launch configuration is missing")
	}
	config, err := loadConfig(lookup("JANGOLOVA_CYMONKEY_CONFIG"))
	if err != nil {
		return err
	}
	runtime := newWindowsRuntime(config)
	return endpoint.run(runtime)
}

func safeMessage(err error) string {
	if err == nil {
		return "native control connection ended"
	}
	return "native control connection or configuration failed"
}
