# https

## Build

```
docker build -t docker.01-edu.org/https .
```

## Run

```
docker run \
    --detach \
    --name https \
    --network endpoint \
    --restart unless-stopped \
    --volume /var/run/docker.sock:/var/run/docker.sock:ro \
    --volume caddy_config:/config \
    --volume caddy_data:/data \
    --publish 80:80 \
    --publish 443:443 \
    docker.01-edu.org/https
```
