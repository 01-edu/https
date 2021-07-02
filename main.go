package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
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

// {
//         "dev.01-edu.org": "all_caddy_1:8080",
//     "git.dev.01-edu.org": "all_caddy_1:8081",
//        "test.01-edu.org": "test01-eduorg_caddy_1:8080",
//    "git.test.01-edu.org": "test01-eduorg_caddy_1:8081",
// }
var proxies = map[string]string{}

// parseEntries parses Docker label "org.01-edu.https" and set proxies accordingly
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

var (
	initialized bool
	tmpl        *template.Template
	httpClient  = http.Client{Timeout: 15 * time.Second}
	development = flag.Bool("dev", false, "development")
)

// setCaddyProxies requests Caddy to proxy the domains to the containers
func setCaddyProxies() {
	// Determine if this a development or production domain by looking at the first domains to be proxied
	// Parse the right configuration template file
	if !initialized {
		if len(proxies) == 0 {
			return
		}
		initialized = true
		for domain := range proxies {
			ips, err := net.LookupIP(domain)
			expect(nil, err)
			if ips[0].IsLoopback() {
				*development = true
				break
			}
		}
		var err error
		if *development {
			tmpl, err = template.ParseFiles("development.tmpl")
		} else {
			tmpl, err = template.ParseFiles("production.tmpl")
		}
		expect(nil, err)
		fmt.Println("template:", tmpl.Name())
	}

	t := time.Now()
	defer func() {
		fmt.Println("config loaded in", time.Since(t), ":")
		for domain, container := range proxies {
			fmt.Println(" ", domain, "=>", container)
		}
		fmt.Println()
	}()

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
	flag.Parse()

	cli, err := client.NewClientWithOpts(client.WithAPIVersionNegotiation())
	expect(nil, err)
	cmd := exec.Command("caddy", "run")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	expect(nil, cmd.Start())
	time.Sleep(time.Second)

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
		up := true // only running containers are listed
		parseEntries(containerName, https, up)
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
