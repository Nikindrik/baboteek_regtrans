# Mosgortrans ML — Go + gRPC + ONNX Runtime

Production inference service. Backend отвечает за NDTP decoding, schedule matching, `cur_dev_s` и выбор строгого target в интервале `(T+10 min, T+15 min]`. ML-сервис отвечает за причинную rolling-историю телеметрии, фиксированный 96-feature builder и ONNX inference.

## Model artifacts

Репозиторий содержит необходимые production-артефакты в `models/`:

- `model_manifest.json`
- `lgbm_residual_final_seed42.onnx`
- `lgbm_late_final.onnx`

Manifest фиксирует порядок 96 признаков и метаданные моделей; при наличии hashes сервис проверяет SHA-256.

## Docker / основной запуск

Рекомендуемый способ запуска всей системы — из корня репозитория:

```bash
./scripts/start_demo.sh
```

Compose монтирует `./models` в ML container read-only и поднимает gRPC сервис на `:50051`.

## Локальная сборка ML

```bash
./scripts/fetch_onnxruntime.sh
export LD_LIBRARY_PATH="$PWD/third_party/onnxruntime/lib:${LD_LIBRARY_PATH:-}"
go test ./...
go vet ./...
CGO_ENABLED=1 go build -tags onnxruntime -o bin/mosgortrans-ml ./cmd/ml
```

## gRPC smoke test

Server reflection включен:

```bash
grpcurl -plaintext 127.0.0.1:50051 list
grpcurl -plaintext -d '{}' 127.0.0.1:50051 mosgortrans.v1.MLInference/Health
```

`Health.telemetryEvents` растет при поступлении телеметрии; `Health.predictions` — когда Backend передает валидный target.

## Offline historical submission

Для жюри рекомендуется готовый wrapper из корня репозитория:

```bash
./scripts/generate_submission.sh
```

Он использует тот же production feature builder и ONNX runners, ingest-ит только события `event_time <= T`, запускает ML с флагами `--generate-submission --submission-only` и сохраняет результат в:

```text
output/submission.csv
```

Writer обеспечивает формат `sample_id;prediction`, полное покрытие и отсутствие пропущенных predictions.

## gRPC contract

См. [`GRPC_CONTRACT.md`](./GRPC_CONTRACT.md) и [`proto/mosgortrans.proto`](./proto/mosgortrans.proto).
