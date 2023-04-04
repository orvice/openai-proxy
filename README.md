# openai-proxy


## Deploy

`docker-compose.yml` example:

```
   openai-proxy:
    image: ghcr.io/orvice/openai-proxy:main
    restart: always
    container_name:   openai-proxy
    ports:
      - 8080
    environment:
      - OPENAI_API_KEY=sk-xxxx
```