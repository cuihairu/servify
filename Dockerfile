# Stage 1: Build admin frontend
FROM node:20-alpine AS admin-builder

RUN corepack enable && corepack prepare pnpm@latest --activate

WORKDIR /app/apps/admin
COPY apps/admin/package.json apps/admin/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY apps/admin/ .
RUN pnpm build

# Stage 2: Build Go server
# Base tracks apps/server/go.mod go directive; GOTOOLCHAIN=local in the
# golang image means the pinned major must satisfy the module requirement.
FROM golang:1.26-alpine AS builder

WORKDIR /app
# Build uses module in apps/server
WORKDIR /app/apps/server
COPY apps/server/go.mod .
COPY apps/server/go.sum .
RUN go mod download

WORKDIR /app
COPY . .
# Copy admin dist from previous stage
COPY --from=admin-builder /app/apps/admin/dist ./apps/admin/dist
# Build server binary by default (apps/server)
RUN CGO_ENABLED=0 GOOS=linux go build -tags weknora -o servify ./apps/server/cmd/server

# Stage 3: Runtime
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/servify .
COPY --from=builder /app/apps/admin/dist ./apps/admin/dist
# Config file can be mounted as ./config.yml at runtime

EXPOSE 8080

CMD ["./servify"]
