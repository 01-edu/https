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

var httpClient = http.Client{Timeout: 15 * time.Second}

func load(path, config string) {
	req, err := http.NewRequest("POST", "http://localhost:2019/config/"+path, bytes.NewBufferString(config))
	expect(nil, err)
	req.Header.Set("content-type", "application/json")
	resp, err := httpClient.Do(req)
	expect(nil, err)
	expect(nil, resp.Body.Close())
}

func readFile(name string) string {
	b, err := os.ReadFile(name)
	expect(nil, err)
	return string(b)
}

func setCaddyProxy(domain, container string) {
	loadConfig := func(filename string) {
		config := readFile("config/" + filename)
		config = strings.ReplaceAll(config, "{{DOMAIN}}", domain)
		config = strings.ReplaceAll(config, "{{CONTAINER}}", container)
		path := strings.TrimSuffix(filename, ".json")
		path = strings.SplitN(path, "-", 2)[0]
		path = strings.ReplaceAll(path, ".", "/")
		load(path, config)
	}
	t := time.Now()
	ips, err := net.LookupIP(domain)
	expect(nil, err)
	if ips[0].IsLoopback() {
		loadConfig("apps.http.servers.srv0.tls_connection_policies-app.json")
		loadConfig("apps.http.servers.srv0.tls_connection_policies-gitea.json")
		loadConfig("apps.tls.certificates.load_files.json")
	}
	loadConfig("apps.http.servers.srv0.routes-app.json")
	loadConfig("apps.http.servers.srv0.routes-gitea.json")
	fmt.Println(domain, "=>", container, "in", time.Since(t))
}

func main() {
	cli, err := client.NewClientWithOpts(client.WithAPIVersionNegotiation())
	expect(nil, err)
	expect(nil, exec.Command("caddy", "start").Run())
	load("", readFile("config.json"))
	ctx := context.Background()
	eventCh, errCh := cli.Events(ctx, types.EventsOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "com.docker.compose.service=caddy"),
			filters.Arg("label", "org.01-edu.domain"),
			filters.Arg("type", "container"),
			filters.Arg("event", "start"),
		),
	})
	// Connection is established, process already existing services before processing new ones
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

	// Process events
	for {
		select {
		case err := <-errCh:
			expect(nil, err)
		case event := <-eventCh:
			setCaddyProxy(event.Actor.Attributes["org.01-edu.domain"], event.Actor.Attributes["name"])
		}
	}
}
