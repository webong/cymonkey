package providerexec

import (
	"errors"
	"testing"
	"time"

	"blockade"
)

func TestEncodeFailurePreservesTypedBlockadeError(t *testing.T) {
	err := blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorRateLimit, errors.New("capacity"))
	err.RetryAfter = 1250 * time.Millisecond
	failure := encodeFailure(err)
	if failure.Code != "rate_limit" || failure.RetryAfterMilliseconds != 1250 {
		t.Fatalf("failure = %#v", failure)
	}
}
