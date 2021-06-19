package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func expect(target, err error) {
	if err != nil && err != target && !errors.Is(err, target) {
		panic(err)
	}
}

var (
	once       sync.Once
	tmpl       *template.Template
	httpClient = http.Client{Timeout: 15 * time.Second}
	proxies    = map[string]string{}
)

func parseEntries(container, https string, up bool) {
	entries := strings.Split(https, ",")
	for _, entry := range entries {
		parts := strings.Split(entry, ":")
		if len(parts) != 2 {
			fmt.Println("invalid entry", entry)
			continue
		}
		domain, port := parts[0], parts[1]
		if up {
			proxies[domain] = container + ":" + port
		} else {
			delete(proxies, domain)
		}
	}
}

// setCaddyProxies requests Caddy to proxy the domains to the containers
func setCaddyProxies() {
	t := time.Now()
	defer func() {
		fmt.Println("config loaded in", time.Since(t))
	}()

	// Determine if this a development or production domain by looking at the first domain to be proxied
	// Parse the right configuration template file
	once.Do(func() {
		for domain := range proxies {
			ips, err := net.LookupIP(domain)
			expect(nil, err)
			var config string
			if ips[0].IsLoopback() {
				config = "development.tmpl"
			} else {
				config = "production.tmpl"
			}
			tmpl, err = template.ParseFiles(config)
			expect(nil, err)
		}
	})

	// Prepare data for the template
	var domains []string
	for domain := range proxies {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	var data []struct{ Domain, Container string }
	for _, domain := range domains {
		data = append(data, struct{ Domain, Container string }{domain, proxies[domain]})
	}

	// Apply the template to the data and load the resulting JSON config in Caddy
	var buf bytes.Buffer
	expect(nil, tmpl.Execute(&buf, data))
	req, err := http.NewRequest("POST", "http://localhost:2019/load", &buf)
	expect(nil, err)
	req.Header.Set("content-type", "application/json")
	resp, err := httpClient.Do(req)
	expect(nil, err)
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	expect(nil, err)
	if resp.StatusCode != http.StatusOK {
		panic(resp.Status + " " + string(b))
	}
}

func main() {
	cli, err := client.NewClientWithOpts(client.WithAPIVersionNegotiation())
	expect(nil, err)
	expect(nil, exec.Command("caddy", "start").Run())

	// Connect to Docker events in order to detect new Caddy services
	ctx := context.Background()
	eventCh, errCh := cli.Events(ctx, types.EventsOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "org.01-edu.https"),
			filters.Arg("type", "container"),
			filters.Arg("event", "start"),
			filters.Arg("event", "die"),
			filters.Arg("event", "oom"),
		),
	})

	// Connection is established, proxy already existing services before proxying new ones
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "org.01-edu.https"),
		),
	})
	expect(nil, err)
	for _, container := range containers {
		containerName := container.Names[0][1:] // remove leading '/'
		https := container.Labels["org.01-edu.https"]
		parseEntries(containerName, https, true)
	}
	setCaddyProxies()

	// Proxy incoming services
	for {
		select {
		case err := <-errCh:
			expect(nil, err)
		case event := <-eventCh:
			containerName := event.Actor.Attributes["name"]
			https := event.Actor.Attributes["org.01-edu.https"]
			up := event.Status == "start"
			parseEntries(containerName, https, up)
			setCaddyProxies()
		}
	}
}
