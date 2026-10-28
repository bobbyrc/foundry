package config

import (
	"encoding/json"
	"strings"
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
`), "/conf/onlet.yaml")
	require.NoError(t, err)

	red := cfg.Redacted()

	assert.Equal(t, "/conf/onlet.yaml", red.Path)
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

func TestRedactedMasksExpandedValuesEverywhere(t *testing.T) {
	t.Setenv("ONLET_TEST_VERCEL_TOKEN", "vcp_live_secret_1")
	t.Setenv("ONLET_TEST_CLI_KEY", "harness-key-value")
	t.Setenv("ONLET_TEST_NTFY_PATH", "abc123-topic-path")
	t.Setenv("ONLET_TEST_INSTR_SECRET", "sk-instruction-secret")
	t.Setenv("ONLET_TEST_SHORT", "tiny")
	t.Setenv("ONLET_TEST_EMPTY", "")
	cfg, err := Parse([]byte(`
harnesses:
  exec:
    type: exec
    command: ["agent"]
    args: ["--api-key=${ONLET_TEST_CLI_KEY}"]
projects:
  app:
    repo: bob/app
    instructions: "Use the key ${ONLET_TEST_INSTR_SECRET} for the API."
    deploy:
      type: command
      options:
        deploy: "vercel --token ${ONLET_TEST_VERCEL_TOKEN}"
        label: "${ONLET_TEST_SHORT}-deploy${ONLET_TEST_EMPTY}"
notify:
  - type: ntfy
    options:
      url: "https://ntfy.example/${ONLET_TEST_NTFY_PATH}"
      label: "${ONLET_TEST_SHORT}"
`), "/conf/onlet.yaml")
	require.NoError(t, err)

	red := cfg.Redacted()

	assert.Equal(t, "/conf/onlet.yaml", red.Path)
	assert.Equal(t, "app", red.Projects["app"].Name)
	assert.Equal(t, "--api-key=***", red.Harness["exec"].Args[0])
	assert.Equal(t, "Use the key *** for the API.", red.Projects["app"].Instructions)
	assert.Equal(t, "vercel --token ***", red.Projects["app"].Deploy.Options["deploy"])
	assert.Equal(t, "https://ntfy.example/***", red.Notify[0].Options["url"])

	// Values shorter than six characters are not masked.
	assert.Equal(t, "tiny-deploy", red.Projects["app"].Deploy.Options["label"])
	assert.Equal(t, "tiny", red.Notify[0].Options["label"])

	data, err := json.Marshal(red)
	require.NoError(t, err)
	for _, secret := range []string{"vcp_live_secret_1", "harness-key-value", "abc123-topic-path", "sk-instruction-secret"} {
		assert.False(t, strings.Contains(string(data), secret), "redacted JSON leaks %q", secret)
	}
	assert.NotContains(t, string(data), "expanded")

	// The original is untouched.
	assert.Equal(t, "vercel --token vcp_live_secret_1", cfg.Projects["app"].Deploy.Options["deploy"])
	assert.Equal(t, []string{"--api-key=harness-key-value"}, cfg.Harness["exec"].Args)
	assert.Equal(t, "https://ntfy.example/abc123-topic-path", cfg.Notify[0].Options["url"])
}
