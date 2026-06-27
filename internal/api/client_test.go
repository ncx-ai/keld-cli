package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeviceStartAndPoll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cli/device/start":
			w.Write([]byte(`{"device_code":"dc","user_code":"UC","verification_url":"https://v","interval":1,"expires_in":2}`))
		case "/v1/cli/device/poll":
			w.WriteHeader(202)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "")
	ds, err := c.DeviceStart()
	if err != nil || ds.UserCode != "UC" {
		t.Fatalf("device start %v %v", ds, err)
	}
	res, err := c.DevicePoll("dc")
	if err != nil || res != nil {
		t.Fatalf("202 should give nil,nil; got %v %v", res, err)
	}
}

func TestOnboardingRequiresToken(t *testing.T) {
	c := NewClient("https://x", "")
	if _, err := c.Onboarding(); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestAtlasErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "")
	_, err := c.DeviceStart()
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
}
