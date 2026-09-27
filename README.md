# Mosgortrans — предиктор изменений в графике движения транспорта

Решение для раннего прогнозирования отклонений городского транспорта от расписания в строгом горизонте **(T+10 мин, T+15 мин]**. Система принимает поток телеметрии NDTP, сопоставляет транспортное средство с расписанием, строит ML-прогноз задержки и риска опоздания и отображает результат в диспетчерском BI-дашборде.

## Архитектура

```text
NDTP emulator -> Backend (Go) -> ML runtime (Go + ONNX) -> Backend state -> React BI dashboard
     :9201           :8080             :50051                          :8088
```

Система состоит из трех основных модулей:

- **Backend** — прием и декодирование NDTP, сопоставление `unit_id -> tr_id`, работа с расписанием, выбор целевой остановки в горизонте 10–15 минут, REST/WebSocket, incidents и What-if.
- **ML** — stateful обработка причинной истории телеметрии, 96 production-признаков, ONNX inference регрессии задержки и классификатора риска.
- **BI-дашборд** — карта ТС, прогнозы, статусы, риски, инциденты, диагностические показатели и What-if сценарии.

Frontend не обращается к ML напрямую: Backend является единственным источником live-состояния.

## Быстрый запуск

Требования: Docker с Docker Compose, `curl`, `jq`. Репозиторий уже содержит исторический датасет и модельные артефакты в `dataset/` и `models/`.

Образ официального NDTP-эмулятора не хранится в Git. Поместите выданный организаторами файл:

```text
dataset/ndtp-telemetry-emulator.tar
```

и загрузите его в Docker:

```bash
docker load -i dataset/ndtp-telemetry-emulator.tar
```

После этого из корня репозитория:

```bash
chmod +x scripts/*.sh
./scripts/start_demo.sh
```

Скрипт собирает и запускает Docker Compose стек, выставляет demo-time shift для исторического расписания и загружает конфигурацию эмулятора. Решение корректно принимает весь поток из 56 ТС; транспорт без доступного расписания отображается в статусе **«Мониторинг»** и не получает искусственный прогноз.

Через 20–30 секунд выполните:

```bash
./scripts/smoke_test.sh
```

Ожидаемый итог:

```text
ALL AVAILABLE LIVE SMOKE TESTS PASSED
```

## Куда смотреть после запуска

- BI-дашборд: `http://localhost:8088/`
- Swagger UI: `http://localhost:8088/swagger/index.html`
- Backend health: `http://localhost:8080/healthz`
- Backend fleet API: `http://localhost:8080/api/v1/fleet`
- Emulator API: `http://localhost:18080/api/config`
- ML gRPC: `localhost:50051`

На дашборде используются статусы **«Мониторинг»**, **«Нет цели 10–15 мин»**, **«В графике»**, **«Риск»**, **«Критический»**, **«Нет связи»**. Цветовой риск отображается только при валидном текущем ML-прогнозе.

## Исторический прогон и submission.csv

Для воспроизводимого прогона на историческом validate-наборе используется тот же production feature builder и те же ONNX-модели:

```bash
./scripts/generate_submission.sh
```

Результат:

```text
output/submission.csv
```

Offline pipeline для каждой контрольной точки использует только телеметрию с `event_time <= T`, то есть будущие данные не попадают в признаки. Скрипт дополнительно проверяет формат, уникальность `sample_id` и конечность прогнозов.

## Ключевые правила прогнозирования

- целевая остановка — первая плановая остановка в интервале **(T+10 мин, T+15 мин]**;
- ТС без расписания остается в мониторинге и не получает выдуманный target/prediction;
- старый прогноз инвалидируется при исчезновении или смене target;
- запоздавший ML-ответ для предыдущего target не может перезаписать новый;
- `current_delay_s` и `predicted_delay_s` — разные показатели;
- fallback, если он используется, явно маркируется как `prediction_source=fallback`;
- What-if доступен только при валидном прогнозе и не изменяет live ML state.

## Документация

- инструкция для жюри: [`JURY_GUIDE.md`](./JURY_GUIDE.md)
- Sphinx-документация: [`docs/README.md`](./docs/README.md)
- архитектура: [`docs/architecture.rst`](./docs/architecture.rst)
- Backend: [`docs/backend.rst`](./docs/backend.rst)
- ML runtime: [`docs/ml.rst`](./docs/ml.rst)
- BI-дашборд: [`docs/frontend.rst`](./docs/frontend.rst)
- API / OpenAPI / gRPC: [`docs/api.rst`](./docs/api.rst)
- эксплуатация и проверки: [`docs/operations.rst`](./docs/operations.rst)
- OpenAPI YAML: [`backend/docs/swagger.yaml`](./backend/docs/swagger.yaml)
- gRPC contract: [`ml/GRPC_CONTRACT.md`](./ml/GRPC_CONTRACT.md)

## Остановка

```bash
docker compose down
```
