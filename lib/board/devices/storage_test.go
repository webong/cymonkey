package devices

import (
	"board"
	"context"
	"errors"
	"io"
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
	registry, err := board.NewRegistry(a, b)
	if err != nil {
		t.Fatal(err)
	}
	open := func(provider string, caps ...board.Capability) *board.Connection {
		c, err := registry.Open(ctx, board.OpenRequest{ProviderID: provider, DeviceID: "volume", Grant: board.Grant{Capabilities: caps, ResourceIDs: []string{"root"}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close(ctx) })
		return c
	}
	source := open("source", board.StorageExport)
	content, err := source.Stream(ctx, board.Action{Capability: board.StorageExport, ResourceID: "root"})
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	archive, err := io.ReadAll(content)
	if err != nil {
		t.Fatal(err)
	}
	denied := open("destination", StorageUpload)
	if _, err := denied.Invoke(ctx, board.Action{Capability: board.StorageRestore, ResourceID: "root"}); !errors.Is(err, board.ErrNotGranted) {
		t.Fatal(err)
	}
	target := open("destination", StorageUpload, board.StorageRestore)
	ch, err := target.OpenChannel(ctx, board.Action{Capability: StorageUpload, ResourceID: "root"}, board.ChannelLimits{WriteBytes: 1 << 20, Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ch.Write(archive); err != nil {
		t.Fatal(err)
	}
	if _, err = target.Invoke(ctx, board.Action{Capability: board.StorageRestore, ResourceID: "root"}); err == nil {
		t.Fatal("restore accepted active upload")
	}
	if err = ch.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = target.Invoke(ctx, board.Action{Capability: board.StorageRestore, ResourceID: "root"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "example"))
	if err != nil || string(data) != "board-owned" {
		t.Fatalf("restored %q %v", data, err)
	}
	if _, err = target.Invoke(ctx, board.Action{Capability: board.StorageRestore, ResourceID: "root"}); err == nil {
		t.Fatal("restore overwrote a nonempty root")
	}
}
