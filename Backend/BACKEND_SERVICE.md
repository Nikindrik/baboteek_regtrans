# Backend Service

## Назначение

Backend Service — центральный оркестрационный модуль системы раннего прогнозирования отклонений городского транспорта от расписания.

Сервис принимает поток NDTP-телеметрии, связывает транспорт с расписанием, поддерживает текущее состояние рейсов, формирует прогнозную точку на горизонте 10–15 минут, вызывает ML Service по gRPC и публикует результат для Frontend через REST и WebSocket.

```text
NDTP Emulator / telemetry source
            |
            | TCP / NDTP
            v
      Backend Service
       |    |      |
       |    |      +--> REST / WebSocket --> Frontend
       |    |
       |    +--> schedule state / fleet state
       |
       +--> gRPC --> ML Service
```

Backend является источником истины для live-состояния транспорта, текущих прогнозов и активных инцидентов.

## Зона ответственности

Backend отвечает за:

- приём TCP-соединений от NDTP-источника;
- разбор бинарных NDTP-пакетов;
- нормализацию телеметрии;
- сопоставление `unit_id` с `tr_id`;
- загрузку и индексирование расписания;
- stateful matching движения ТС с последовательностью остановок;
- расчёт текущего отклонения от графика `cur_dev_s`;
- выбор целевой остановки в строгом горизонте 10–15 минут;
- передачу телеметрии и schedule context в ML Service;
- периодический запрос прогноза;
- хранение актуального fleet state;
- REST API;
- WebSocket live updates;
- Swagger/OpenAPI;
- сценарные What-if расчёты;
- обработку временной недоступности ML без падения всего сервиса.

Backend не выполняет ML-инференс самостоятельно и не должен дублировать модельную логику ML Service.

## Основной поток данных

```text
NDTP packet
    |
    v
NDTP parser
    |
    v
normalized telemetry
    |
    +--> unit_id -> tr_id
    |
    v
stateful schedule matcher
    |
    +--> current schedule state
    +--> cur_dev_s
    +--> target stop
    |
    v
gRPC request to ML Service
    |
    v
PredictionResponse
    |
    v
fleet state
    |
    +--> REST
    +--> WebSocket
    v
Frontend
```

## NDTP ingress

Backend принимает телеметрию по TCP.

Локальный порт:

```text
9201
```

NDTP-источник выполняет handshake и затем отправляет realtime-пакеты. Для каждого устройства `unitId` передаётся в поле `peerAddress` протокола.

Из навигационной телеметрии используются, в частности:

```text
unit_id
event_time
lat
lon
speed
heading
location_valid
```

Невалидные координаты не должны повреждать текущее состояние рейса.

## Сопоставление `unit_id` и `tr_id`

NDTP идентифицирует бортовое устройство через `unit_id`, а расписание и ML-контракт используют `tr_id`.

Backend поддерживает resolver:

```text
unit_id -> tr_id
```

Неизвестный `unit_id` не должен автоматически преобразовываться в произвольный `tr_id`. Такое событие должно быть отмечено как unresolved и исключено из обычного prediction flow до появления корректного сопоставления.

## Stateful schedule matching

Для каждого активного `tr_id` Backend поддерживает состояние движения по расписанию.

Типовой state включает:

```text
active run
last confirmed stop
next expected stop
last fact time
last plan time
current deviation
match confidence
previous telemetry point
approach / closest-approach state
```

Подтверждение прохождения остановки строится не по одному расстоянию до точки, а по совокупности сигналов:

- последовательность остановок;
- ожидаемая следующая остановка;
- приближение к остановке;
- минимальная дистанция;
- удаление после closest approach;
- направление движения;
- низкая скорость или dwell рядом с остановкой.

После подтверждения прохождения остановки текущее отклонение рассчитывается как:

```text
cur_dev_s = actual_passage_time - planned_passage_time
```

Если новых подтверждённых фактов временно нет, сервис сохраняет последнее известное значение и может снижать `match_confidence`.

## Горизонт прогноза

Для момента прогноза `T` Backend выбирает первую остановку, плановое время которой удовлетворяет условию:

```text
T + 10 минут < target_time <= T + 15 минут
```

В секундах:

```text
600 < horizon_s <= 900
```

Если подходящей остановки нет, target считается невалидным. Backend не подменяет её ближайшей или последней остановкой вне официального окна.

## Интеграция с ML Service

Backend взаимодействует с ML Service по Protobuf/gRPC.

```text
Backend --> gRPC --> ML Service
```

Основные RPC:

```text
Ingest
IngestStream
Predict
Health
```

### Ingest

Передаёт в ML нормализованную телеметрию и текущее состояние расписания.

### Predict

Передаёт:

```text
tr_id
request time
ScheduleState
TargetPoint
```

и получает:

```text
predicted_delay_s
late_probability
risk_level
reason
reason_confidence
ml_latency_ms
```

Prediction не требуется выполнять на каждый NDTP-пакет. Телеметрия может поступать часто, а прогноз запрашиваться с отдельной периодичностью.

## Fleet state

Backend хранит актуальное состояние каждого ТС, доступное Frontend.

Ключевые поля:

```text
unit_id
tr_id
updated_at
is_online
last_point
track
target_stop_id
target_time_begin
current_delay_s
predicted_delay_s
late_probability
risk_level
reason
reason_confidence
```

Frontend использует это состояние как источник live-данных и не должен самостоятельно пересчитывать прогнозные поля.

## REST API

HTTP-сервис работает на порту:

```text
8080
```

Основные endpoints:

```http
GET  /healthz
GET  /api/v1/fleet
POST /api/v1/what-if/reserve
POST /api/v1/what-if/traffic-light
```

Swagger/OpenAPI доступен через:

```text
/swagger/index.html
```

Полный список REST-методов определяется актуальной OpenAPI-спецификацией Backend.

## WebSocket

Live-канал для Frontend:

```text
/ws/fleet
```

Основные типы событий:

```text
INIT
VEHICLE_UPDATE
INCIDENT
```

### INIT

Передаёт начальный снимок состояния флота.

### VEHICLE_UPDATE

Передаёт новое состояние одного ТС.

### INCIDENT

Сигнализирует о проблемном состоянии. Актуальный список активных проблем формируется по текущему fleet state, а не по количеству накопленных событий.

## What-if API

Backend поддерживает сценарные оценки возможных мер.

### Резервное ТС

```http
POST /api/v1/what-if/reserve
```

Сценарий оценивает возможное уменьшение разрыва обслуживания при заданном времени готовности резерва.

Он не обнуляет фактическую задержку исходного ТС.

### Приоритет светофора

```http
POST /api/v1/what-if/traffic-light
```

Сценарий оценивает возможный выигрыш при увеличении доступного времени движения.

Результат является сценарной оценкой и не интерпретируется как доказанный causal effect.

### Общий принцип

```text
What-if result != mutation of live prediction
```

Сценарный ответ возвращается отдельно и не изменяет текущее ML-состояние транспорта.

Если для ТС нет валидного прогноза в официальном горизонте, Backend может вернуть:

```text
HTTP 409 Conflict
```

## Health

Проверка Backend:

```bash
curl http://127.0.0.1:8080/healthz
```

Типовой ответ:

```json
{
  "mode": "in-memory",
  "status": "healthy"
}
```

## Конфигурация ML-подключения

Локальный запуск:

```env
ML_GRPC_HOST=127.0.0.1
ML_GRPC_PORT=50051
```

В Docker Compose Backend должен обращаться к ML по имени сервиса, например:

```env
ML_GRPC_HOST=ml
ML_GRPC_PORT=50051
```

## Demo time shift

Для демонстрации live synthetic telemetry вместе с историческим расписанием может использоваться временной сдвиг:

```env
TIME_SHIFT_HOURS=-12
```

Это demo/test настройка, а не часть алгоритма прогнозирования. В production-контуре с согласованными временными шкалами параметр должен быть равен нулю или не использоваться.

## Запуск

Текущая директория Backend:

```text
hahakaton/
```

Development run:

```bash
cd hahakaton
go run ./cmd/server
```

Сборка бинарника:

```bash
cd hahakaton
mkdir -p bin
go build -o bin/mosgortrans-backend ./cmd/server
./bin/mosgortrans-backend
```

Ожидаемые порты после старта:

```text
8080  HTTP / REST / WebSocket
9201  NDTP TCP ingress
```

## Docker networking

В контейнерной схеме:

```text
NDTP Emulator
     |
     | TCP
     v
Backend
     |
     | gRPC
     v
ML Service

Frontend / nginx
     |
     | /api + /ws
     v
Backend
```

Внутри Docker-сети используются service names, а не `127.0.0.1` между контейнерами.

Браузер Frontend не должен обращаться к внутреннему Docker hostname Backend напрямую; REST и WebSocket проксируются через frontend/nginx.

## Надёжность

Backend рассчитан на непрерывный поток и должен сохранять работоспособность при частичных сбоях.

Основные механизмы:

- reconnect NDTP-источника после восстановления TCP-сервера;
- сохранение последнего известного fleet state;
- отдельная проверка доступности ML;
- отсутствие подмены невалидного target корректным прогнозом;
- обработка duplicate/out-of-order telemetry;
- ограничение live state текущими объектами;
- WebSocket reconnect на стороне Frontend.

При недоступности ML degraded mode должен быть явно отличим от реального ML inference.

## Границы сервиса

Backend не должен:

- обучать модели;
- выполнять ONNX inference;
- изменять прогноз во Frontend локальными эвристиками;
- выбирать target вне `(T+10m, T+15m]`;
- создавать произвольный `tr_id` для неизвестного `unit_id`;
- выдавать сценарный What-if результат за фактическое изменение live state.

## Саммари

Backend Service связывает транспортный поток, расписание, ML и интерфейс в один real-time контур:

```text
NDTP
 -> parsing
 -> schedule matching
 -> current deviation
 -> target 10-15 min
 -> ML gRPC
 -> fleet state
 -> REST / WebSocket
 -> Frontend
```
