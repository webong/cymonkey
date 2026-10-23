// mobile serves mobile targets as an ext package that speaks the mobile.target
// contract. The host starts one process per target; the process owns the WebRTC
// peer and the media conversion, and the host relays the app's frames to it.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"board/mobiletarget"
	"jangolova/mobilekit"

	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"github.com/webong/ext/pkg/plugin-go/contract/transport/jsonline"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "board mobile:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	descriptor, err := packagekit.LoadDescriptor(filepath.Join(filepath.Dir(os.Args[0]), store.ManifestName))
	if err != nil {
		return err
	}
	if !(packagekit.Manifest{Descriptor: descriptor}).Supports(mobilekit.Contract, mobilekit.Version) {
		return errors.New("plugin manifest does not declare the mobile.target contract")
	}
	server := mobiletarget.New()
	defer server.Close()
	return jsonline.ServeFunc(ctx, descriptor, server.Call, jsonline.FuncOptions{})
}
