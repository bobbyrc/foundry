package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestDurationYAMLRoundTrip(t *testing.T) {
	type wrap struct {
		D Duration `yaml:"d"`
	}
	out, err := yaml.Marshal(wrap{D: Duration{15 * time.Minute}})
	require.NoError(t, err)
	assert.Equal(t, "d: 15m0s\n", string(out))

	var back wrap
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, 15*time.Minute, back.D.Duration)
}

func TestDurationJSONRoundTrip(t *testing.T) {
	data, err := json.Marshal(Duration{2 * time.Hour})
	require.NoError(t, err)
	assert.Equal(t, `"2h0m0s"`, string(data))

	var d Duration
	require.NoError(t, json.Unmarshal([]byte(`"30s"`), &d))
	assert.Equal(t, 30*time.Second, d.Duration)

	assert.Error(t, json.Unmarshal([]byte(`30`), &d), "numbers are not durations")
}

func TestDurationRejectsBadValues(t *testing.T) {
	var d Duration
	err := json.Unmarshal([]byte(`"ten minutes"`), &d)
	assert.ErrorContains(t, err, `"ten minutes"`)

	var y struct {
		D Duration `yaml:"d"`
	}
	err = yaml.Unmarshal([]byte("d: 10\n"), &y)
	assert.ErrorContains(t, err, `"10"`)
}
