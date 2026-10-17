package providerexec

import (
	"contentkit"
	"errors"
	"testing"
	"time"
)

func TestEncodeFailurePreservesTypedBlockadeError(t *testing.T) {
	err := contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorRateLimit, errors.New("capacity"))
	err.RetryAfter = 1250 * time.Millisecond
	failure := encodeFailure(err)
	if failure.Code != "rate_limit" || failure.RetryAfterMilliseconds != 1250 {
		t.Fatalf("failure = %#v", failure)
	}
}
