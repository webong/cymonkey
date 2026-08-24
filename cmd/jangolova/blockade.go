package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"jangolova/internal/blockade"
)

func blockadeCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("blockade requires validate or observe")
	}
	switch args[0] {
	case "validate":
		return blockadeValidateCommand(args[1:])
	case "observe":
		return blockadeObserveCommand(args[1:])
	default:
		return fmt.Errorf("unknown blockade command %q", args[0])
	}
}

func blockadeValidateCommand(args []string) error {
	flags := flag.NewFlagSet("blockade validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Blockade YAML config")
	checkFiles := flags.Bool("check-files", false, "verify local model files exist")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("blockade validate accepts flags only")
	}
	if strings.TrimSpace(*configPath) == "" {
		return errors.New("--config is required")
	}
	config, err := blockade.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if *checkFiles {
		if err := config.ValidateModelFiles(); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stdout, "valid Blockade config: %d engine(s)\n", len(config.Engines))
	return nil
}

func blockadeObserveCommand(args []string) error {
	flags := flag.NewFlagSet("blockade observe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Blockade YAML config")
	engineID := flags.String("engine", "", "engine id")
	imagePath := flags.String("image", "", "PNG or JPEG image path")
	prompt := flags.String("prompt", "", "optional observation prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("blockade observe accepts flags only")
	}
	if *configPath == "" || *imagePath == "" {
		return errors.New("--config and --image are required")
	}
	config, err := blockade.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	id := *engineID
	if id == "" {
		id = config.Engines[0].ID
	}
	engine, ok := config.Engine(id)
	if !ok {
		return fmt.Errorf("Blockade engine %q is not configured", id)
	}
	if engine.Kind != "local-ultralytics" {
		return fmt.Errorf("Blockade observe currently supports local-ultralytics, got %q", engine.Kind)
	}
	pool, err := blockade.StartConfiguredLocalEngine(context.Background(), engine)
	if err != nil {
		return err
	}
	defer pool.Close()
	image, err := os.ReadFile(*imagePath)
	if err != nil {
		return err
	}
	result, err := (blockade.Client{WorkerPool: pool}).Observe(context.Background(), blockade.ObserveRequest{Image: image, Prompt: *prompt})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
