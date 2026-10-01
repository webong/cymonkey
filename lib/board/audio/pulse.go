// Package audio describes caller-selected audio devices and normalizes runtime observations.
package audio

import (
	"regexp"
	"strconv"
	"strings"
)

var pulseSampleSpecPattern = regexp.MustCompile(`^(\S+)\s+(\d+)ch\s+(\d+)Hz$`)

// ParsePulseDevices reads the output of pactl list short sinks/sources.
// Ready means the requested devices were listed; it does not prove media is flowing.
func ParsePulseDevices(sinks, sources string, request Request) Observation {
	observation := Observation{Backend: "pulseaudio", Devices: []Device{}}
	outputID := defaultString(request.OutputDevice, "board_output")
	inputID := defaultString(request.InputDevice, "board_microphone")
	parse := func(output, defaultKind string) {
		for _, line := range strings.Split(output, "\n") {
			fields := strings.Split(strings.TrimSpace(line), "\t")
			if len(fields) < 5 {
				continue
			}
			kind := defaultKind
			if defaultKind == "input" && fields[1] == outputID+".monitor" {
				kind = "system-capture"
			} else if defaultKind == "input" && fields[1] == inputID {
				kind = "microphone"
			}
			device := Device{ID: fields[1], Kind: kind, State: strings.ToLower(fields[4])}
			if sample := pulseSampleSpecPattern.FindStringSubmatch(fields[3]); len(sample) == 4 {
				device.SampleFormat = sample[1]
				device.Channels, _ = strconv.Atoi(sample[2])
				device.SampleRate, _ = strconv.Atoi(sample[3])
			}
			observation.Devices = append(observation.Devices, device)
		}
	}
	parse(sinks, "output")
	parse(sources, "input")
	for _, device := range observation.Devices {
		if device.Kind == "output" && device.ID == outputID {
			observation.Playback = request.Playback
		}
		if device.Kind == "microphone" && device.ID == inputID {
			observation.Microphone = request.Microphone
		}
		if device.Kind == "system-capture" && device.ID == outputID+".monitor" {
			observation.SystemCapture = request.SystemCapture
		}
	}
	missing := []string{}
	if request.Playback && !observation.Playback {
		missing = append(missing, "playback")
	}
	if request.Microphone && !observation.Microphone {
		missing = append(missing, "microphone")
	}
	if request.SystemCapture && !observation.SystemCapture {
		missing = append(missing, "system-output capture")
	}
	observation.Ready = len(missing) == 0
	if !observation.Ready {
		observation.Reason = "requested audio devices are unavailable: " + strings.Join(missing, ", ")
	}
	return observation
}

func defaultString(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
