# Backend <-> ML gRPC contract

Canonical file: `proto/mosgortrans.proto`

Service: `mosgortrans.v1.MLInference`

RPCs:

- `Ingest(IngestRequest) -> IngestResponse` — unary ingest; useful for debug and when per-event acknowledgement is required.
- `IngestStream(stream IngestRequest) -> IngestSummary` — long-lived client stream for decoded NDTP telemetry.
- `Predict(PredictRequest) -> PredictionResponse` — unary prediction. The complete ML output returns to Backend in this response.
- `Health(HealthRequest) -> HealthResponse` — readiness/counters.

## Final connection pattern

Backend opens one persistent gRPC `ClientConn` to ML. HTTP/2 multiplexing lets it keep an `IngestStream` open while issuing `Predict` RPCs concurrently over the same connection.

```text
NDTP -> Backend decode/scheduler
          |
          | gRPC IngestStream (continuous decoded telemetry)
          v
        ML Go + ONNX state
          ^
          | gRPC Predict request / PredictionResponse
          |
       Backend -> Frontend
```

There is no second reverse TCP connection. `PredictionResponse` travels back to Backend as the gRPC response on the same gRPC channel.

Recommended final behavior:

1. Backend creates one `grpc.ClientConn` to `ML_HOST:50051`.
2. Backend opens `IngestStream` and continuously `Send()`s decoded telemetry + `ScheduleState`.
3. Every 15-30 seconds per active vehicle (or on the required event), Backend calls unary `Predict` using the same `ClientConn`.
4. ML returns the complete `PredictionResponse` (`predicted_delay_s`, probability, risk, reason, latency, etc.).
5. On stream failure Backend reconnects and starts a fresh stream; do **not** replay a large stale backlog.
6. For initial integration/debug, unary `Ingest` may be used instead of `IngestStream` because it gives an acknowledgement for every event.

Wire fields and field numbers are the same as the original project contract. Only `option go_package` was added for Go code generation; it does not change protobuf wire compatibility.
