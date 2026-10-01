package main

import (
	"errors"
	"fmt"
	"strings"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	executionProviderCPU      = "cpu"
	executionProviderCUDA     = "cuda"
	executionProviderTensorRT = "tensorrt"
	executionProviderOpenVINO = "openvino"
	executionProviderCoreML   = "coreml"
)

var supportedExecutionProviders = []string{
	executionProviderCPU,
	executionProviderCUDA,
	executionProviderTensorRT,
	executionProviderOpenVINO,
	executionProviderCoreML,
}

func validateExecutionProviders(providers []executionProviderConfig) error {
	seen := make(map[string]bool, len(providers))
	for index, provider := range providers {
		name, err := canonicalExecutionProviderName(provider.Name)
		if err != nil {
			return fmt.Errorf("executionProviders[%d]: %w", index, err)
		}
		if seen[name] {
			return fmt.Errorf("execution provider %q is repeated", name)
		}
		seen[name] = true
		if name == executionProviderCPU {
			if len(provider.Options) != 0 {
				return errors.New("CPU execution provider does not accept options")
			}
			if index != len(providers)-1 {
				return errors.New("CPU execution provider must be last")
			}
		}
	}
	return nil
}

func canonicalExecutionProviderName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, supported := range supportedExecutionProviders {
		if name == supported {
			return name, nil
		}
	}
	return "", fmt.Errorf("unsupported execution provider %q (supported: %s)", name, strings.Join(supportedExecutionProviders, ", "))
}

func configureOnnxExecutionProviders(id string, providers []executionProviderConfig) (*ort.SessionOptions, []string, error) {
	if err := validateExecutionProviders(providers); err != nil {
		return nil, nil, fmt.Errorf("ONNX provider %q: %w", id, err)
	}
	if len(providers) == 0 || (len(providers) == 1 && strings.EqualFold(strings.TrimSpace(providers[0].Name), executionProviderCPU)) {
		return nil, []string{executionProviderCPU}, nil
	}
	options, err := ort.NewSessionOptions()
	if err != nil {
		return nil, nil, fmt.Errorf("create ONNX Runtime session options: %w", err)
	}
	effective := make([]string, 0, len(providers)+1)
	for _, provider := range providers {
		name, _ := canonicalExecutionProviderName(provider.Name)
		if name == executionProviderCPU {
			effective = append(effective, name)
			continue
		}
		if err := appendOnnxExecutionProvider(options, name, provider.Options); err != nil {
			_ = options.Destroy()
			return nil, nil, fmt.Errorf("configure ONNX provider %q execution provider %q: %w", id, name, err)
		}
		effective = append(effective, name)
	}
	if len(effective) == 0 || effective[len(effective)-1] != executionProviderCPU {
		effective = append(effective, executionProviderCPU)
	}
	return options, effective, nil
}

func appendOnnxExecutionProvider(session *ort.SessionOptions, name string, options map[string]string) error {
	switch name {
	case executionProviderCUDA:
		provider, err := ort.NewCUDAProviderOptions()
		if err != nil {
			return err
		}
		if len(options) != 0 {
			err = provider.Update(options)
		}
		if err == nil {
			err = session.AppendExecutionProviderCUDA(provider)
		}
		return errors.Join(err, provider.Destroy())
	case executionProviderTensorRT:
		provider, err := ort.NewTensorRTProviderOptions()
		if err != nil {
			return err
		}
		if len(options) != 0 {
			err = provider.Update(options)
		}
		if err == nil {
			err = session.AppendExecutionProviderTensorRT(provider)
		}
		return errors.Join(err, provider.Destroy())
	case executionProviderOpenVINO:
		return session.AppendExecutionProviderOpenVINO(options)
	case executionProviderCoreML:
		return session.AppendExecutionProviderCoreMLV2(options)
	default:
		return fmt.Errorf("execution provider %q cannot be appended", name)
	}
}
