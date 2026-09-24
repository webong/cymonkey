package provider

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"cymonkey/src/internal/builtin"
	"cymonkey/src/internal/engineprovider"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	"cymonkey/src/internal/userscripts"
	"cymonkey/src/targetconn"
)

func enginesCommand(args []string) error {
	flags := flag.NewFlagSet("engines", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "write the interaction-engine inventory as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("engines accepts flags only")
	}
	registry, err := builtin.EngineRegistry()
	if err != nil {
		return err
	}
	engines := engineprovider.DiscoverEngines(context.Background(), registry)
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"apiVersion": engineprovider.APIVersion,
			"engines":    engines,
		})
	}
	for _, engine := range engines {
		availability := "available"
		if !engine.Available {
			availability = "unavailable"
		}
		fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", engine.Adapter, availability, strings.Join(engine.Capabilities, ","))
	}
	return nil
}

// ConnectEngine attaches to a caller-owned browser from another local CLI flow.
func ConnectEngine(args []string, stdout io.Writer) error { return connectEngine(args, stdout) }

func connectEngineCommand(args []string) error { return connectEngine(args, os.Stdout) }

func connectEngine(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("connect-engine", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	adapterName := flags.String("adapter", "auto", "interaction-engine adapter name or auto")
	targetKind := flags.String("target-kind", "", "caller-owned target kind")
	source := flags.String("source", "", "optional resource to present after connecting")
	driverName := flags.String("driver", "", "driver backend for cymonkey control plane (auto, playwright, puppeteer, cdp, bidi)")
	optionsText := flags.String("options", "{}", "engine-specific options as a JSON object")
	disconnectTimeout := flags.Duration("disconnect-timeout", 15*time.Second, "maximum disconnect duration")
	userscriptTarget := flags.String("userscripts-target", "", "stable browser/profile ID whose approved userscripts should be replayed")
	defaultUserscriptStore, err := userscripts.DefaultDirectory()
	if err != nil {
		return err
	}
	userscriptStore := flags.String("userscripts-store", defaultUserscriptStore, "userscript store directory")
	var endpoints endpointFlags
	flags.Var(&endpoints, "endpoint", "caller-owned target endpoint in PROTOCOL=URL form; may be repeated")
	var handles handleFlags
	flags.Var(&handles, "handle", "opaque caller-owned target handle in NAME=VALUE form; may be repeated")
	var requiredCapabilities stringFlags
	flags.Var(&requiredCapabilities, "require-capability", "capability required when selecting an adapter; may be repeated")
	var credentialRefs referenceFlags
	flags.Var(&credentialRefs, "credential-ref", "endpoint credential reference in NAME=REFERENCE form; may be repeated")
	var tlsRefs referenceFlags
	flags.Var(&tlsRefs, "tls-ref", "endpoint TLS reference in NAME=REFERENCE form; may be repeated")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("connect-engine accepts flags only")
	}
	if strings.TrimSpace(*targetKind) == "" {
		return errors.New("--target-kind is required")
	}
	if *userscriptTarget != "" && *targetKind != "browser" {
		return errors.New("--userscripts-target requires --target-kind browser")
	}
	if *userscriptTarget != "" {
		requiredCapabilities = append(requiredCapabilities, "script.register")
	}
	if *disconnectTimeout <= 0 {
		return errors.New("--disconnect-timeout must be positive")
	}
	optionsMap := map[string]any{}
	if strings.TrimSpace(*optionsText) != "" && strings.TrimSpace(*optionsText) != "{}" {
		if err := json.Unmarshal([]byte(*optionsText), &optionsMap); err != nil {
			return fmt.Errorf("decode --options JSON: %w", err)
		}
	}
	if strings.TrimSpace(*driverName) != "" {
		optionsMap["driver"] = strings.TrimSpace(*driverName)
	}
	options, err := json.Marshal(optionsMap)
	if err != nil {
		return err
	}
	registry, err := builtin.EngineRegistry()
	if err != nil {
		return err
	}
	selectedAdapter := strings.TrimSpace(*adapterName)
	if selectedAdapter == "auto" {
		providerEndpoints := make([]engineprovider.TargetEndpoint, 0, len(endpoints))
		for _, endpoint := range endpoints {
			providerEndpoints = append(providerEndpoints, engineprovider.TargetEndpoint{
				Name: endpoint.Name, Protocol: endpoint.Protocol, URL: endpoint.URL,
			})
		}
		selectedAdapter, err = engineprovider.SelectAutomaticEngine(context.Background(), registry, engineprovider.Target{
			Kind: strings.TrimSpace(*targetKind), Endpoints: providerEndpoints,
		}, requiredCapabilities)
		if err != nil {
			return err
		}
	}
	adapter, ok := registry.Engine(selectedAdapter)
	if !ok {
		return fmt.Errorf("interaction engine %q is not registered", selectedAdapter)
	}
	targetEndpoints := endpoints.clone()
	if err := applyEndpointReferences(targetEndpoints, credentialRefs, tlsRefs); err != nil {
		return err
	}
	target, release, err := targetconn.Prepare(context.Background(), targetconn.DefaultResolver(), orchestrator.EngineTarget{
		Kind: strings.TrimSpace(*targetKind), Endpoints: targetEndpoints,
		Handles: orchestrator.EngineHandles(handles.clone()),
	})
	if err != nil {
		return err
	}
	defer release(context.Background())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	instance, err := adapter.Connect(ctx, manifest.EngineSpec{
		Adapter:              selectedAdapter,
		RequiredCapabilities: append([]string(nil), requiredCapabilities...),
		Source:               strings.TrimSpace(*source),
		Options:              options,
	}, target)
	if err != nil {
		return fmt.Errorf("connect interaction engine: %w", targetconn.Redact(err, target))
	}
	if instance == nil {
		return errors.New("connect interaction engine: adapter returned no instance")
	}
	var appliedUserscripts map[string]userscripts.Record
	if *userscriptTarget != "" {
		appliedUserscripts, err = syncUserscripts(ctx, instance, *userscriptStore, *userscriptTarget, nil)
		if err != nil {
			_ = instance.Disconnect(context.Background())
			return fmt.Errorf("replay approved userscripts: %w", err)
		}
	}

	health := orchestrator.EngineHealth{
		Status:     orchestrator.EngineHealthUnknown,
		Message:    "interaction engine does not implement an active health probe",
		ObservedAt: time.Now().UTC(),
	}
	if provider, ok := instance.(orchestrator.EngineHealthProvider); ok {
		healthCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		health = provider.EngineHealth(healthCtx)
		cancel()
	}
	health.Message = targetconn.RedactString(health.Message, target)
	result := engineprovider.Instance{
		APIVersion: engineprovider.APIVersion,
		InstanceID: "standalone",
		Adapter:    selectedAdapter,
		Status:     "connected",
		Health: engineprovider.Health{
			Status: health.Status, Message: health.Message, ObservedAt: health.ObservedAt,
		},
	}
	if provider, ok := instance.(orchestrator.EngineCapabilityProvider); ok {
		result.Capabilities = provider.EngineCapabilities()
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		_ = instance.Disconnect(context.Background())
		return err
	}

	var terminal *orchestrator.EngineEvent
	var events <-chan orchestrator.EngineEvent
	if source, ok := instance.(orchestrator.EngineEventSource); ok {
		events = source.EngineEvents()
	}
	var syncTick <-chan time.Time
	if *userscriptTarget != "" {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		syncTick = ticker.C
	}
	for terminal == nil && ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-syncTick:
			appliedUserscripts, err = syncUserscripts(ctx, instance, *userscriptStore, *userscriptTarget, appliedUserscripts)
			if err != nil {
				_ = instance.Disconnect(context.Background())
				return fmt.Errorf("synchronize approved userscripts: %w", err)
			}
		case event, open := <-events:
			if !open {
				events = nil
				continue
			}
			event.Message = targetconn.RedactString(event.Message, target)
			if err := json.NewEncoder(stdout).Encode(engineEventValue(event)); err != nil {
				_ = instance.Disconnect(context.Background())
				return err
			}
			if event.Status == "disconnected" || event.Status == "failed" {
				terminal = &event
			}
		}
	}
	disconnectCtx, cancel := context.WithTimeout(context.Background(), *disconnectTimeout)
	defer cancel()
	if err := instance.Disconnect(disconnectCtx); err != nil && terminal == nil {
		return targetconn.Redact(err, target)
	}
	if err := release(disconnectCtx); err != nil {
		return errors.New("release target connection material")
	}
	if terminal != nil && terminal.Status == "failed" {
		return fmt.Errorf("interaction engine failed: %s", terminal.Message)
	}
	return nil
}

func engineEventValue(event orchestrator.EngineEvent) engineprovider.InstanceEvent {
	return engineprovider.InstanceEvent{Type: event.Type, Status: event.Status, Message: event.Message, OccurredAt: event.OccurredAt}
}

func decodeEngineOptions(value string) (json.RawMessage, error) {
	raw := json.RawMessage(strings.TrimSpace(value))
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, errors.New("--options must be a JSON object")
	}
	return raw, nil
}

var handleFlagNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)

type endpointFlags []orchestrator.TargetEndpoint

type referenceFlags map[string]string

func (r *referenceFlags) String() string { return fmt.Sprint(map[string]string(*r)) }
func (r *referenceFlags) Set(value string) error {
	name, reference, ok := strings.Cut(value, "=")
	if !ok || !handleFlagNamePattern.MatchString(name) || !handleFlagNamePattern.MatchString(reference) {
		return errors.New("connection reference must use NAME=REFERENCE")
	}
	if *r == nil {
		*r = referenceFlags{}
	}
	if _, exists := (*r)[name]; exists {
		return fmt.Errorf("connection reference for endpoint %s is repeated", name)
	}
	(*r)[name] = reference
	return nil
}

func applyEndpointReferences(endpoints []orchestrator.TargetEndpoint, credentials, tlsValues referenceFlags) error {
	found := make(map[string]bool, len(endpoints))
	for index := range endpoints {
		endpoint := &endpoints[index]
		found[endpoint.Name] = true
		endpoint.CredentialRef = credentials[endpoint.Name]
		endpoint.TLSRef = tlsValues[endpoint.Name]
	}
	for name := range credentials {
		if !found[name] {
			return fmt.Errorf("credential reference endpoint %q was not supplied", name)
		}
	}
	for name := range tlsValues {
		if !found[name] {
			return fmt.Errorf("TLS reference endpoint %q was not supplied", name)
		}
	}
	return nil
}

type stringFlags []string

func (s *stringFlags) String() string { return strings.Join(*s, ",") }
func (s *stringFlags) Set(value string) error {
	value = strings.TrimSpace(value)
	if !handleFlagNamePattern.MatchString(value) {
		return errors.New("capability must use a protocol-style name")
	}
	*s = append(*s, value)
	return nil
}

func (e *endpointFlags) String() string { return fmt.Sprint([]orchestrator.TargetEndpoint(*e)) }

func (e *endpointFlags) Set(value string) error {
	protocol, endpointURL, ok := strings.Cut(value, "=")
	if !ok || !handleFlagNamePattern.MatchString(protocol) || strings.TrimSpace(endpointURL) == "" {
		return errors.New("--endpoint must use PROTOCOL=URL")
	}
	*e = append(*e, orchestrator.TargetEndpoint{Name: protocol, Protocol: protocol, URL: endpointURL})
	return nil
}

func (e endpointFlags) clone() []orchestrator.TargetEndpoint {
	return append([]orchestrator.TargetEndpoint(nil), e...)
}

type handleFlags map[string]string

func (h *handleFlags) String() string {
	values := make([]string, 0, len(*h))
	for name, value := range *h {
		values = append(values, name+"="+value)
	}
	return strings.Join(values, ",")
}

func (h *handleFlags) Set(value string) error {
	name, item, ok := strings.Cut(value, "=")
	if !ok || !handleFlagNamePattern.MatchString(name) || item == "" {
		return errors.New("--handle must use NAME=VALUE with a valid name and non-empty value")
	}
	if strings.ContainsRune(item, '\x00') {
		return fmt.Errorf("--handle %s contains a null byte", name)
	}
	if *h == nil {
		*h = handleFlags{}
	}
	(*h)[name] = item
	return nil
}

func (h handleFlags) clone() map[string]string {
	values := make(map[string]string, len(h))
	for name, value := range h {
		values[name] = value
	}
	return values
}
