package cymonkeyhost

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestSupervisorStopsComponentsOnCancellation(t *testing.T) {
	if os.Getenv("CYMONKEY_HOST_CHILD") == "1" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		<-ctx.Done()
		return
	}
	config := Config{
		APIVersion: ConfigAPIVersion,
		Kind:       "CymonkeyHost",
		Components: []ComponentConfig{{
			ID:      "fixture",
			Command: []string{os.Args[0], "-test.run=TestSupervisorStopsComponentsOnCancellation"},
			Environment: map[string]string{
				"CYMONKEY_HOST_CHILD": "1",
			},
		}},
	}
	supervisor, err := NewSupervisor(config, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- supervisor.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not stop after cancellation")
	}
}
