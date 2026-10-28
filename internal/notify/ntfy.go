package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	ntfyDefaultServer = "https://ntfy.sh"
	ntfyTimeout       = 10 * time.Second
	// ntfyErrBodyLimit caps how much of a failing response body goes into the error.
	ntfyErrBodyLimit = 200
)

func init() {
	Register("ntfy", func(cfg Config) (Notifier, error) {
		n, err := newNtfy(cfg.Options, nil)
		if err != nil {
			return nil, err
		}
		return n, nil
	})
}

// ntfy publishes notifications to an ntfy topic (https://ntfy.sh or a self-hosted server).
type ntfy struct {
	server string
	topic  string
	token  string // optional access token, sent as a Bearer token
	client *http.Client
}

// newNtfy builds an ntfy notifier from its YAML options. A nil client uses a default client with
// a 10 second timeout. Options: "server" (default https://ntfy.sh), "topic" (required) and
// "token_env" (the name of an environment variable holding an access token).
func newNtfy(opts map[string]any, client *http.Client) (*ntfy, error) {
	topic := optString(opts, "topic")
	if topic == "" {
		return nil, fmt.Errorf("notify ntfy: option %q is required", "topic")
	}
	server := optString(opts, "server")
	if server == "" {
		server = ntfyDefaultServer
	}

	var token string
	if envName := optString(opts, "token_env"); envName != "" {
		token = os.Getenv(envName)
		if token == "" {
			return nil, fmt.Errorf("notify ntfy: environment variable %q (token_env) is empty", envName)
		}
	}

	if client == nil {
		client = &http.Client{Timeout: ntfyTimeout}
	}
	return &ntfy{
		server: strings.TrimRight(server, "/"),
		topic:  topic,
		token:  token,
		client: client,
	}, nil
}

// Notify POSTs the notification to the topic. The body is n.Body, or n.Title when the body is
// empty. Title, priority, tags and click URL travel as ntfy headers.
func (t *ntfy) Notify(ctx context.Context, n Notification) error {
	body := n.Body
	if body == "" {
		body = n.Title
	}
	endpoint := t.server + "/" + url.PathEscape(t.topic)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify ntfy: build request: %w", err)
	}
	req.Header.Set("Title", headerSafe(n.Title))
	req.Header.Set("Priority", ntfyPriority(n.Priority))
	if len(n.Tags) > 0 {
		req.Header.Set("Tags", strings.Join(n.Tags, ","))
	}
	if n.URL != "" {
		req.Header.Set("Click", headerSafe(n.URL))
	}
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("notify ntfy: post to %s: %w", t.topic, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, ntfyErrBodyLimit))
		return fmt.Errorf("notify ntfy: %s returned %s: %s", t.topic, resp.Status, strings.TrimSpace(string(snippet)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// ntfyPriority maps a foundry priority to ntfy's 1 (min) to 5 (max) scale.
func ntfyPriority(p Priority) string {
	switch effectivePriority(p) {
	case PriorityLow:
		return "2"
	case PriorityHigh:
		return "5"
	default:
		return "3"
	}
}

// headerSafe replaces line breaks, which Go refuses to send in a header value.
func headerSafe(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

// optString returns an option as a trimmed string. YAML values arrive as any, so the value is
// formatted with fmt.Sprint. A missing or nil option is the empty string.
func optString(opts map[string]any, key string) string {
	v, ok := opts[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
