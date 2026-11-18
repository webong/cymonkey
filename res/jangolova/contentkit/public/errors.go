package public

import (
	"errors"
	"strings"
)

// PublicError rewrites library names in an error message to the public ones.
func PublicError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.NewReplacer(
		"Blockade provider adapter", "content adapter",
		"Blockade provider", "content adapter",
		"Blockade config", "content config",
		"blockade", "content",
	).Replace(err.Error())
	return errors.New(message)
}
