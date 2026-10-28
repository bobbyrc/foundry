package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactedHidesSecretsWithoutMutatingOriginal(t *testing.T) {
	cfg, err := Parse([]byte(`
harnesses:
  exec:
    type: exec
    command: ["agent"]
    env:
      API_TOKEN: abc123
projects:
  app:
    repo: bob/app
    env:
      SECRET_PATH: /run/secret
    deploy:
      type: command
      options:
        deploy: "vercel deploy --prod"
        vercel_token: tok-1
    observe:
      - type: sentry
        options:
          org: acme
          auth_token: tok-2
          Password: pw
notify:
  - type: ntfy
    options:
      topic: public-topic
      api_key: key-1
`), "/conf/foundry.yaml")
	require.NoError(t, err)

	red := cfg.Redacted()

	assert.Equal(t, "/conf/foundry.yaml", red.Path)
	assert.Equal(t, "app", red.Projects["app"].Name)
	assert.Equal(t, "***", red.Harness["exec"].Env["API_TOKEN"])
	assert.Equal(t, "***", red.Projects["app"].Env["SECRET_PATH"])
	assert.Equal(t, "***", red.Projects["app"].Deploy.Options["vercel_token"])
	assert.Equal(t, "vercel deploy --prod", red.Projects["app"].Deploy.Options["deploy"])
	assert.Equal(t, "***", red.Projects["app"].Observe[0].Options["auth_token"])
	assert.Equal(t, "***", red.Projects["app"].Observe[0].Options["Password"])
	assert.Equal(t, "acme", red.Projects["app"].Observe[0].Options["org"])
	assert.Equal(t, "***", red.Notify[0].Options["api_key"])
	assert.Equal(t, "public-topic", red.Notify[0].Options["topic"])

	// The original is untouched.
	assert.Equal(t, "abc123", cfg.Harness["exec"].Env["API_TOKEN"])
	assert.Equal(t, "/run/secret", cfg.Projects["app"].Env["SECRET_PATH"])
	assert.Equal(t, "tok-1", cfg.Projects["app"].Deploy.Options["vercel_token"])
	assert.Equal(t, "tok-2", cfg.Projects["app"].Observe[0].Options["auth_token"])
	assert.Equal(t, "key-1", cfg.Notify[0].Options["api_key"])

	// The redacted copy still marshals, and its Duration values survive.
	data, err := json.Marshal(red)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"verify_for":"10m0s"`)
}
