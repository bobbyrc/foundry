package notify

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bobbyrc/onlet/internal/sysproc"
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
// (a push CLI, a chat webhook script) without onlet needing a built-in driver for it.
type execNotifier struct {
	command string
}

// Notify runs the command with `sh -c` and the notification in ONLET_NOTIFY_TITLE,
// ONLET_NOTIFY_BODY, ONLET_NOTIFY_PRIORITY and ONLET_NOTIFY_URL. A non-zero exit is an
// error that includes the command's combined output.
func (e *execNotifier) Notify(ctx context.Context, n Notification) error {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", e.command)
	// On timeout the whole process group is killed, and Wait does not block on pipes a background
	// child still holds open. Without this a single backgrounded child blocked every notification.
	// A command that exits 0 but leaves such a child behind still succeeds (see sysproc.Wait).
	sysproc.Configure(cmd)
	cmd.Env = append(os.Environ(),
		"ONLET_NOTIFY_TITLE="+n.Title,
		"ONLET_NOTIFY_BODY="+n.Body,
		"ONLET_NOTIFY_PRIORITY="+string(effectivePriority(n.Priority)),
		"ONLET_NOTIFY_URL="+n.URL,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Start()
	if err == nil {
		err = sysproc.Wait(cmd)
	}
	if err != nil {
		return fmt.Errorf("notify exec: %w: %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}
