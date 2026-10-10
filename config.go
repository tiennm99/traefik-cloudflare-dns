package main

import (
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

const defaultComment = "managed by traefik-cloudflare-dns"

// Config holds every setting, read from environment variables.
type Config struct {
	APIToken       string
	ZoneID         string
	Domain         string
	Target         string
	RecordType     string
	Proxied        bool
	TTL            int
	Comment        string
	Tags           []string
	DeleteAfter    time.Duration
	ResyncInterval time.Duration
	DryRun         bool
	DockerHost     string
	LogLevel       string
}

// LoadConfig reads the configuration through getenv and validates it.
func LoadConfig(getenv func(string) string) (Config, error) {
	c := Config{
		APIToken:   strings.TrimSpace(getenv("CF_API_TOKEN")),
		ZoneID:     strings.TrimSpace(getenv("CF_ZONE_ID")),
		Domain:     strings.ToLower(strings.Trim(strings.TrimSpace(getenv("DOMAIN")), ".")),
		Target:     strings.TrimSpace(getenv("TARGET")),
		Comment:    getenv("RECORD_COMMENT"),
		DockerHost: strings.TrimSpace(getenv("DOCKER_HOST")),
		LogLevel:   strings.ToLower(strings.TrimSpace(getenv("LOG_LEVEL"))),
	}

	var missing []string
	for name, value := range map[string]string{
		"CF_API_TOKEN": c.APIToken,
		"CF_ZONE_ID":   c.ZoneID,
		"DOMAIN":       c.Domain,
		"TARGET":       c.Target,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return Config{}, fmt.Errorf("missing required variables: %s", strings.Join(missing, ", "))
	}

	c.RecordType = recordTypeFor(c.Target)
	if c.RecordType == "CNAME" {
		c.Target = strings.ToLower(strings.TrimSuffix(c.Target, "."))
	}

	if c.Comment == "" {
		c.Comment = defaultComment
	}
	if strings.ContainsAny(c.Comment, "\r\n") {
		return Config{}, fmt.Errorf("RECORD_COMMENT must not contain line breaks")
	}

	for _, tag := range strings.Split(getenv("RECORD_TAGS"), ",") {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if !strings.Contains(tag, ":") {
			return Config{}, fmt.Errorf("RECORD_TAGS entry %q is not in name:value form", tag)
		}
		c.Tags = append(c.Tags, tag)
	}

	var err error
	if c.Proxied, err = parseBool(getenv, "PROXIED", false); err != nil {
		return Config{}, err
	}
	if c.DryRun, err = parseBool(getenv, "DRY_RUN", false); err != nil {
		return Config{}, err
	}
	if c.TTL, err = parseInt(getenv, "TTL", 1); err != nil {
		return Config{}, err
	}
	if c.DeleteAfter, err = parseDuration(getenv, "DELETE_AFTER", time.Hour); err != nil {
		return Config{}, err
	}
	if c.ResyncInterval, err = parseDuration(getenv, "RESYNC_INTERVAL", time.Minute); err != nil {
		return Config{}, err
	}
	if c.ResyncInterval <= 0 {
		return Config{}, fmt.Errorf("RESYNC_INTERVAL must be positive")
	}

	if c.DockerHost == "" {
		c.DockerHost = "unix:///var/run/docker.sock"
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	return c, nil
}

// recordTypeFor returns A or AAAA for an IP target and CNAME for a hostname.
func recordTypeFor(target string) string {
	ip := net.ParseIP(target)
	switch {
	case ip == nil:
		return "CNAME"
	case ip.To4() != nil:
		return "A"
	default:
		return "AAAA"
	}
}

func parseBool(getenv func(string) string, name string, def bool) (bool, error) {
	v := strings.TrimSpace(getenv(name))
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %q is not a boolean", name, v)
	}
	return b, nil
}

func parseInt(getenv func(string) string, name string, def int) (int, error) {
	v := strings.TrimSpace(getenv(name))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not an integer", name, v)
	}
	return n, nil
}

func parseDuration(getenv func(string) string, name string, def time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(getenv(name))
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%s: %q is not a non-negative duration such as 1h or 30m", name, v)
	}
	return d, nil
}

// osGetenv is the production getenv.
var osGetenv = os.Getenv
