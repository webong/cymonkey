package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"jangolova/bookmarklet"
)

func bookmarkletCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("bookmarklet requires export or import; use bookmarklet help")
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(stdout, "Usage: cmy bookmarklet export --source FILE [--format url|html] [--name NAME]\n       cmy bookmarklet import --source FILE\n\nExport writes a javascript: URL or a draggable installation page to stdout. Import decodes a javascript: URL to source for review. Neither command executes code or modifies browser bookmarks.")
		return err
	}
	if args[0] != "export" && args[0] != "import" {
		return fmt.Errorf("unknown bookmarklet command %q", args[0])
	}
	flags := flag.NewFlagSet("bookmarklet "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	sourcePath := flags.String("source", "", "source file (JavaScript for export; javascript: URL for import)")
	var format, name string
	if args[0] == "export" {
		flags.StringVar(&format, "format", "url", "url or html")
		flags.StringVar(&name, "name", "Jangolova bookmarklet", "installation link name")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *sourcePath == "" || flags.NArg() != 0 {
		return errors.New("bookmarklet requires --source FILE and accepts flags only")
	}
	if args[0] == "export" && format != "url" && format != "html" {
		return errors.New("bookmarklet --format must be url or html")
	}
	f, err := os.Open(*sourcePath)
	if err != nil {
		return err
	}
	limit := int64(bookmarklet.MaxSourceBytes)
	if args[0] == "import" {
		limit = 3*(limit+64) + 11
	}
	data, readErr := io.ReadAll(io.LimitReader(f, limit+1))
	closeErr := f.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if int64(len(data)) > limit {
		return errors.New("bookmarklet input is too large")
	}
	var output string
	if args[0] == "import" {
		output, err = bookmarklet.Decode(string(data))
	} else if format == "html" {
		output, err = bookmarklet.InstallPage(name, string(data))
	} else {
		output, err = bookmarklet.Encode(string(data))
		output += "\n"
	}
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, output)
	return err
}
