package boardplugin

import (
	"context"
	"errors"
	"testing"
)

func TestContentReadStopsWhenStreamContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	content := &content{ctx: ctx, session: &session{}}
	cancel()
	if _, err := content.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read after cancellation = %v, want context.Canceled", err)
	}
}
