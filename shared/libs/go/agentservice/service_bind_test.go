package agentservice

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

// TestLaunch_BindHost verifies the default and the override of TERN_AGENT_BIND_HOST.
func TestLaunch_BindHost(t *testing.T) {
	tests := []struct {
		name     string
		env      string // empty means unset
		wantHost string // "0.0.0.0" matches any unspecified address (Go reports it as "::")
	}{
		{"unset defaults to loopback", "", "127.0.0.1"},
		{"explicit loopback", "127.0.0.1", "127.0.0.1"},
		{"all interfaces", "0.0.0.0", "0.0.0.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env == "" {
				t.Setenv("TERN_AGENT_BIND_HOST", "")
				os.Unsetenv("TERN_AGENT_BIND_HOST")
			} else {
				t.Setenv("TERN_AGENT_BIND_HOST", tc.env)
			}
			srv := New()
			if err := srv.Launch(context.Background(), 0); err != nil {
				t.Fatalf("Launch: %v", err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = srv.Shutdown(ctx)
			}()
			host, _, err := net.SplitHostPort(srv.ln.Addr().String())
			if err != nil {
				t.Fatalf("SplitHostPort(%q): %v", srv.ln.Addr().String(), err)
			}
			if tc.wantHost == "0.0.0.0" {
				if ip := net.ParseIP(host); ip == nil || !ip.IsUnspecified() {
					t.Errorf("bound host = %q, want an unspecified address", host)
				}
				return
			}
			if host != tc.wantHost {
				t.Errorf("bound host = %q, want %q", host, tc.wantHost)
			}
		})
	}
}
