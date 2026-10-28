package engine

import (
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/bobbyrc/foundry/internal/config"
)

// secretEnvWords mark an environment variable as holding a secret when one of them is a whole
// segment of its name, split on underscores: GH_TOKEN, SENTRY_AUTH_TOKEN and DATABASE_PASSWORD
// match, GIT_AUTHOR_NAME does not. Matching substrings instead would also catch AUTHOR and MONKEY.
// SESSION is not a word: session IDs (CLAUDE_CODE_SESSION_ID) are not secrets, and redacting them
// mangled the paths that contain them; a session secret is named SESSION_SECRET or SESSION_KEY.
var secretEnvWords = []string{"TOKEN", "TOKENS", "SECRET", "SECRETS", "PASSWORD", "PASSWD", "PASS", "KEY", "KEYS", "APIKEY", "CREDENTIAL", "CREDENTIALS", "AUTH", "DSN", "COOKIE", "PAT"}

// minSecretLen is the shortest secret value redactSecrets replaces.
const minSecretLen = 8

// redactSecrets replaces every occurrence in s of a secret value in env with "***". Deploy and
// logs output becomes evidence that agents read and quote in public PRs, so a script that prints
// its credentials must not leave them in the evidence. env holds "NAME=value" pairs. A value is
// secret when its name has a secret word as a whole segment (see secretEnvWords), or when it is a
// URL with credentials (scheme://user:pass@host), whatever the name. Values shorter than
// minSecretLen are skipped, because replacing them would mangle ordinary text. Longer values are
// replaced first, so a value that contains another is replaced whole.
func redactSecrets(s string, env []string) string {
	var secrets []string
	for _, pair := range env {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || len(value) < minSecretLen {
			continue
		}
		if !isSecretEnvName(name) && !hasURLCredentials(value) {
			continue
		}
		secrets = append(secrets, value)
		if hasURLCredentials(value) {
			secrets = append(secrets, urlPasswordSecrets(value)...)
		}
	}
	sort.SliceStable(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, v := range secrets {
		s = strings.ReplaceAll(s, v, "***")
	}
	return s
}

// isSecretEnvName reports whether one of secretEnvWords is a whole underscore-separated segment
// of the upper-cased environment variable name.
func isSecretEnvName(name string) bool {
	for _, seg := range strings.Split(strings.ToUpper(name), "_") {
		for _, w := range secretEnvWords {
			if seg == w {
				return true
			}
		}
	}
	return false
}

// hasURLCredentials reports whether value is a URL with a user or password before its host, such
// as postgres://user:pass@db.local/app.
func hasURLCredentials(value string) bool {
	_, rest, ok := strings.Cut(value, "://")
	return ok && strings.Contains(rest, "@")
}

// minURLPasswordLen is the shortest URL password redactSecrets replaces on its own. A password is
// often printed alone (a connect error names only the password), and the whole URL does not match
// that text.
const minURLPasswordLen = 6

// urlPasswordSecrets returns the password of the URL value, and its query-escaped form when that
// differs, if the password is at least minURLPasswordLen bytes. It returns nil when value does not
// parse or has no password.
func urlPasswordSecrets(value string) []string {
	u, err := url.Parse(value)
	if err != nil || u.User == nil {
		return nil
	}
	pw, ok := u.User.Password()
	if !ok || len(pw) < minURLPasswordLen {
		return nil
	}
	out := []string{pw}
	if q := url.QueryEscape(pw); q != pw {
		out = append(out, q)
	}
	return out
}

// evidenceEnv returns the environment that the deploy and logs commands of p run with: foundry's
// environment, then the project's env. It is the input to redactSecrets for their output.
func evidenceEnv(p *config.Project) []string {
	return append(os.Environ(), projectEnvPairs(p.Env)...)
}
