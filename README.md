# traefik-cloudflare-dns

**Cloudflare DNS records that follow your Traefik containers.** traefik-cloudflare-dns watches Docker for containers whose Traefik router rules name a host, creates a Cloudflare record for each new host, and deletes the records it created once their host has been gone long enough.

- **Follows Docker events.** It reacts when a container starts, stops or is removed, and resyncs every minute in case an event was missed.
- **Never touches records it didn't make.** It only treats a record as its own when the record points at this instance's `TARGET` and carries its comment and tags. Records made by hand, or by another instance pointing at another server, stay as they are.
- **Waits before deleting.** A host must be down for `DELETE_AFTER` (1 hour by default) before its record is removed, so redeploys and restarts never cause a DNS gap.
- **Small and safe.** A static, distroless, non-root image with no ports, built to sit behind a read-only Docker socket proxy.

## How it decides what to create

Every running container's `traefik.http.routers.<name>.rule` labels are read. Each hostname in a `Host(...)` matcher that is `DOMAIN` or a name under it gets a record:

| `TARGET` | Record |
| --- | --- |
| IPv4 address | `A` |
| IPv6 address | `AAAA` |
| hostname | `CNAME` |

If a record with that name already exists and isn't owned by this instance, it is left alone, and a warning is logged once.

A typical setup is one wildcard record (`*.example.com`) for your main server, plus one instance of this tool on every other server, with `TARGET` set to that server's IP. Explicit records take priority over the wildcard, so each app's name resolves to the server it runs on.

## Quick start

```yaml
services:
  traefik-cloudflare-dns:
    image: ghcr.io/tiennm99/traefik-cloudflare-dns:1
    restart: unless-stopped
    environment:
      CF_API_TOKEN: ${CF_API_TOKEN:?required}
      CF_ZONE_ID: ${CF_ZONE_ID:?required}
      DOMAIN: example.com
      TARGET: 192.0.2.10
      DOCKER_HOST: tcp://dockerproxy:2375
    depends_on:
      - dockerproxy

  dockerproxy:
    image: tecnativa/docker-socket-proxy:latest
    restart: unless-stopped
    environment:
      CONTAINERS: 1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
```

The proxy only needs `CONTAINERS`. Events and ping are allowed by the proxy's defaults, and every `POST` is refused, so the tool can read Docker but never change it.

Create the Cloudflare API token with **Zone → DNS → Edit**, limited to the one zone.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `CF_API_TOKEN` | required | Cloudflare API token with DNS edit on the zone |
| `CF_ZONE_ID` | required | Zone ID, from the zone's overview page |
| `DOMAIN` | required | Only hosts equal to or under this name are managed |
| `TARGET` | required | Record content: this server's IP, or a hostname for a CNAME |
| `PROXIED` | `false` | Create orange-cloud (proxied) records |
| `TTL` | `1` | Record TTL in seconds; `1` means automatic |
| `RECORD_COMMENT` | `managed by traefik-cloudflare-dns` | Comment set on every record, and part of ownership |
| `RECORD_TAGS` | empty | Comma-separated `name:value` tags, set on every record and part of ownership |
| `DELETE_AFTER` | `1h` | How long a host must be down before its record is deleted |
| `RESYNC_INTERVAL` | `1m` | Full resync period, on top of Docker events |
| `DRY_RUN` | `false` | Log creates and deletes without making them |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker API endpoint, `unix://` or `tcp://` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |

### Comments and tags

Cloudflare allows record comments on every plan. They are limited to 100 characters on Free and 500 on paid plans. **Tags are only available on paid plans**, and on a Free zone Cloudflare rejects any record that has them, so leave `RECORD_TAGS` empty there.

Ownership is matched exactly. If you change `RECORD_COMMENT` or `RECORD_TAGS`, records created under the old values are no longer this instance's, so they won't be deleted automatically. Update or remove them by hand.

### Deletion timing

The clock for `DELETE_AFTER` starts when a resync finds an owned record whose host no longer belongs to any running container. It resets if the host comes back. The timers live in memory, so a restart starts them over. That can only delay a deletion, never bring one forward.

Give each server's instance its own `TARGET`. Two instances pointing at the same target with the same comment would each treat the other's records as their own.

## Development

```bash
go vet ./...
go test -race ./...
docker build -t traefik-cloudflare-dns:dev .
```

CI runs the same checks on every push. Pushes to `main` publish `ghcr.io/tiennm99/traefik-cloudflare-dns:edge`. A `vX.Y.Z` tag publishes `X.Y.Z`, `X.Y` and `X`.

## License

[Apache 2.0](LICENSE)
