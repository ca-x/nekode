ARG GO_VERSION=1.26
ARG NODE_VERSION=22

FROM node:${NODE_VERSION}-alpine AS web
WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

FROM golang:${GO_VERSION}-alpine AS build
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web /src/web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags="-s -w \
      -X 'github.com/ca-x/nekode/internal/version.Version=${VERSION}' \
      -X 'github.com/ca-x/nekode/internal/version.Commit=${COMMIT}' \
      -X 'github.com/ca-x/nekode/internal/version.BuildTime=${BUILD_TIME}'" \
    -o /out/nekode ./cmd/nekode

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -g 1000 app \
    && adduser -D -u 1000 -G app app

WORKDIR /app

COPY --from=build /out/nekode /app/nekode
COPY --from=web /src/web/dist /app/web/dist

RUN mkdir -p /data \
    && chown -R app:app /app /data

USER app

ENV NEKODE_ADDR=:18790
ENV NEKODE_BASE_URL=http://localhost:18790
ENV NEKODE_DATA_DIR=/data
ENV NEKODE_DB_TYPE=sqlite
ENV NEKODE_DB_DSN=/data/nekode.db

VOLUME ["/data"]
EXPOSE 18790

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -q -T 3 -O /dev/null http://127.0.0.1:18790/health || exit 1

ENTRYPOINT ["/app/nekode"]
CMD ["serve", "--addr", ":18790"]
