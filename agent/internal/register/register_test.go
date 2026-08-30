package register

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/YCJE/XProbe/internal/model"
)

func TestRegister_HappyPathAndHeaders(t *testing.T) {
	var gotAuth, gotCT, gotPath string
	var gotBody model.RegisterRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotCT = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"token":"tok-123","agent_id":7}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ServerURL: srv.URL}
	resp, err := c.Register(context.Background(), model.RegisterRequest{RegisterCode: "CODE123", Hostname: "h"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.Token != "tok-123" || resp.AgentID != 7 {
		t.Fatalf("resp = %+v", resp)
	}
	if gotPath != "/api/v1/agent/register" || gotAuth != "" || gotCT != "application/json" {
		t.Fatalf("path/auth/content-type = %s/%s/%s", gotPath, gotAuth, gotCT)
	}
	if gotBody.RegisterCode != "CODE123" {
		t.Fatalf("body = %+v", gotBody)
	}
}

func TestRegister_ErrorStatus(t *testing.T) {
	cases := map[int]string{
		http.StatusUnauthorized:    "register code invalid",
		http.StatusConflict:        "host fingerprint conflict",
		http.StatusTooManyRequests: "rate limit exceeded",
	}
	for status, msg := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte(`{"error":"` + msg + `"}`))
		}))
		c := &Client{HTTP: srv.Client(), ServerURL: srv.URL}
		_, err := c.Register(context.Background(), model.RegisterRequest{})
		srv.Close()
		if err == nil || status < 400 {
			t.Fatalf("status %d should error", status)
		}
	}
}
