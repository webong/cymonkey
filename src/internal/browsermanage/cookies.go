package browsermanage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/webong/ext/res/web/contract"
)

// Cookie runs a cookie operation ("query" or "normalize") on a trusted browser
// adapter that declares it, through ext's browser share protocol. The adapter
// owns every browser-specific detail: profile discovery, store formats and
// decrypting values. The result holds live cookie values, so a caller must not
// log, store, or forward it. Errors never carry the request or the cookies.
func (h *Host) Cookie(ctx context.Context, endpoint Endpoint, operation string, request contract.CookieRequest) (contract.CookieQueryResult, error) {
	if operation != "query" && operation != "normalize" {
		return contract.CookieQueryResult{}, fmt.Errorf("unsupported cookie operation %q", operation)
	}
	if endpoint.Adapter == nil || !endpoint.Adapter.HasBrowserShare("cookie."+operation) {
		return contract.CookieQueryResult{}, fmt.Errorf("browser adapter does not support cookie.%s", operation)
	}
	request.Version = contract.Version
	encoded, err := json.Marshal(request)
	if err != nil {
		return contract.CookieQueryResult{}, err
	}
	if len(encoded) > 8<<20 {
		return contract.CookieQueryResult{}, errors.New("cookie request exceeds 8 MiB")
	}
	output, err := h.run(ctx, endpoint, []string{"cookie", operation}, encoded, contract.MaxCookieQueryResultBytes+4096, true, nil)
	if err != nil {
		return contract.CookieQueryResult{}, err
	}
	var result contract.CookieQueryResult
	if err := json.Unmarshal(output, &result); err != nil {
		return contract.CookieQueryResult{}, errors.New("browser adapter returned an invalid cookie result")
	}
	for _, cookie := range result.Cookies {
		if err := contract.ValidateCookie(cookie); err != nil {
			return contract.CookieQueryResult{}, errors.New("browser adapter returned an invalid cookie")
		}
	}
	return result, nil
}
