package oauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCodexRefresh(t *testing.T) {
	originalClient := http.DefaultClient
	defer func() {
		http.DefaultClient = originalClient
	}()

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got, want := req.Method, http.MethodPost; got != want {
			t.Fatalf("expected method %s, got %s", want, got)
		}
		if got, want := req.URL.String(), codexTokenURL; got != want {
			t.Fatalf("expected url %s, got %s", want, got)
		}
		if got := req.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("expected form content-type, got %q", got)
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("failed reading request body: %v", err)
		}
		payload := string(body)
		for _, expected := range []string{
			"grant_type=refresh_token",
			"client_id=test-client-id",
			"refresh_token=old-refresh-token",
		} {
			if !strings.Contains(payload, expected) {
				t.Fatalf("expected payload %q to contain %q", payload, expected)
			}
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"new-access-token","token_type":"Bearer","expires_in":3600}`)),
			Header:     make(http.Header),
		}, nil
	})}

	provider := NewCodexProvider("test-client-id")
	resp, err := provider.Refresh(context.Background(), "old-refresh-token")
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if got, want := resp.AccessToken, "new-access-token"; got != want {
		t.Fatalf("expected access token %q, got %q", want, got)
	}
	if got, want := resp.RefreshToken, "old-refresh-token"; got != want {
		t.Fatalf("expected refresh token %q, got %q", want, got)
	}
	if resp.ExpiresAt.Before(time.Now().Add(59 * time.Minute)) {
		t.Fatalf("expected expiration to be set in the future, got %v", resp.ExpiresAt)
	}
}
