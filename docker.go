package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Container is the part of a running container this tool reads.
type Container struct {
	ID     string            `json:"Id"`
	Labels map[string]string `json:"Labels"`
}

// DockerClient talks to the Docker Engine API over a unix socket or TCP,
// such as a read-only docker-socket-proxy.
type DockerClient struct {
	base   string
	http   *http.Client
	stream *http.Client
}

// NewDockerClient builds a client from a DOCKER_HOST value
// (unix:///path or tcp://host:port).
func NewDockerClient(dockerHost string) (*DockerClient, error) {
	u, err := url.Parse(dockerHost)
	if err != nil {
		return nil, fmt.Errorf("DOCKER_HOST %q: %w", dockerHost, err)
	}
	transport := &http.Transport{}
	base := ""
	switch u.Scheme {
	case "unix":
		socket := u.Path
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}
		base = "http://docker"
	case "tcp", "http":
		base = "http://" + u.Host
	default:
		return nil, fmt.Errorf("DOCKER_HOST %q: scheme must be unix or tcp", dockerHost)
	}
	return &DockerClient{
		base:   base,
		http:   &http.Client{Transport: transport, Timeout: 30 * time.Second},
		stream: &http.Client{Transport: transport},
	}, nil
}

// RunningContainers lists running containers with their labels.
func (d *DockerClient) RunningContainers(ctx context.Context) ([]Container, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+"/containers/json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("list containers: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var containers []Container
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	return containers, nil
}

// WatchContainerEvents streams container start, die and destroy events and
// calls onEvent for each one. It returns when the stream ends or ctx is done.
func (d *DockerClient) WatchContainerEvents(ctx context.Context, onEvent func(action string)) error {
	filters := `{"type":["container"],"event":["start","die","destroy"]}`
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		d.base+"/events?filters="+url.QueryEscape(filters), nil)
	if err != nil {
		return err
	}
	resp, err := d.stream.Do(req)
	if err != nil {
		return fmt.Errorf("watch events: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("watch events: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	dec := json.NewDecoder(bufio.NewReader(resp.Body))
	for {
		var ev struct {
			Type   string `json:"Type"`
			Action string `json:"Action"`
		}
		if err := dec.Decode(&ev); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("watch events: %w", err)
		}
		if ev.Type == "container" {
			onEvent(ev.Action)
		}
	}
}
