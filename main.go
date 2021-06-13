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

var initialized bool

func setCaddyProxy(domain, container string) {
	if !initialized {
		initialized = true
		ips, err := net.LookupIP(domain)
		expect(nil, err)
		if ips[0].IsLoopback() {
			expect(nil, os.Chdir("development"))
		} else {
			expect(nil, os.Chdir("production"))
		}
		load("", readFile("base.json"))
	}
	t := time.Now()
	files, err := os.ReadDir(".")
	expect(nil, err)
	for _, file := range files {
		config := readFile(file.Name())
		config = strings.ReplaceAll(config, "{{DOMAIN}}", domain)
		config = strings.ReplaceAll(config, "{{CONTAINER}}", container)
		path := strings.TrimSuffix(file.Name(), ".json")
		path = strings.SplitN(path, "-", 2)[0]
		path = strings.ReplaceAll(path, ".", "/")
		load(path, config)
	}
	fmt.Println(domain, "=>", container, "in", time.Since(t))
}

func main() {
	cli, err := client.NewClientWithOpts(client.WithAPIVersionNegotiation())
	expect(nil, err)
	expect(nil, exec.Command("caddy", "start").Run())
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
