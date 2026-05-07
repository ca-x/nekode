ARG GO_VERSION=1.26.0

FROM golang:${GO_VERSION} AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X github.com/ca-x/nekode/internal/version.Version=${VERSION:-dev} -X github.com/ca-x/nekode/internal/version.Commit=${COMMIT:-unknown} -X github.com/ca-x/nekode/internal/version.BuildTime=${BUILD_TIME:-unknown}" \
    -o /out/nekode ./cmd/nekode

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/nekode /app/nekode
EXPOSE 18790
ENTRYPOINT ["/app/nekode"]
CMD ["serve", "--addr", ":18790"]
