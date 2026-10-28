package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bobbyrc/onlet/internal/domain"
)

const (
	sentryDefaultBaseURL  = "https://sentry.io"
	sentryDefaultTokenEnv = "SENTRY_AUTH_TOKEN"
	sentryDefaultQuery    = "is:unresolved"
	sentryTimeout         = 30 * time.Second
	sentryErrBodyLimit    = 512
)

func init() {
	Register("sentry", newSentryObserver)
}

// sentryObserver reads unresolved issues of one Sentry project through its REST API.
type sentryObserver struct {
	name    string
	baseURL string
	org     string
	project string
	query   string
	token   string
	client  *http.Client
}

// newSentryObserver builds a "sentry" observer. Options: "org" and "project" (required),
// "base_url" (default https://sentry.io), "token_env" (default SENTRY_AUTH_TOKEN) and "query"
// (default is:unresolved). The token is read from cfg.Env[token_env], then the process environment.
func newSentryObserver(cfg Config) (Observer, error) {
	return newSentry(cfg, &http.Client{Timeout: sentryTimeout})
}

// newSentry is newSentryObserver with an injected HTTP client, for tests.
func newSentry(cfg Config, client *http.Client) (Observer, error) {
	org := optString(cfg.Options, "org")
	project := optString(cfg.Options, "project")
	if org == "" || project == "" {
		return nil, fmt.Errorf("observer %q: options \"org\" and \"project\" are required", cfg.Name)
	}
	baseURL := optString(cfg.Options, "base_url")
	if baseURL == "" {
		baseURL = sentryDefaultBaseURL
	}
	tokenEnv := optString(cfg.Options, "token_env")
	if tokenEnv == "" {
		tokenEnv = sentryDefaultTokenEnv
	}
	token := cfg.Env[tokenEnv]
	if token == "" {
		token = os.Getenv(tokenEnv)
	}
	if token == "" {
		return nil, fmt.Errorf("observer %q: sentry token: environment variable %s is not set", cfg.Name, tokenEnv)
	}
	query := optString(cfg.Options, "query")
	if query == "" {
		query = sentryDefaultQuery
	}
	return &sentryObserver{
		name:    cfg.Name,
		baseURL: strings.TrimRight(baseURL, "/"),
		org:     org,
		project: project,
		query:   query,
		token:   token,
		client:  client,
	}, nil
}

type sentryIssue struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Culprit   string    `json:"culprit"`
	Level     string    `json:"level"`
	Count     string    `json:"count"`
	Permalink string    `json:"permalink"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

// Poll lists the project's issues active in the last 24 hours and returns those last seen at or
// after since.
func (o *sentryObserver) Poll(ctx context.Context, since time.Time) ([]domain.SignalObservation, error) {
	endpoint := fmt.Sprintf("%s/api/0/projects/%s/%s/issues/?query=%s&statsPeriod=24h&limit=100",
		o.baseURL, url.PathEscape(o.org), url.PathEscape(o.project), url.QueryEscape(o.query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("observer %q: %w", o.name, err)
	}
	req.Header.Set("Authorization", "Bearer "+o.token)
	req.Header.Set("Accept", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("observer %q: sentry request: %w", o.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, sentryErrBodyLimit))
		return nil, fmt.Errorf("observer %q: sentry status %d: %s", o.name, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var issues []sentryIssue
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		return nil, fmt.Errorf("observer %q: decode sentry issues: %w", o.name, err)
	}

	var out []domain.SignalObservation
	for _, is := range issues {
		if is.LastSeen.Before(since) {
			continue
		}
		count, err := strconv.ParseInt(is.Count, 10, 64)
		if err != nil {
			count = 0
		}
		out = append(out, domain.SignalObservation{
			Fingerprint: "sentry-" + is.ID,
			Title:       is.Title,
			Level:       is.Level,
			Count:       count,
			URL:         is.Permalink,
			Culprit:     is.Culprit,
			FirstSeen:   is.FirstSeen,
			LastSeen:    is.LastSeen,
		})
	}
	return out, nil
}
