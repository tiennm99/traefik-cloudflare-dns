package main

import (
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func requiredEnv() map[string]string {
	return map[string]string{
		"CF_API_TOKEN": "token",
		"CF_ZONE_ID":   "zone",
		"DOMAIN":       "Example.com.",
		"TARGET":       "192.0.2.10",
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	c, err := LoadConfig(envOf(requiredEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Domain != "example.com" || c.RecordType != "A" || c.TTL != 1 || c.Proxied || c.DryRun {
		t.Fatalf("unexpected config: %+v", c)
	}
	if c.Comment != "managed by traefik-cloudflare-dns" {
		t.Fatalf("comment = %q", c.Comment)
	}
	if c.DeleteAfter != time.Hour || c.ResyncInterval != time.Minute {
		t.Fatalf("durations = %v, %v", c.DeleteAfter, c.ResyncInterval)
	}
	if c.DockerHost != "unix:///var/run/docker.sock" {
		t.Fatalf("docker host = %q", c.DockerHost)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	_, err := LoadConfig(envOf(map[string]string{"DOMAIN": "example.com"}))
	if err == nil || !strings.Contains(err.Error(), "CF_API_TOKEN, CF_ZONE_ID, TARGET") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadConfigRecordType(t *testing.T) {
	for target, want := range map[string]string{
		"192.0.2.10":        "A",
		"2001:db8::1":       "AAAA",
		"Host.Example.net.": "CNAME",
	} {
		env := requiredEnv()
		env["TARGET"] = target
		c, err := LoadConfig(envOf(env))
		if err != nil {
			t.Fatal(err)
		}
		if c.RecordType != want {
			t.Errorf("TARGET %q: type %s, want %s", target, c.RecordType, want)
		}
	}
}

func TestLoadConfigOptional(t *testing.T) {
	env := requiredEnv()
	env["RECORD_COMMENT"] = "created by the jp companion"
	env["RECORD_TAGS"] = "managed-by:traefik-cloudflare-dns, server:jp"
	env["PROXIED"] = "true"
	env["DELETE_AFTER"] = "30m"
	env["DRY_RUN"] = "1"
	c, err := LoadConfig(envOf(env))
	if err != nil {
		t.Fatal(err)
	}
	if c.Comment != "created by the jp companion" || !c.Proxied || !c.DryRun || c.DeleteAfter != 30*time.Minute {
		t.Fatalf("unexpected config: %+v", c)
	}
	if len(c.Tags) != 2 || c.Tags[1] != "server:jp" {
		t.Fatalf("tags = %v", c.Tags)
	}
}

func TestLoadConfigInvalid(t *testing.T) {
	for name, kv := range map[string][2]string{
		"bad tag":      {"RECORD_TAGS", "nocolon"},
		"bad bool":     {"PROXIED", "maybe"},
		"bad duration": {"DELETE_AFTER", "soon"},
		"line break":   {"RECORD_COMMENT", "a\nb"},
		"zero resync":  {"RESYNC_INTERVAL", "0s"},
	} {
		env := requiredEnv()
		env[kv[0]] = kv[1]
		if _, err := LoadConfig(envOf(env)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
