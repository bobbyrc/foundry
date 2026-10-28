package deploy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CheckHealth performs a GET on url and returns nil only for a 2xx response. A zero timeout means
// 10 seconds.
func CheckHealth(ctx context.Context, url string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("health %s: %w", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("health %s: %w", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("health %s: status %d", url, resp.StatusCode)
	}
	return nil
}
