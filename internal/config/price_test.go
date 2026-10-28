package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRejectsNonFinitePrices(t *testing.T) {
	for _, v := range []string{".inf", "-.inf", ".nan"} {
		_, err := Parse([]byte("harnesses:\n  codex:\n    type: codex\n    price: {input_per_mtok: "+v+"}\n"), "")
		require.Error(t, err, v)
		require.Contains(t, err.Error(), "prices must be finite", v)
	}
}
