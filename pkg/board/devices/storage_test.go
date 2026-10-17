package devices

import (
	"context"
	"errors"
	"io"
	"kit/devicekit"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorageTransferAndCommitGrants(t *testing.T) {
	ctx := context.Background()
	src, dst := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "example"), []byte("board-owned"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := Storage(StorageOptions{ID: "source", RootID: "root", Path: src})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Storage(StorageOptions{ID: "destination", RootID: "root", Path: dst, AllowRestore: true})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := devicekit.NewRegistry(a, b)
	if err != nil {
		t.Fatal(err)
	}
	open := func(provider string, caps ...devicekit.Capability) *devicekit.Connection {
		c, err := registry.Open(ctx, devicekit.OpenRequest{ProviderID: provider, DeviceID: "volume", Grant: devicekit.Grant{Capabilities: caps, ResourceIDs: []string{"root"}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close(ctx) })
		return c
	}
	source := open("source", devicekit.StorageExport)
	content, err := source.Stream(ctx, devicekit.Action{Capability: devicekit.StorageExport, ResourceID: "root"})
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	archive, err := io.ReadAll(content)
	if err != nil {
		t.Fatal(err)
	}
	denied := open("destination", StorageUpload)
	if _, err := denied.Invoke(ctx, devicekit.Action{Capability: devicekit.StorageRestore, ResourceID: "root"}); !errors.Is(err, devicekit.ErrNotGranted) {
		t.Fatal(err)
	}
	target := open("destination", StorageUpload, devicekit.StorageRestore)
	ch, err := target.OpenChannel(ctx, devicekit.Action{Capability: StorageUpload, ResourceID: "root"}, devicekit.ChannelLimits{WriteBytes: 1 << 20, Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ch.Write(archive); err != nil {
		t.Fatal(err)
	}
	if _, err = target.Invoke(ctx, devicekit.Action{Capability: devicekit.StorageRestore, ResourceID: "root"}); err == nil {
		t.Fatal("restore accepted active upload")
	}
	if err = ch.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = target.Invoke(ctx, devicekit.Action{Capability: devicekit.StorageRestore, ResourceID: "root"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "example"))
	if err != nil || string(data) != "board-owned" {
		t.Fatalf("restored %q %v", data, err)
	}
	if _, err = target.Invoke(ctx, devicekit.Action{Capability: devicekit.StorageRestore, ResourceID: "root"}); err == nil {
		t.Fatal("restore overwrote a nonempty root")
	}
}
