package notify

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bobbyrc/foundry/internal/sysproc"
)

// execTimeout bounds one notification command. It is a variable so tests can shorten it.
var execTimeout = 30 * time.Second

func init() {
	Register("exec", func(cfg Config) (Notifier, error) {
		command := optString(cfg.Options, "command")
		if command == "" {
			return nil, fmt.Errorf("notify exec: option %q is required", "command")
		}
		return &execNotifier{command: command}, nil
	})
}

// execNotifier runs a shell command for each notification, so users can wire any channel
// (a push CLI, a chat webhook script) without foundry needing a built-in driver for it.
type execNotifier struct {
	command string
}

// Notify runs the command with `sh -c` and the notification in FOUNDRY_NOTIFY_TITLE,
// FOUNDRY_NOTIFY_BODY, FOUNDRY_NOTIFY_PRIORITY and FOUNDRY_NOTIFY_URL. A non-zero exit is an
// error that includes the command's combined output.
func (e *execNotifier) Notify(ctx context.Context, n Notification) error {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", e.command)
	// On timeout the whole process group is killed, and Wait does not block on pipes a background
	// child still holds open. Without this a single backgrounded child blocked every notification.
	sysproc.Configure(cmd)
	cmd.Env = append(os.Environ(),
		"FOUNDRY_NOTIFY_TITLE="+n.Title,
		"FOUNDRY_NOTIFY_BODY="+n.Body,
		"FOUNDRY_NOTIFY_PRIORITY="+string(effectivePriority(n.Priority)),
		"FOUNDRY_NOTIFY_URL="+n.URL,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("notify exec: %w: %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}
