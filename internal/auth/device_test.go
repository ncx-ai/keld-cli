package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ncx-ai/keld-cli/internal/api"
)

func TestLoginPollsThenSucceeds(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cli/device/start":
			w.Write([]byte(`{"device_code":"dc","user_code":"UC","verification_url":"https://v","interval":1,"expires_in":10}`))
		case "/v1/cli/device/poll":
			calls++
			if calls < 2 {
				w.WriteHeader(202)
				return
			}
			w.Write([]byte(`{"access_token":"AT","principal":"p","org":"o"}`))
		}
	}))
	defer srv.Close()
	got, err := Login(api.NewClient(srv.URL, ""), false, func(time.Duration) {}, func(string) error { return nil })
	if err != nil || got.AccessToken != "AT" {
		t.Fatalf("login %v %v", got, err)
	}
}

func TestLoginContinuesWhenBrowserOpenFails(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cli/device/start":
			w.Write([]byte(`{"device_code":"dc","user_code":"UC","verification_url":"https://v","interval":1,"expires_in":10}`))
		case "/v1/cli/device/poll":
			w.Write([]byte(`{"access_token":"AT","principal":"p","org":"o"}`))
		}
	}))
	defer srv.Close()
	// openBrowser=true with an opener that always fails (e.g. headless/SSH/CI).
	// Login must still proceed to poll and succeed.
	got, err := Login(api.NewClient(srv.URL, ""), true, func(time.Duration) {}, func(string) error {
		return errors.New("no browser available")
	})
	if err != nil {
		t.Fatalf("login should not abort on browser-open failure: %v", err)
	}
	if got == nil || got.AccessToken != "AT" {
		t.Fatalf("expected AuthData with AT, got %v", got)
	}
}

func TestLoginTimesOut(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cli/device/start":
			// expires_in=1, interval=1 → loop runs once then times out
			w.Write([]byte(`{"device_code":"dc","user_code":"UC","verification_url":"https://v","interval":1,"expires_in":1}`))
		case "/v1/cli/device/poll":
			w.WriteHeader(202)
		}
	}))
	defer srv.Close()
	_, err := Login(api.NewClient(srv.URL, ""), false, func(time.Duration) {}, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if err.Error() != "login timed out; please run `keld login` again" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestLoginPollsDespiteBlockingOpener is the regression test for the hang where
// the browser opener blocked the device-poll loop (some Linux xdg-open setups do
// not return until the browser closes). Login must poll and succeed even when the
// opener never returns.
func TestLoginPollsDespiteBlockingOpener(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cli/device/start":
			w.Write([]byte(`{"device_code":"dc","user_code":"UC","verification_url":"https://v","interval":1,"expires_in":10}`))
		case "/v1/cli/device/poll":
			w.Write([]byte(`{"access_token":"AT","principal":"p","org":"o"}`))
		}
	}))
	defer srv.Close()

	block := make(chan struct{})
	defer close(block)
	blockingOpener := func(string) error { <-block; return nil } // never returns until the test ends

	done := make(chan *AuthData, 1)
	go func() {
		got, err := Login(api.NewClient(srv.URL, ""), true, func(time.Duration) {}, blockingOpener)
		if err == nil {
			done <- got
		}
	}()

	select {
	case got := <-done:
		if got == nil || got.AccessToken != "AT" {
			t.Fatalf("expected AuthData with AT, got %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Login blocked on the browser opener — the device-poll loop never ran")
	}
}
