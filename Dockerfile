# One image per service: docker build --build-arg SERVICE=gateway .
# Builds natively for the host (arm64 on Apple silicon / Graviton, amd64 elsewhere);
# use `docker buildx build --platform linux/amd64,linux/arm64` for multi-arch.
FROM golang:1.24-alpine AS build
ARG SERVICE
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
# go.sum is generated here if it hasn't been committed yet (see README).
RUN [ -f go.sum ] && grep -q . go.sum || go mod tidy
RUN test -n "$SERVICE" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
