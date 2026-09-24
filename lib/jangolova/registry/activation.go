package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"cymonkey/lib/jangolova/contract"
	"cymonkey/lib/jangolova/sdk"
)

// Activation is an explicit host decision about one reviewed artifact. Mount
// receives the verified bytes, never a mutable cache path or executable command.
// It owns loading into a caller-owned runtime and returning the module session.
type Activation struct {
	Approve  func(context.Context, Module, Artifact) error
	Mount    func(context.Context, Module, io.Reader) (sdk.EngineInstance, error)
	MaxBytes int64
}

// Activate re-verifies a selected cached artifact immediately before mounting.
// Metadata discovery and Pull never call this function implicitly. Approval is
// required even when the downloaded digest matches.
func Activate(ctx context.Context, module Module, platform, cachedPath string, host Activation) (sdk.EngineInstance, error) {
	if host.Approve == nil || host.Mount == nil {
		return nil, errors.New("explicit host approval and mount callbacks are required")
	}
	if err := (Registry{SchemaVersion: SchemaVersion, RegistryID: "activation", Modules: []Module{module}}).Validate(); err != nil {
		return nil, err
	}
	if module.Status != "available" {
		return nil, errors.New("only available modules may be activated")
	}
	if !contains(module.Platforms, platform) {
		return nil, errors.New("module platform does not match activation platform")
	}
	var selected *Artifact
	for _, a := range module.Artifacts {
		if a.Platform == platform {
			if selected != nil {
				return nil, errors.New("ambiguous activation artifact")
			}
			copy := a
			selected = &copy
		}
	}
	if selected == nil {
		return nil, errors.New("no artifact for activation platform")
	}
	limit := host.MaxBytes
	if limit <= 0 {
		limit = 64 << 20
	}
	file, err := os.Open(cachedPath)
	if err != nil {
		return nil, err
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	file.Close()
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, errors.New("activation artifact exceeds byte limit")
	}
	hash := sha256.Sum256(content)
	if !strings.EqualFold(hex.EncodeToString(hash[:]), selected.SHA256) {
		return nil, errors.New("cached activation artifact digest mismatch")
	}
	if selected.Size > 0 && selected.Size != int64(len(content)) {
		return nil, errors.New("activation artifact size mismatch")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Supply separate metadata copies: approval cannot change what is mounted.
	clone := func() Module { b, _ := json.Marshal(module); var v Module; _ = json.Unmarshal(b, &v); return v }
	if err := host.Approve(ctx, clone(), *selected); err != nil {
		return nil, fmt.Errorf("module activation denied: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	instance, err := host.Mount(ctx, clone(), bytes.NewReader(content))
	if err != nil {
		if instance != nil {
			_ = instance.Disconnect(context.Background())
		}
		return nil, err
	}
	if instance == nil {
		return nil, errors.New("module mount returned no session")
	}
	caller, ok := instance.(sdk.Caller)
	if !ok {
		_ = instance.Disconnect(context.Background())
		return nil, errors.New("module session does not implement semantic calls")
	}
	fail := func(err error) (sdk.EngineInstance, error) {
		_ = instance.Disconnect(context.Background())
		return nil, err
	}
	raw, err := caller.Call(ctx, "hello", json.RawMessage(`{}`))
	if err != nil {
		return fail(err)
	}
	var hello contract.Hello
	if json.Unmarshal(raw, &hello) != nil || contract.ValidateHello(hello) != nil || hello.ProtocolVersion != module.ProtocolVersion || !contains(hello.Runtimes, module.Runtime) {
		return fail(errors.New("activated module hello does not match selected metadata"))
	}
	raw, err = caller.Call(ctx, "capabilities", json.RawMessage(`{}`))
	if err != nil {
		return fail(err)
	}
	var caps []contract.Capability
	if json.Unmarshal(raw, &caps) != nil || contract.ValidateCapabilities(caps) != nil {
		return fail(errors.New("activated module capabilities are invalid"))
	}
	for _, action := range module.Actions {
		found := false
		for _, cap := range caps {
			if cap.Name == action && cap.Runtime == module.Runtime {
				found = true
				break
			}
		}
		if !found {
			return fail(fmt.Errorf("activated module is missing action %q", action))
		}
	}
	for _, method := range []string{"describe", "health"} {
		raw, err = caller.Call(ctx, method, json.RawMessage(`{}`))
		var obj map[string]json.RawMessage
		if err != nil {
			return fail(err)
		}
		if json.Unmarshal(raw, &obj) != nil || obj == nil {
			return fail(fmt.Errorf("activated module %s response is invalid", method))
		}
		if method == "describe" {
			var description contract.Description
			if json.Unmarshal(raw, &description) != nil || description.Revision == "" || description.Surfaces == nil {
				return fail(errors.New("activated module description is missing revision or surfaces"))
			}
		}
		if method == "health" {
			var health struct {
				Status    string `json:"status"`
				Connected bool   `json:"connected"`
			}
			if json.Unmarshal(raw, &health) != nil || !(health.Connected || health.Status == "ready" || health.Status == "healthy" || health.Status == "connected") {
				return fail(errors.New("activated module is not healthy"))
			}
		}
	}
	return instance, nil
}
