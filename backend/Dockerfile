FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/mosgortrans-backend ./cmd/server

FROM debian:bookworm-slim
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/mosgortrans-backend /app/mosgortrans-backend
COPY data/ /app/data/
EXPOSE 8080 9201
ENTRYPOINT ["/app/mosgortrans-backend"]
