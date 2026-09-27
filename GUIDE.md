# Инструкция для жюри

Ниже — короткий end-to-end сценарий проверки решения: live NDTP поток, прогнозы и алерты в дашборде, метрики и исторический прогон.

## 1. Подготовка NDTP-эмулятора

Исторический датасет и модели уже находятся в репозитории. Образ официального NDTP-эмулятора в Git не хранится.

Поместите выданный организаторами файл в:

```text
dataset/ndtp-telemetry-emulator.tar
```

и загрузите образ:

```bash
docker load -i dataset/ndtp-telemetry-emulator.tar
```

## 2. Запуск live-системы

Из корня репозитория:

```bash
chmod +x scripts/*.sh
./scripts/start_demo.sh
```

Скрипт поднимает Docker Compose стек:

```text
NDTP emulator -> Backend -> ML -> Backend REST/WebSocket -> BI dashboard
```

После запуска подождите 20–30 секунд для накопления телеметрии.

## 3. Где смотреть прогнозы, риски и алерты

Откройте BI-дашборд:

```text
http://localhost:8088/
```

На экране отображаются текущие позиции ТС, прогноз задержки, риск, активные инциденты, средняя ML latency и число fallback-прогнозов.

Статусы ТС:

- **Мониторинг** — телеметрия принимается, но для ТС нет schedule-backed прогнозного контура;
- **Нет цели 10–15 мин** — расписание есть, но в данный момент нет target stop в строгом горизонте;
- **В графике** — валидный прогноз и низкий риск;
- **Риск / Критический** — валидный прогноз с повышенным риском задержки;
- **Нет связи** — телеметрия устарела/отсутствует.

ТС в статусе «Мониторинг» не считаются ошибкой и не получают искусственный прогноз.

## 4. Автоматическая live-проверка

```bash
./scripts/smoke_test.sh
```

Smoke test проверяет Backend health, Swagger/OpenAPI, live fleet, lifecycle target/prediction, отсутствие stale predictions, What-if guardrails, non-mutating What-if и lifecycle активных incidents.

Ожидаемый итог:

```text
ALL AVAILABLE LIVE SMOKE TESTS PASSED
```

## 5. API и технические метрики

Swagger UI:

```text
http://localhost:8088/swagger/index.html
```

Backend health:

```text
http://localhost:8080/healthz
```

Live fleet:

```bash
curl -s http://localhost:8080/api/v1/fleet | jq
```

Если установлен `grpcurl`, состояние ML runtime и счетчики можно посмотреть так:

```bash
grpcurl -plaintext -d '{}' localhost:50051 mosgortrans.v1.MLInference/Health
```

При работающем потоке счетчик `telemetryEvents` растет; при наличии валидных целей растет `predictions`.

## 6. Исторический датасет

Для воспроизводимого исторического прогона используется уже включенный каталог `dataset/`:

```bash
./scripts/generate_submission.sh
```

Скрипт собирает тот же ML Docker image, запускает causal offline pipeline и создает:

```text
output/submission.csv
```

Для каждой validate-точки используются только события `event_time <= T`. После генерации автоматически проверяются структура файла, уникальность `sample_id` и конечность прогнозов.

## 7. What-if

Для ТС с валидным прогнозом доступны два scenario-only сценария:

- продление зеленой фазы;
- выпуск резервного ТС.

What-if не изменяет основной live ML-прогноз (`live_state_changed=false`). Для ТС без валидного прогноза Backend отклоняет сценарий.

## 8. Остановка

```bash
docker compose down
```
