package client

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/bobbyrc/foundry/internal/domain"
)

// Events opens GET /events and calls fn for each event, in stream order, until ctx is done or
// the server closes the stream; both return nil. Other failures (connection, non-2xx status,
// read errors) return an error. Events are read from the Server-Sent Events format: "id:",
// "event:" and "data:" lines, a blank line ending each event, and ":" comment lines ignored.
// Events whose data is not a JSON domain.Event are skipped. The stream is not reconnected;
// callers resume by calling again with the last ID they saw.
func (c *Client) Events(ctx context.Context, after int64, fn func(domain.Event)) error {
	q := url.Values{}
	if after > 0 {
		q.Set("after", strconv.FormatInt(after, 10))
	}
	u := c.BaseURL + apiPrefix + "/events"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	c.authorize(req)

	// The stream is open-ended, so the request timeout of the normal client must not apply.
	hc := *c.httpClient()
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return decodeError(resp)
	}
	return readEventStream(ctx, resp.Body, fn)
}

// readEventStream parses an SSE body and calls fn for each complete event.
func readEventStream(ctx context.Context, r io.Reader, fn func(domain.Event)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)

	var (
		id        int64
		eventName string
		data      []string
	)
	dispatch := func() {
		defer func() { id, eventName, data = 0, "", nil }()
		if len(data) == 0 {
			return
		}
		var ev domain.Event
		if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &ev); err != nil {
			return
		}
		if ev.ID == 0 {
			ev.ID = id
		}
		if ev.Type == "" {
			ev.Type = eventName
		}
		fn(ev)
	}

	for sc.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := sc.Text()
		if line == "" {
			dispatch()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value := line, ""
		if i := strings.IndexByte(line, ':'); i >= 0 {
			field, value = line[:i], strings.TrimPrefix(line[i+1:], " ")
		}
		switch field {
		case "id":
			if n, err := strconv.ParseInt(value, 10, 64); err == nil {
				id = n
			}
		case "event":
			eventName = value
		case "data":
			data = append(data, value)
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}
