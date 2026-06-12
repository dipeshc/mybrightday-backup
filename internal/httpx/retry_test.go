package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

// rtFunc adapts a function to http.RoundTripper.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// recordingBody tracks whether a response body was fully read and closed.
type recordingBody struct {
	io.Reader
	drained bool
	closed  bool
}

func (b *recordingBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF {
		b.drained = true
	}
	return n, err
}

func (b *recordingBody) Close() error {
	b.closed = true
	return nil
}

func newResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// fastPolicy keeps test backoff sleeps in the low milliseconds.
var fastPolicy = Policy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 4 * time.Millisecond}

func newRequest(t *testing.T, method, body string) *http.Request {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, "http://example.test/", r)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	return req
}

func TestRoundTripSuccessFirstAttempt(t *testing.T) {
	attempts := 0
	tr := &RetryTransport{
		Base: rtFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return newResponse(http.StatusOK, "ok"), nil
		}),
		Policy: fastPolicy,
	}

	resp, err := tr.RoundTrip(newRequest(t, http.MethodGet, ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestRoundTripRetriesAfterStatusAndDrainsBody(t *testing.T) {
	firstBody := &recordingBody{Reader: strings.NewReader("unavailable")}
	attempts := 0
	tr := &RetryTransport{
		Base: rtFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: firstBody}, nil
			}
			return newResponse(http.StatusOK, "ok"), nil
		}),
		Policy: fastPolicy,
	}

	resp, err := tr.RoundTrip(newRequest(t, http.MethodGet, ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
	if !firstBody.drained || !firstBody.closed {
		t.Errorf("first response body drained=%t closed=%t, want both true", firstBody.drained, firstBody.closed)
	}
}

func TestRoundTripExhaustsAttemptsReturnsLastResponse(t *testing.T) {
	attempts := 0
	tr := &RetryTransport{
		Base: rtFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return newResponse(http.StatusBadGateway, "bad"), nil
		}),
		Policy: fastPolicy,
	}

	resp, err := tr.RoundTrip(newRequest(t, http.MethodGet, ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	if attempts != fastPolicy.MaxAttempts {
		t.Errorf("attempts = %d, want %d", attempts, fastPolicy.MaxAttempts)
	}
}

func TestRoundTripExhaustsAttemptsReturnsLastError(t *testing.T) {
	attempts := 0
	tr := &RetryTransport{
		Base: rtFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return nil, fmt.Errorf("wrapped: %w", syscall.ECONNRESET)
		}),
		Policy: fastPolicy,
	}

	resp, err := tr.RoundTrip(newRequest(t, http.MethodGet, ""))
	if resp != nil {
		t.Errorf("resp = %v, want nil", resp)
	}
	if !errors.Is(err, syscall.ECONNRESET) {
		t.Errorf("err = %v, want ECONNRESET", err)
	}
	if attempts != fastPolicy.MaxAttempts {
		t.Errorf("attempts = %d, want %d", attempts, fastPolicy.MaxAttempts)
	}
}

func TestRoundTripDoesNotRetryNonRetryableStatus(t *testing.T) {
	attempts := 0
	tr := &RetryTransport{
		Base: rtFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return newResponse(http.StatusNotFound, "missing"), nil
		}),
		Policy: fastPolicy,
	}

	resp, err := tr.RoundTrip(newRequest(t, http.MethodGet, ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestRoundTripDoesNotRetryContextCanceled(t *testing.T) {
	attempts := 0
	tr := &RetryTransport{
		Base: rtFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return nil, fmt.Errorf("wrapped: %w", context.Canceled)
		}),
		Policy: fastPolicy,
	}

	_, err := tr.RoundTrip(newRequest(t, http.MethodGet, ""))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestRoundTripBodyWithoutGetBodyIsNotRetried(t *testing.T) {
	attempts := 0
	tr := &RetryTransport{
		Base: rtFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return newResponse(http.StatusServiceUnavailable, "down"), nil
		}),
		Policy: fastPolicy,
	}

	req := newRequest(t, http.MethodPost, "payload")
	// http.NewRequest sets GetBody for *strings.Reader; clear it to simulate a
	// streaming body that cannot be replayed.
	req.GetBody = nil

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestRoundTripResourcesBodyViaGetBody(t *testing.T) {
	var bodies []string
	attempts := 0
	tr := &RetryTransport{
		Base: rtFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			data, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("reading request body: %v", err)
			}
			bodies = append(bodies, string(data))
			if attempts < 2 {
				return newResponse(http.StatusTooManyRequests, "slow down"), nil
			}
			return newResponse(http.StatusOK, "ok"), nil
		}),
		Policy: fastPolicy,
	}

	resp, err := tr.RoundTrip(newRequest(t, http.MethodPost, "payload"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if len(bodies) != 2 || bodies[0] != "payload" || bodies[1] != "payload" {
		t.Errorf("bodies = %q, want [payload payload]", bodies)
	}
}

func TestRoundTripGetBodyErrorAborts(t *testing.T) {
	getBodyErr := errors.New("body gone")
	tr := &RetryTransport{
		Base: rtFunc(func(req *http.Request) (*http.Response, error) {
			return newResponse(http.StatusServiceUnavailable, "down"), nil
		}),
		Policy: fastPolicy,
	}

	req := newRequest(t, http.MethodPost, "payload")
	req.GetBody = func() (io.ReadCloser, error) { return nil, getBodyErr }

	_, err := tr.RoundTrip(req)
	if !errors.Is(err, getBodyErr) {
		t.Errorf("err = %v, want %v", err, getBodyErr)
	}
}

func TestRoundTripCancelledContextDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tr := &RetryTransport{
		Base: rtFunc(func(req *http.Request) (*http.Response, error) {
			return newResponse(http.StatusServiceUnavailable, "down"), nil
		}),
		// A long delay proves the sleep is cut short by the cancelled context.
		Policy: Policy{MaxAttempts: 2, BaseDelay: time.Hour, MaxDelay: time.Hour},
	}

	req := newRequest(t, http.MethodGet, "").WithContext(ctx)
	start := time.Now()
	_, err := tr.RoundTrip(req)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v, expected immediate return", elapsed)
	}
}

func TestRoundTripNilBaseUsesDefaultTransport(t *testing.T) {
	tr := &RetryTransport{Policy: fastPolicy}
	// An unroutable scheme makes http.DefaultTransport fail fast without
	// touching the network.
	req := newRequest(t, http.MethodGet, "")
	req.URL = &url.URL{Scheme: "bogus", Host: "nowhere"}

	if _, err := tr.RoundTrip(req); err == nil {
		t.Error("expected error from default transport, got nil")
	}
}

func TestShouldRetryStatus(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{http.StatusRequestTimeout, true},
		{http.StatusTooEarly, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusNotFound, false},
	}
	for _, tt := range tests {
		if got := shouldRetryStatus(tt.code); got != tt.want {
			t.Errorf("shouldRetryStatus(%d) = %t, want %t", tt.code, got, tt.want)
		}
	}
}

// timeoutErr implements net.Error with Timeout() == true.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestShouldRetryErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"canceled", context.Canceled, false},
		{"wrapped canceled", fmt.Errorf("w: %w", context.Canceled), false},
		{"deadline", context.DeadlineExceeded, true},
		{"eof", io.EOF, true},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"connreset", syscall.ECONNRESET, true},
		{"connrefused", syscall.ECONNREFUSED, true},
		{"net timeout", timeoutErr{}, true},
		{"plain", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetryErr(tt.err); got != tt.want {
				t.Errorf("shouldRetryErr(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		v    string
		want time.Duration
		ok   bool
	}{
		{"empty", "", 0, false},
		{"seconds", "5", 5 * time.Second, true},
		{"padded seconds", " 7 ", 7 * time.Second, true},
		{"negative", "-1", 0, false},
		{"http date future", now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second, true},
		{"http date past", now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
		{"garbage", "soonish", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tt.v, now)
			if ok != tt.ok || got != tt.want {
				t.Errorf("parseRetryAfter(%q) = (%v, %t), want (%v, %t)", tt.v, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestNextDelay(t *testing.T) {
	p := Policy{BaseDelay: time.Second, MaxDelay: 8 * time.Second}
	now := time.Now()

	t.Run("nil response uses backoff", func(t *testing.T) {
		d := nextDelay(0, p, nil, now)
		if d < 500*time.Millisecond || d > time.Second {
			t.Errorf("delay = %v, want within [500ms, 1s]", d)
		}
	})

	t.Run("retry-after raises delay and is clamped", func(t *testing.T) {
		resp := newResponse(http.StatusTooManyRequests, "")
		resp.Header.Set("Retry-After", "60")
		if d := nextDelay(0, p, resp, now); d != maxRetryAfter {
			t.Errorf("delay = %v, want %v (clamped)", d, maxRetryAfter)
		}
	})

	t.Run("smaller retry-after does not lower backoff", func(t *testing.T) {
		resp := newResponse(http.StatusTooManyRequests, "")
		resp.Header.Set("Retry-After", "0")
		d := nextDelay(3, p, resp, now)
		// attempt 3 → 8s capped, equal jitter floor is 4s.
		if d < 4*time.Second {
			t.Errorf("delay = %v, want >= 4s backoff floor", d)
		}
	})
}

func TestBackoffDelay(t *testing.T) {
	base := time.Second
	max := 8 * time.Second

	for attempt := 0; attempt < 5; attempt++ {
		expected := base << attempt
		if expected > max {
			expected = max
		}
		d := backoffDelay(attempt, base, max)
		if d < expected/2 || d > expected {
			t.Errorf("attempt %d: delay = %v, want within [%v, %v]", attempt, d, expected/2, expected)
		}
	}

	// A large attempt overflows the shift; the result must cap at max.
	if d := backoffDelay(63, base, max); d < max/2 || d > max {
		t.Errorf("overflow delay = %v, want within [%v, %v]", d, max/2, max)
	}
}

func TestSleepCtx(t *testing.T) {
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Errorf("zero delay err = %v, want nil", err)
	}
	if err := sleepCtx(context.Background(), -time.Second); err != nil {
		t.Errorf("negative delay err = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx err = %v, want context.Canceled", err)
	}
}

func TestPolicyDefaults(t *testing.T) {
	var p Policy
	if got := p.maxAttempts(); got != defaultMaxAttempts {
		t.Errorf("maxAttempts = %d, want %d", got, defaultMaxAttempts)
	}
	if got := p.baseDelay(); got != defaultBaseDelay {
		t.Errorf("baseDelay = %v, want %v", got, defaultBaseDelay)
	}
	if got := p.maxDelay(); got != defaultMaxDelay {
		t.Errorf("maxDelay = %v, want %v", got, defaultMaxDelay)
	}

	p = Policy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Second}
	if p.maxAttempts() != 2 || p.baseDelay() != time.Millisecond || p.maxDelay() != time.Second {
		t.Error("explicit policy values not honoured")
	}
}

func TestDrainAndCloseNilBody(t *testing.T) {
	// Must not panic.
	drainAndClose(nil)

	body := &recordingBody{Reader: bytes.NewReader([]byte("x"))}
	drainAndClose(body)
	if !body.drained || !body.closed {
		t.Errorf("drained=%t closed=%t, want both true", body.drained, body.closed)
	}
}
