package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/webong/ext/res/web/contract"
)

const cookiesUsage = "cookies requires query or normalize"

// cookiesCommand reads or normalizes browser cookies through a trusted ext
// browser adapter. The adapter owns profile discovery, store formats and
// decrypting values; Cymonkey only selects the adapter and relays the request.
// The result holds live cookie values, so it goes to stdout only when asked for.
func cookiesCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 || (args[0] != "query" && args[0] != "normalize") {
		return errors.New(cookiesUsage)
	}
	flags := flag.NewFlagSet("cymy cookies "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	browserName := flags.String("browser", "", "browser adapter name, such as chrome, firefox, or safari")
	targetID := flags.String("target", "", "ID returned by cymy browser targets")
	profile := flags.String("profile", "", "absolute path to a custom browser user data directory or profile")
	profileDirectory := flags.String("profile-directory", "", "profile directory within the browser user data directory")
	site := flags.String("site", "", "http or https site whose cookies are read")
	includeExpired := flags.Bool("include-expired", false, "include expired cookies")
	allowAllHosts := flags.Bool("allow-all-hosts", false, "read cookies for every host when no --site is given")
	storeID := flags.String("store-id", "", "native cookie store ID (normalize)")
	inputFile := flags.String("input-file", "", "native browser cookie export to normalize; - reads stdin")
	var names stringList
	flags.Var(&names, "name", "cookie name to include; repeatable")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("cookies accepts flags only")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	selector := browserSelector{ID: *targetID, Browser: *browserName, ProfilePath: *profile, ProfileDirectory: *profileDirectory}
	endpoint, _, err := selector.endpoint(ctx)
	if err != nil {
		return err
	}
	request := contract.CookieRequest{Site: *site, Names: names, IncludeExpired: *includeExpired, AllowAllHosts: *allowAllHosts}
	if args[0] == "query" {
		if *site == "" && !*allowAllHosts {
			return errors.New("cookies query needs --site, or --allow-all-hosts to read every host")
		}
	} else {
		if *storeID == "" || *inputFile == "" {
			return errors.New("cookies normalize needs --store-id and --input-file")
		}
		export, err := readCookieExport(*inputFile)
		if err != nil {
			return err
		}
		request.StoreID, request.NativeExport = *storeID, export
	}
	result, err := browserHost().Cookie(ctx, endpoint, args[0], request)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

// readCookieExport reads one JSON value of at most 8 MiB from a file or stdin.
func readCookieExport(path string) (json.RawMessage, error) {
	var reader io.Reader = os.Stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, 8<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 8<<20 || !json.Valid(data) {
		return nil, errors.New("the cookie export must be one JSON value of at most 8 MiB")
	}
	return data, nil
}
