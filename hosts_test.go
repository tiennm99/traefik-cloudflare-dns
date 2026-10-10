package main

import (
	"slices"
	"testing"
)

func TestHostsFromLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   []string
	}{
		{
			name: "coolify rule with path prefix",
			labels: map[string]string{
				"traefik.enable":                       "true",
				"traefik.http.routers.https-0-x.rule":  "Host(`App.Example.com`) && PathPrefix(`/`)",
				"traefik.http.routers.http-0-x.rule":   "Host(`app.example.com`) && PathPrefix(`/`)",
				"traefik.http.services.x.loadbalancer": "ignored",
			},
			want: []string{"app.example.com"},
		},
		{
			name: "several hosts in one matcher and several matchers",
			labels: map[string]string{
				"traefik.http.routers.r.rule": "Host(`a.example.com`, `b.example.com`) || Host(`c.example.com.`)",
			},
			want: []string{"a.example.com", "b.example.com", "c.example.com"},
		},
		{
			name: "host regexp and tcp routers are ignored",
			labels: map[string]string{
				"traefik.http.routers.r.rule": "HostRegexp(`{sub:[a-z]+}.example.com`)",
				"traefik.tcp.routers.t.rule":  "HostSNI(`db.example.com`)",
			},
			want: nil,
		},
		{
			name: "traefik disabled",
			labels: map[string]string{
				"traefik.enable":              "false",
				"traefik.http.routers.r.rule": "Host(`a.example.com`)",
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HostsFromLabels(tt.labels)
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInDomain(t *testing.T) {
	for host, want := range map[string]bool{
		"example.com":          true,
		"app.example.com":      true,
		"a.b.example.com":      true,
		"notexample.com":       false,
		"example.com.evil.net": false,
	} {
		if got := InDomain(host, "example.com"); got != want {
			t.Errorf("InDomain(%q) = %v, want %v", host, got, want)
		}
	}
}
