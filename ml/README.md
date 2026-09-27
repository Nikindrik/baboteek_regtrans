# Mosgortrans ML — Go + gRPC + ONNX Runtime

Production inference service. Backend owns NDTP decoding, schedule matching, `cur_dev_s` and selection of the strict `(T+10 min, T+15 min]` target. This service owns rolling telemetry state, the frozen 96-feature builder and ONNX inference.

## Required model artifacts

Runtime requires three files in one model directory:

- `model_manifest.json`
- `lgbm_residual_final_seed42.onnx`
- `lgbm_late_final.onnx`

The uploaded source archive did **not** contain these three files. Do not start the final demo until they are copied from the trained-artifact directory into the final bundle's `models/` directory. The manifest is validated against the exact 96-feature order and, when present, model SHA-256 values.

## Local build

```bash
./scripts/fetch_onnxruntime.sh   # normally only refreshes local linker aliases
export LD_LIBRARY_PATH="$PWD/third_party/onnxruntime/lib:${LD_LIBRARY_PATH:-}"
go test ./...
go vet ./...
CGO_ENABLED=1 go build -tags onnxruntime -o bin/mosgortrans-ml ./cmd/ml
```

## Run

```bash
export LD_LIBRARY_PATH="$PWD/third_party/onnxruntime/lib:${LD_LIBRARY_PATH:-}"
./bin/mosgortrans-ml \
  --bind 0.0.0.0:50051 \
  --model-dir /path/to/models
```

Expected model version: `final_top96_seed42_onnx_go_v1`.

## gRPC smoke test

Server reflection is enabled:

```bash
grpcurl -plaintext 127.0.0.1:50051 list
grpcurl -plaintext -d '{}' 127.0.0.1:50051 mosgortrans.v1.MLInference/Health
```

`Health.telemetryEvents` must grow when Backend ingests data; `Health.predictions` must grow when a valid strict-horizon target is available.

## Offline `submission.csv`

The same frozen feature builder / ONNX runners can generate the validate submission. Only `event_time <= T` is ingested.

```bash
./bin/mosgortrans-ml \
  --bind 0.0.0.0:50051 \
  --model-dir /path/to/models \
  --generate-submission \
  --dataset-dir /path/to/dataset \
  --submission-out /path/to/submission.csv
```

The writer enforces `sample_id;prediction`, full coverage and no missing predictions.

## Docker

The image expects the model directory at `/models` (the root Compose file mounts it read-only). Generated protobuf sources and ONNX Runtime are bundled; they are not downloaded again during a normal source build except for Go modules.

See `GRPC_CONTRACT.md` for the wire contract.
