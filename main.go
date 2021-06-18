package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
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

func readFile(name string) string {
	b, err := os.ReadFile(name)
	expect(nil, err)
	return string(b)
}

var (
	once       sync.Once
	alreadySet = map[string]struct{}{}
	httpClient = http.Client{Timeout: 15 * time.Second}
)

// setCaddyProxy requests caddy to proxy the domain to the container
func setCaddyProxy(domain, container string) {
	// Don't proxy a domain name twice
	if _, ok := alreadySet[domain]; ok {
		return
	}
	alreadySet[domain] = struct{}{}

	// Apply the JSON config in caddy at the path specified
	setCaddyConfig := func(path, config string) {
		req, err := http.NewRequest("POST", "http://localhost:2019/config/"+path, bytes.NewBufferString(config))
		expect(nil, err)
		req.Header.Set("content-type", "application/json")
		resp, err := httpClient.Do(req)
		expect(nil, err)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			panic(resp.Status + " " + path + " " + config)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Determine if this a development or production machine by looking at the first domain name to be proxied
	once.Do(func() {
		ips, err := net.LookupIP(domain)
		expect(nil, err)
		if ips[0].IsLoopback() {
			expect(nil, os.Chdir("development"))
		} else {
			expect(nil, os.Chdir("production"))
		}
		setCaddyConfig("", readFile("base.json"))
	})

	// Apply all config files
	t := time.Now()
	files, err := os.ReadDir(".")
	expect(nil, err)
	for _, file := range files {
		if file.Name() == "base.json" {
			continue
		}
		config := readFile(file.Name())
		config = strings.ReplaceAll(config, "{{DOMAIN}}", domain)
		config = strings.ReplaceAll(config, "{{CONTAINER}}", container)
		path := strings.TrimSuffix(file.Name(), ".json")
		path = strings.SplitN(path, "-", 2)[0] // Ignore the text after "-" (included)
		path = strings.ReplaceAll(path, ".", "/")
		setCaddyConfig(path, config)
	}
	fmt.Println(domain, "=>", container, "in", time.Since(t))
}

func main() {
	cli, err := client.NewClientWithOpts(client.WithAPIVersionNegotiation())
	expect(nil, err)
	expect(nil, exec.Command("caddy", "start").Run())

	// Connect to Docker events in order to detect new caddy services
	ctx := context.Background()
	eventCh, errCh := cli.Events(ctx, types.EventsOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "com.docker.compose.service=caddy"),
			filters.Arg("label", "org.01-edu.domain"),
			filters.Arg("type", "container"),
			filters.Arg("event", "start"),
		),
	})

	// Connection is established, proxy already existing services before proxying new ones
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "com.docker.compose.service=caddy"),
			filters.Arg("label", "org.01-edu.domain"),
		),
	})
	expect(nil, err)
	for _, container := range containers {
		setCaddyProxy(container.Labels["org.01-edu.domain"], container.Names[0][1:])
	}

	// Proxy incoming services
	for {
		select {
		case err := <-errCh:
			expect(nil, err)
		case event := <-eventCh:
			setCaddyProxy(event.Actor.Attributes["org.01-edu.domain"], event.Actor.Attributes["name"])
		}
	}
}
