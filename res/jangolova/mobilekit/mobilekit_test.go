package mobilekit

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"testing"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
)

func jpegFrame(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, image.NewGray(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatal(err)
	}
	return append([]byte{KindVideo}, buffer.Bytes()...)
}

func TestCheckFrame(t *testing.T) {
	all := Media{Video: true, Audio: true, Microphone: true, Playback: true}
	pcm := func(kind byte, n int) []byte { return append([]byte{kind}, make([]byte, n)...) }
	for name, test := range map[string]struct {
		media Media
		frame []byte
		want  error
	}{
		"video":              {all, jpegFrame(t, 64, 48), nil},
		"video not granted":  {Media{Audio: true}, jpegFrame(t, 64, 48), ErrNotGranted},
		"video too wide":     {all, jpegFrame(t, 4097, 8), ErrFrame},
		"video too many px":  {all, jpegFrame(t, 4000, 3000), ErrFrame},
		"video not a jpeg":   {all, []byte{KindVideo, 1, 2, 3}, ErrFrame},
		"audio":              {all, pcm(KindAudio, 3840), nil},
		"audio too long":     {all, pcm(KindAudio, 3844), ErrFrame},
		"audio misaligned":   {all, pcm(KindAudio, 7), ErrFrame},
		"microphone":         {all, pcm(KindMicrophone, 8), nil},
		"microphone refused": {Media{Audio: true}, pcm(KindMicrophone, 8), ErrNotGranted},
		"playback inbound":   {all, pcm(KindPlayback, 8), ErrNotGranted},
		"unknown kind":       {all, pcm(9, 8), ErrNotGranted},
		"empty payload":      {all, []byte{KindAudio}, ErrFrame},
	} {
		if got := CheckFrame(test.media, test.frame); !errors.Is(got, test.want) {
			t.Errorf("%s: CheckFrame = %v, want %v", name, got, test.want)
		}
	}
}

func TestPolicyOrdersStartViewerAndStop(t *testing.T) {
	admit := policy()
	ask := func(operation string) error {
		return admit(context.Background(), plugin.Request{Operation: operation})
	}
	if err := ask("list"); err == nil {
		t.Fatal("list was admitted before start")
	}
	if err := ask("start"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := ask("start"); err == nil {
		t.Fatal("start was admitted twice")
	}
	if err := ask("list"); err != nil {
		t.Fatalf("list after start: %v", err)
	}
	if err := ask("stop"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := ask("list"); err == nil {
		t.Fatal("list was admitted after stop")
	}
	if err := ask("stop"); err == nil {
		t.Fatal("stop was admitted twice")
	}
}
