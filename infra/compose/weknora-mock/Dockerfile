# Mock WeKnora service for CI/integration testing
# 源码在同目录 main.go（此前源码以 printf 拼接在 Dockerfile 内，无法本地复用/测试）。
# 说明：本 mock 只服务 API 端口（API_PORT，默认 9000）；compose 额外发布的
# WEKNORA_WEB_PUBLISH_PORT->9001 对应真实 WeKnora 的 Web UI，mock 未实现。
FROM alpine:latest

# curl 供容器 healthcheck 使用（compose 里 healthcheck 调 curl）
RUN apk --no-cache add ca-certificates curl

COPY --from=golang:1.25-alpine /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH

WORKDIR /app

COPY main.go ./

RUN go mod init weknora-mock && \
    CGO_ENABLED=0 go build -o weknora-mock .

EXPOSE 9000 9001

CMD ["./weknora-mock"]
