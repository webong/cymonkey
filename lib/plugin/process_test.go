package providerplugin

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDecodeRemoteErrorSupportsStructuredAndLegacyErrors(t *testing.T) {
	structured := json.RawMessage(`{"code":"rate_limit","message":"retry later","retryAfterMilliseconds":250}`)
	err := decodeRemoteError(structured)
	var remote *RemoteError
	if !errors.As(err, &remote) {
		t.Fatalf("error = %T %v", err, err)
	}
	if remote.Code != "rate_limit" || remote.Message != "retry later" || remote.RetryAfterMilliseconds != 250 {
		t.Fatalf("remote error = %#v", remote)
	}

	legacy := decodeRemoteError(json.RawMessage(`"old provider failure"`))
	if legacy.Error() != "plugin: old provider failure" {
		t.Fatalf("legacy error = %q", legacy)
	}
}

func TestDecodeRemoteErrorRejectsInvalidPayloads(t *testing.T) {
	for _, raw := range []string{`{}`, `{"message":"failure","retryAfterMilliseconds":-1}`, `{"message":"failure","retryAfterMilliseconds":31536000001}`, `false`} {
		if err := decodeRemoteError(json.RawMessage(raw)); err == nil {
			t.Fatalf("decodeRemoteError(%s) succeeded", raw)
		}
	}
}
