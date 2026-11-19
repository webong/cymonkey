package contentcmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"jangolova/contentkit/public"
	"net/url"
	"os"
	"strings"
)

func runContentService(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		_, err := fmt.Fprintln(stdout, "Usage: cymy content service test --url URL [--token-env NAME]")
		return err
	}
	if args[0] != "test" {
		return fmt.Errorf("unknown cymy content service command %q", args[0])
	}
	flags := flag.NewFlagSet("cymy content service test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	baseURL := flags.String("url", "", "content service URL")
	tokenEnvironment := flags.String("token-env", "", "environment variable containing the content service token")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*baseURL) == "" {
		return errors.New("cymy content service test requires --url")
	}
	parsed, err := url.Parse(*baseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("--url must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	token := ""
	if name := strings.TrimSpace(*tokenEnvironment); name != "" {
		token = strings.TrimSpace(os.Getenv(name))
		if token == "" {
			return fmt.Errorf("content token environment variable %q is not set", name)
		}
	}
	client := public.ServiceClient{BaseURL: strings.TrimRight(*baseURL, "/"), BearerToken: token}
	health, err := client.Health(context.Background())
	if err != nil {
		return err
	}
	capabilities, err := client.Capabilities(context.Background())
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"url": *baseURL, "health": health, "capabilities": capabilities})
}
