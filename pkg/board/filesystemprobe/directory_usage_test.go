package filesystemprobe

import "testing"

func TestDirectoryUsageBytesReportsAggregateUsage(t *testing.T) {
	used, available := DirectoryUsageBytes(t.TempDir())
	if !available || used < 0 {
		t.Fatalf("expected usage for temporary directory, used=%d available=%v", used, available)
	}
}

func TestDirectoryUsageBytesRejectsMissingDirectory(t *testing.T) {
	used, available := DirectoryUsageBytes(t.TempDir() + "/missing")
	if available || used != 0 {
		t.Fatalf("expected missing directory to be unavailable, used=%d available=%v", used, available)
	}
}
