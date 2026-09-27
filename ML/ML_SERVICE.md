# ML Service

## Назначение

ML Service — отдельный вычислительный сервис для раннего прогнозирования отклонения городского транспорта от расписания.

Для транспортного средства в момент `T` сервис оценивает задержку на первой целевой остановке, плановое время которой попадает в окно:

```text
(T + 10 минут, T + 15 минут]
```

ML Service получает подготовленные данные от Backend по gRPC, поддерживает rolling history телеметрии, строит production-признаки и выполняет инференс ONNX-моделей.

```text
Backend
   |
   | Protobuf / gRPC
   v
ML Service
   |
   +-- rolling telemetry state
   +-- feature builder
   +-- residual regression
   +-- late-risk classification
   +-- reason engine
   |
   v
PredictionResponse
```

ML Service не принимает NDTP напрямую и не взаимодействует с Frontend.

## Зона ответственности

ML Service отвечает за:

- приём нормализованной телеметрии от Backend;
- rolling history по `tr_id`;
- расчёт production-признаков;
- прогноз изменения задержки;
- расчёт вероятности существенного опоздания;
- формирование `risk_level`;
- формирование объясняющей причины;
- измерение latency инференса;
- health counters;
- offline-генерацию `submission.csv` тем же production core.

Backend отвечает за NDTP parsing, `unit_id -> tr_id`, schedule matching, `cur_dev_s` и выбор target stop.

## Постановка задачи

На момент прогнозирования известны:

```text
tr_id
T
cur_dev_s
target_stop_id
target_time_begin
```

Целевая величина:

```text
target_delay_s
```

Задержка определяется как:

```text
delay = fact_time - plan_time
```

Положительное значение означает опоздание, отрицательное — опережение графика.

При обучении и инференсе соблюдается causal-ограничение:

```text
event_time <= T
```

Данные после момента формирования прогноза не используются.

## Горизонт

ML Service дополнительно проверяет target horizon:

```text
600 < horizon_s <= 900
```

То есть ровно 10 минут не входят в окно, а ровно 15 минут входят.

Если target не соответствует этому диапазону, обычный production-прогноз не формируется.

## Модель задержки

Основная модель использует residual formulation.

Вместо прямого прогнозирования будущей задержки модель оценивает её изменение относительно текущего отклонения:

```text
delta_delay = target_delay_s - cur_dev_s
```

Итог:

```text
predicted_delay_s = cur_dev_s + predicted_delta_s
```

Так модель отвечает на вопрос: сколько текущей задержки транспорт дополнительно накопит или сможет отыграть за следующие 10–15 минут.

Production-конфигурация:

```text
algorithm: LightGBM
problem: residual regression
features: 96
seed: 42
ensemble: no
fleet/grid context: no
runtime format: ONNX
```

Локальная метрика:

```text
MAE ≈ 57.3 сек
```

Платформенный score для сформированного submission:

```text
1.00000
```

Платформенная метрика ограничена сверху, поэтому `1.00000` не означает MAE = 0.

## Модель вероятности задержки

Отдельная LightGBM-модель оценивает вероятность существенного опоздания:

```text
P(late)
```

Она используется как дополнительная risk-head и не заменяет основной regression-прогноз.

Зафиксированные метрики:

```text
AUC     ≈ 0.9422
logloss ≈ 0.2615
Brier   ≈ 0.0822
```

На выходе формируются:

```text
late_probability
risk_level
```

Допустимые уровни риска:

```text
green
yellow
red
```

## Production-признаки

Вектор инференса содержит 96 признаков.

Основные группы:

- `cur_dev_s`;
- горизонт прогноза;
- лаги текущего отклонения;
- rolling statistics отклонения;
- rolling speed за 1/3/5/10/15 минут;
- stopped share;
- speed slopes;
- GPS displacement;
- path distance;
- telemetry staleness;
- координаты target stop;
- distance to target;
- heading to target;
- required speed;
- ETA при текущей скорости;
- временные признаки;
- schedule gap;
- stop order;
- last known fact delay;
- seconds since last fact.

Точный порядок признаков является частью модельного контракта и проверяется при старте через `model_manifest.json`.

## Rolling state

Телеметрия хранится в ограниченном rolling window.

```text
history window: 1 час
```

Сервис обновляет состояние на каждом принятом telemetry event, но не запускает prediction автоматически для каждого пакета.

```text
frequent telemetry events
        |
        v
rolling state
        |
        +---- Predict request from Backend
```

Это разделяет ingestion rate и inference rate и уменьшает лишнюю нагрузку.

## Обработка телеметрии

При ingestion сервис проверяет корректность ключевых полей:

- `tr_id`;
- timestamp;
- порядок событий;
- валидность GPS;
- координаты;
- скорость;
- heading.

Для duplicate/out-of-order событий предусмотрены отдельные статусы, чтобы некорректная последовательность не повреждала rolling state.

## Reason engine

Причина риска формируется детерминированно по рассчитанным признакам и текущему состоянию.

Примеры причин:

```text
недостаточно свежая телеметрия
длительный простой / посадка
резкое замедление движения
нарастающее отставание от графика
ожидаемое накопление задержки
ожидаемое сокращение текущего отклонения
устойчивое текущее отклонение
```

В ответе возвращается:

```text
reason_confidence
```

Генеративная модель для reason engine не используется.

## Runtime

Обучение и эксперименты выполняются в Python, production serving — в Go.

```text
Python training
      |
      v
LightGBM models
      |
      v
ONNX export
      |
      v
Go + ONNX Runtime
      |
      v
gRPC
```

Такое разделение уменьшает runtime overhead и упрощает интеграцию с Go Backend.

## Production-проект

Текущая директория сервиса:

```text
mosgortrans_ml_go/
```

Структура:

```text
mosgortrans_ml_go/
├── cmd/
│   └── ml/
│       └── main.go
├── internal/
│   ├── core/
│   ├── grpcapi/
│   ├── offline/
│   └── ort/
├── proto/
│   └── mosgortrans.proto
├── scripts/
│   ├── gen_proto.sh
│   └── fetch_onnxruntime.sh
├── third_party/
│   └── onnxruntime/
├── Dockerfile
├── go.mod
└── go.sum
```

## Модельные артефакты

Основные production-файлы:

```text
lgbm_residual_final_seed42.onnx
lgbm_late_final.onnx
model_manifest.json
```

Технический идентификатор, возвращаемый текущей сборкой через Health:

```text
final_top96_seed42_onnx_go_v1
```

Это version identifier конкретного набора артефактов, а не пользовательское название сервиса.

## gRPC API

Сервис:

```text
mosgortrans.v1.MLInference
```

Методы:

```text
Ingest
IngestStream
Predict
Health
```

### Ingest

Unary ingestion одного telemetry event вместе с `ScheduleState`.

Основные поля `TelemetryEvent`:

```text
unit_id
tr_id
event_time_unix_ms
lat
lon
speed_kmh
heading_deg
location_valid
```

### IngestStream

Client-streaming ingestion нескольких событий по одному gRPC stream.

### Predict

Принимает `tr_id`, время запроса, `ScheduleState` и `TargetPoint`.

Основные поля ответа:

```text
status
tr_id
target_stop_id
target_time_unix_ms
horizon_s
current_delay_s
predicted_delta_s
predicted_delay_s
late_probability
risk_level
reason
reason_confidence
telemetry_staleness_s
ml_latency_ms
```

### Health

Возвращает readiness и счётчики:

```text
ok
status
model_version
telemetry_events
predictions
```

## Производительность

Замеренный smoke latency Go + ONNX Runtime:

```text
≈ 0.536 ms
```

В полном replay pipeline также фиксировалась миллисекундная latency без накопления ingress backlog.

## Сборка

Сначала задаётся путь к ONNX Runtime:

```bash
cd mosgortrans_ml_go
export LD_LIBRARY_PATH="$PWD/third_party/onnxruntime/lib:${LD_LIBRARY_PATH:-}"
```

Тесты:

```bash
go test ./...
go vet ./...
```

Production build:

```bash
mkdir -p bin
CGO_ENABLED=1 go build \
  -tags onnxruntime \
  -o bin/mosgortrans-ml \
  ./cmd/ml
```

## Запуск

```bash
cd mosgortrans_ml_go
export LD_LIBRARY_PATH="$PWD/third_party/onnxruntime/lib:${LD_LIBRARY_PATH:-}"

./bin/mosgortrans-ml \
  --bind 0.0.0.0:50051 \
  --model-dir /path/to/onnx
```

Production gRPC port:

```text
50051
```

Сервер включает gRPC reflection, поэтому для ручной проверки можно использовать `grpcurl`.

## Health check

```bash
grpcurl -plaintext \
  127.0.0.1:50051 \
  mosgortrans.v1.MLInference/Health
```

Типовой ответ:

```json
{
  "ok": true,
  "status": "ready; rejected_events=0",
  "modelVersion": "final_top96_seed42_onnx_go_v1",
  "telemetryEvents": "32572",
  "predictions": "1630"
}
```

## Offline submission

Production binary поддерживает отдельный offline-режим:

```text
--generate-submission
```

Пример:

```bash
./bin/mosgortrans-ml \
  --bind 0.0.0.0:50051 \
  --model-dir /path/to/onnx \
  --generate-submission \
  --dataset-dir ../dataset \
  --submission-out ../dataset/submission.csv
```

По умолчанию offline-режим выключен и не влияет на live serving.

## Границы сервиса

ML Service не отвечает за:

- NDTP parsing;
- `unit_id -> tr_id` mapping;
- выбор активного рейса;
- вычисление `cur_dev_s` из расписания;
- выбор target stop;
- REST API;
- WebSocket для Frontend;
- выполнение сценарных действий диспетчера.

## Саммари

```text
normalized telemetry + schedule context
              |
              v
        rolling state
              |
              v
         96 features
              |
       +------+------+
       |             |
       v             v
 residual ONNX    risk ONNX
       |             |
       +------+------+
              |
              v
 PredictionResponse via gRPC
```
