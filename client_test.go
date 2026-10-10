package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCloudflareClientPaginatesAndFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing token")
		}
		page := r.URL.Query().Get("page")
		results := map[string]string{
			"1": `[{"id":"1","type":"A","name":"a.example.com","content":"192.0.2.1"},{"id":"2","type":"TXT","name":"t.example.com","content":"x"}]`,
			"2": `[{"id":"3","type":"CNAME","name":"c.example.com","content":"h.example.net","comment":"managed"}]`,
		}[page]
		fmt.Fprintf(w, `{"success":true,"errors":[],"result":%s,"result_info":{"page":%s,"total_pages":2}}`, results, page)
	}))
	defer srv.Close()

	c := NewCloudflareClient("tok", "zone")
	c.base = srv.URL
	records, err := c.ListRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[1].Comment != "managed" {
		t.Fatalf("records = %+v", records)
	}
}

func TestCloudflareClientCreateSendsCommentAndTags(t *testing.T) {
	var got Record
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/zones/zone/dns_records" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		got.ID = "new"
		b, _ := json.Marshal(got)
		fmt.Fprintf(w, `{"success":true,"errors":[],"result":%s}`, b)
	}))
	defer srv.Close()

	c := NewCloudflareClient("tok", "zone")
	c.base = srv.URL
	rec, err := c.CreateRecord(context.Background(), Record{
		Type: "A", Name: "a.example.com", Content: "192.0.2.1", TTL: 1,
		Comment: "managed by traefik-cloudflare-dns", Tags: []string{"owner:x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID != "new" || got.Comment != "managed by traefik-cloudflare-dns" || len(got.Tags) != 1 {
		t.Fatalf("sent %+v, got %+v", got, rec)
	}
}

func TestCloudflareClientReportsAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}],"result":null}`)
	}))
	defer srv.Close()

	c := NewCloudflareClient("tok", "zone")
	c.base = srv.URL
	_, err := c.ListRecords(context.Background())
	if err == nil || !strings.Contains(err.Error(), "10000 Authentication error") {
		t.Fatalf("err = %v", err)
	}
}

func TestDockerClientListsAndStreamsEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/containers/json":
			fmt.Fprint(w, `[{"Id":"abc","Labels":{"traefik.http.routers.r.rule":"Host(`+"`a.example.com`"+`)"}}]`)
		case "/events":
			if !strings.Contains(r.URL.Query().Get("filters"), `"start"`) {
				t.Errorf("filters = %s", r.URL.Query().Get("filters"))
			}
			fmt.Fprint(w, `{"Type":"container","Action":"start"}`+"\n"+`{"Type":"container","Action":"die"}`+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d, err := NewDockerClient("tcp://" + strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	containers, err := d.RunningContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 || HostsFromLabels(containers[0].Labels)[0] != "a.example.com" {
		t.Fatalf("containers = %+v", containers)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var actions []string
	_ = d.WatchContainerEvents(ctx, func(a string) { actions = append(actions, a) })
	if strings.Join(actions, ",") != "start,die" {
		t.Fatalf("actions = %v", actions)
	}
}

func TestNewDockerClientRejectsUnknownScheme(t *testing.T) {
	if _, err := NewDockerClient("ssh://host"); err == nil {
		t.Fatal("expected an error")
	}
}
