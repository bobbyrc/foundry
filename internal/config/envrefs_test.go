package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvRefsListsEachReferencedVariableOnce(t *testing.T) {
	data := []byte("data_dir: ${DOGFOOD}/data\ndeploy: ${DOGFOOD}/release.sh \"$ONETT_REF\"\ntoken: ${API_KEY}\n")
	assert.Equal(t, []string{"API_KEY", "DOGFOOD"}, EnvRefs(data), "sorted, deduplicated, bare $NAME ignored")
	assert.Empty(t, EnvRefs([]byte("server:\n  addr: 127.0.0.1:7777\n")))
}
