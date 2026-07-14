# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/manager ./cmd/manager
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/runner ./cmd/runner

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app

COPY --from=build /out/manager /app/manager
COPY --from=build /out/runner /app/runner

USER nonroot:nonroot

ENTRYPOINT ["/app/manager"]
