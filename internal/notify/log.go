package notify

import (
	"context"
	"log/slog"
)

func init() {
	Register("log", func(cfg Config) (Notifier, error) {
		return logNotifier{}, nil
	})
}

// logNotifier writes each notification to the default slog logger. It is useful when no
// external channel is configured, and as a record that a notification was raised.
type logNotifier struct{}

// Notify logs the notification at info level.
func (logNotifier) Notify(_ context.Context, n Notification) error {
	slog.Default().Info("notification",
		"title", n.Title,
		"priority", string(effectivePriority(n.Priority)),
		"body", n.Body,
		"url", n.URL,
	)
	return nil
}
