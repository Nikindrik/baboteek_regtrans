FROM golang:1.23-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Generated protobuf sources are committed. The bundle also carries ORT itself,
# so a normal build does not need protoc or an ORT download.
RUN chmod +x scripts/fetch_onnxruntime.sh && ./scripts/fetch_onnxruntime.sh
ENV LD_LIBRARY_PATH=/src/third_party/onnxruntime/lib
RUN CGO_ENABLED=1 go build -tags onnxruntime -o /out/mosgortrans-ml ./cmd/ml

FROM debian:bookworm-slim
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libgomp1 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/mosgortrans-ml /app/mosgortrans-ml
COPY --from=build /src/third_party/onnxruntime/lib/ /app/lib/
ENV LD_LIBRARY_PATH=/app/lib
EXPOSE 50051
ENTRYPOINT ["/app/mosgortrans-ml"]
CMD ["--bind", "0.0.0.0:50051", "--model-dir", "/models"]
