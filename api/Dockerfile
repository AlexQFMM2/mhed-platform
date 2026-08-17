FROM golang:1.24-alpine AS build

WORKDIR /src
COPY api/go.mod api/go.sum ./
RUN go mod download
COPY api/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mhed-api ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/mhed-api /mhed-api
EXPOSE 8080
ENTRYPOINT ["/mhed-api"]
