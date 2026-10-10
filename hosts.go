package main

import (
	"regexp"
	"strings"
)

var (
	routerRuleLabel = regexp.MustCompile(`^traefik\.http\.routers\.[^.]+\.rule$`)
	hostMatcher     = regexp.MustCompile(`Host\(([^)]*)\)`)
	backtickValue   = regexp.MustCompile("`([^`]+)`")
)

// HostsFromLabels returns the hostnames that a container's Traefik HTTP
// router rules match with Host(...), lowercased and deduplicated.
// HostRegexp and other matchers are ignored. A container labelled
// traefik.enable=false yields nothing.
func HostsFromLabels(labels map[string]string) []string {
	if strings.EqualFold(strings.TrimSpace(labels["traefik.enable"]), "false") {
		return nil
	}
	seen := map[string]bool{}
	var hosts []string
	for key, rule := range labels {
		if !routerRuleLabel.MatchString(key) {
			continue
		}
		for _, m := range hostMatcher.FindAllStringSubmatch(rule, -1) {
			for _, v := range backtickValue.FindAllStringSubmatch(m[1], -1) {
				host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(v[1]), "."))
				if host != "" && !seen[host] {
					seen[host] = true
					hosts = append(hosts, host)
				}
			}
		}
	}
	return hosts
}

// InDomain reports whether host is domain itself or a name under it.
func InDomain(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}
