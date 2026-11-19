package audio

import (
	"fmt"
	"strings"
	"testing"
)

func TestPulseDevicesMatchSelectedNames(t *testing.T) {
	for _, names := range [][2]string{{"board_output", "board_microphone"}, {"session-speaker", "session-mic"}} {
		t.Run(names[0], func(t *testing.T) {
			request := Request{Playback: true, Microphone: true, SystemCapture: true, OutputDevice: names[0], InputDevice: names[1]}
			sinks := fmt.Sprintf("0\t%s\tmodule-null-sink.c\ts16le 2ch 48000Hz\tIDLE\n", names[0])
			sources := fmt.Sprintf("1\t%s.monitor\tmodule-null-sink.c\ts16le 2ch 48000Hz\tIDLE\n2\t%s\tmodule-null-source.c\ts16le 2ch 48000Hz\tIDLE\n", names[0], names[1])
			got := ParsePulseDevices(sinks, sources, request)
			if !got.Ready || !got.Playback || !got.Microphone || !got.SystemCapture || len(got.Devices) != 3 {
				t.Fatalf("audio observation = %+v", got)
			}
			if got.Devices[0].SampleRate != 48000 || got.Devices[0].Channels != 2 {
				t.Fatalf("sample format = %+v", got.Devices[0])
			}
			missing := ParsePulseDevices("", "", request)
			if missing.Ready || !strings.Contains(missing.Reason, "playback") {
				t.Fatalf("missing = %+v", missing)
			}
			request.OutputDevice = "different"
			if ParsePulseDevices(sinks, sources, request).Ready {
				t.Fatal("unselected devices reported ready")
			}
		})
	}
}
