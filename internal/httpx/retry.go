// Package httpx provides cross-cutting HTTP helpers for outbound API clients.
//
// RetryTransport wraps an http.RoundTripper to absorb transient network and
// upstream failures (timeouts, connection resets, 429/5xx) via bounded
// exponential backoff with equal jitter, honouring Retry-After response
// headers when present. It is intentionally a transport-layer wrapper rather
// than a per-call helper so that adding it to a client retries every outbound
// request without per-call-site changes.
package httpx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultMaxAttempts = 5
	defaultBaseDelay   = 1 * time.Second
	defaultMaxDelay    = 8 * time.Second

	// maxRetryAfter caps server-supplied Retry-After hints so a pathological
	// header cannot stall the run; backoff itself is capped by MaxDelay.
	maxRetryAfter = 30 * time.Second
)

// Policy controls retry behaviour. The zero value uses package defaults
// (5 attempts, 1s base delay, 8s cap).
type Policy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func (p Policy) maxAttempts() int {
	if p.MaxAttempts <= 0 {
		return defaultMaxAttempts
	}
	return p.MaxAttempts
}

func (p Policy) baseDelay() time.Duration {
	if p.BaseDelay <= 0 {
		return defaultBaseDelay
	}
	return p.BaseDelay
}

func (p Policy) maxDelay() time.Duration {
	if p.MaxDelay <= 0 {
		return defaultMaxDelay
	}
	return p.MaxDelay
}

// RetryTransport wraps Base with retry semantics. A nil Base falls back to
// http.DefaultTransport. RetryTransport implements http.RoundTripper.
type RetryTransport struct {
	Base   http.RoundTripper
	Policy Policy
}

// RoundTrip executes req with retries on transient failures. Per the
// http.RoundTripper contract it must not modify req; on retry it sources a
// fresh request body from req.GetBody when req.Body is non-nil. If req.Body is
// set without GetBody, the request is sent once and not retried (re-sending a
// partially-consumed body is unsafe).
func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	canRetryBody := req.Body == nil || req.GetBody != nil
	maxAttempts := t.Policy.maxAttempts()
	if !canRetryBody && req.Body != nil {
		slog.Debug("httpx: request has body without GetBody; retries disabled",
			"method", req.Method, "url", req.URL.String())
		maxAttempts = 1
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}

		resp, err := base.RoundTrip(req)

		if err == nil && !shouldRetryStatus(resp.StatusCode) {
			return resp, nil
		}

		if err != nil && !shouldRetryErr(err) {
			return nil, err
		}

		lastResp, lastErr = resp, err

		if attempt == maxAttempts-1 {
			break
		}

		delay := nextDelay(attempt, t.Policy, resp, time.Now())
		if err != nil {
			slog.Debug("httpx: retrying after error",
				"method", req.Method, "url", req.URL.String(),
				"attempt", attempt+1, "delay", delay, "error", err.Error())
		} else {
			slog.Debug("httpx: retrying after status",
				"method", req.Method, "url", req.URL.String(),
				"attempt", attempt+1, "delay", delay, "status", resp.StatusCode)
			drainAndClose(resp.Body)
		}

		if waitErr := sleepCtx(req.Context(), delay); waitErr != nil {
			return nil, waitErr
		}
	}

	return lastResp, lastErr
}

// shouldRetryStatus returns true for upstream-side transient HTTP statuses.
func shouldRetryStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, // 408
		http.StatusTooEarly,            // 425
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	}
	return false
}

// shouldRetryErr returns true for transient transport-level errors.
// context.Canceled is treated as terminal (the caller asked to stop).
func shouldRetryErr(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return false
}

// nextDelay computes the sleep before the next attempt: exponential backoff
// with equal jitter, raised to a Retry-After hint (clamped to maxRetryAfter)
// when the response carries one. resp may be nil (transport-level error).
func nextDelay(attempt int, p Policy, resp *http.Response, now time.Time) time.Duration {
	delay := backoffDelay(attempt, p.baseDelay(), p.maxDelay())
	if resp != nil {
		if ra, ok := parseRetryAfter(resp.Header.Get("Retry-After"), now); ok {
			if ra > maxRetryAfter {
				ra = maxRetryAfter
			}
			if ra > delay {
				delay = ra
			}
		}
	}
	return delay
}

// parseRetryAfter parses Retry-After in delta-seconds or HTTP-date form.
// Returns false for absent, unparseable, or negative values.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

// backoffDelay returns BaseDelay<<attempt capped at MaxDelay, with equal
// jitter (half fixed, half random) so consecutive retries keep a floor and
// cannot all fire within milliseconds.
func backoffDelay(attempt int, base, max time.Duration) time.Duration {
	d := base << attempt
	if d <= 0 || d > max {
		d = max
	}
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
