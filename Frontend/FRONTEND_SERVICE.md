# Frontend Service

## Назначение

Frontend Service — диспетчерский интерфейс системы раннего прогнозирования отклонений городского транспорта от расписания.

Его задача — в одном интерфейсе показать текущее состояние транспорта и прогноз на горизонте 10–15 минут:

- какие ТС сейчас находятся на линии;
- какие ТС требуют внимания;
- где находится выбранное ТС;
- каково текущее отклонение от графика;
- какая задержка ожидается на целевой остановке;
- какова вероятность опоздания;
- какой уровень риска определён системой;
- какая причина риска выявлена;
- какая целевая остановка используется для прогноза;
- какой результат даёт сценарный анализ.

Frontend не обращается к ML Service напрямую.

```text
Frontend Service
      |
      | REST / WebSocket
      v
Backend Service
      |
      | gRPC
      v
ML Service
```

Backend является источником live state.

## Зона ответственности

Frontend отвечает за:

- визуализацию текущего fleet state;
- отображение live-статуса соединения;
- список ТС;
- фильтрацию и поиск;
- отображение активных инцидентов;
- live-карту;
- drill-down выбранного ТС;
- отображение текущей и прогнозной задержки;
- отображение вероятности и уровня риска;
- отображение причины прогноза;
- запуск What-if сценариев через Backend REST API;
- отображение сценарного результата отдельно от live state;
- reconnect WebSocket;
- same-origin REST/WS работу в development и Docker.

Frontend не рассчитывает ML prediction и не изменяет прогноз локальными эвристиками.

## Стек

```text
React
TypeScript
Vite
MapLibre GL
WebSocket
REST
CSS
```

Основная структура:

```text
frontend/
├── src/
│   ├── App.tsx
│   ├── main.tsx
│   ├── styles.css
│   ├── api/
│   │   └── whatIf.ts
│   ├── hooks/
│   │   └── useFleetSocket.ts
│   ├── types/
│   │   └── fleet.ts
│   └── vite-env.d.ts
├── vite.config.ts
└── package.json
```

## Основные представления

Интерфейс разделён на два основных представления:

```text
Транспорт
Карта
```

Так список флота, инциденты и большая карта не конкурируют за одно рабочее пространство.

## Страница «Транспорт»

Страница предназначена для оперативного просмотра состояния флота и рисков.

Для каждого ТС Frontend использует данные Backend:

```text
unit_id
tr_id
is_online
speed
current_delay_s
predicted_delay_s
late_probability
risk_level
target_stop_id
target_time_begin
reason
reason_confidence
updated_at
```

В верхней части интерфейса отображаются:

```text
live connection status
горизонт прогноза 10-15 минут
сводное состояние флота
```

Ключевое различие:

```text
current_delay_s
= известное текущее отклонение

predicted_delay_s
= прогнозируемое отклонение на target stop через 10-15 минут
```

## Активные инциденты

Список активных проблем строится из актуального состояния флота:

```text
vehicles
-> risk_level == yellow || red
-> одна карточка на одно текущее проблемное ТС
```

Повторные `INCIDENT` events одного и того же ТС не создают дополнительные активные карточки.

Если ТС возвращается в `green`, оно исчезает из списка текущих инцидентов.

Историческая лента событий, если потребуется, должна храниться отдельно от current incidents.

## Страница «Карта»

Карта реализована на MapLibre GL.

На ней отображаются актуальные online-ТС.

Цвет маркера соответствует `risk_level`:

```text
green  -> штатное состояние
yellow -> повышенный риск
red    -> высокий риск
```

Offline или устаревшие объекты не показываются как актуальные.

Frontend использует координаты из текущего `VehicleState`.

## Фокус на выбранном ТС

При выборе транспортного средства интерфейс может перейти на карту и сфокусироваться на нём:

```text
selected vehicle
-> открыть карту
-> обеспечить видимость объекта
-> центрировать карту
-> flyTo / zoom
```

На карте показывается компактная карточка выбранного ТС с ключевой информацией.

Одновременно не требуется отображать несколько competing detail panels.

## Обновление MapLibre

MapLibre instance не пересоздаётся на каждом live update.

Схема:

```text
инициализация карты
-> при создании компонента / смене map style

новый fleet state
-> GeoJSON
-> source.setData(...)

изменение selected vehicle
-> flyTo(...)
```

Это сохраняет плавность интерфейса при частых обновлениях.

## Модель данных

Основной объект Frontend соответствует Backend fleet state.

```ts
interface VehicleState {
  unit_id: number;
  tr_id: number;
  updated_at: string;
  is_online: boolean;

  last_point: {
    unit_id: number;
    event_time: string;
    lat: number;
    lon: number;
    speed: number;
    heading: number;
    valid: boolean;
  };

  track: TelemetryPoint[];

  target_stop_id: number;
  target_time_begin: string;

  current_delay_s: number;
  predicted_delay_s: number;
  late_probability: number;

  risk_level: "green" | "yellow" | "red";

  reason: string;
  reason_confidence: number;
}
```

Поля prediction state не должны переименовываться независимо от Backend-контракта.

## WebSocket

Основной live-канал:

```text
/ws/fleet
```

Поддерживаемые типы событий:

```text
INIT
VEHICLE_UPDATE
INCIDENT
```

### INIT

Начальная синхронизация текущего fleet state.

Пример структуры:

```json
{
  "type": "INIT",
  "payload": {
    "vehicles": [],
    "incidents": []
  }
}
```

### VEHICLE_UPDATE

Обновляет состояние одного ТС. Если `unit_id` уже присутствует, объект заменяется актуальной версией; иначе добавляется.

### INCIDENT

Сигнализирует о проблемном состоянии. Для текущего списка инцидентов Frontend ориентируется на fleet state, а не на длину event history.

## WebSocket reconnect

После разрыва соединения Frontend автоматически пытается восстановить WebSocket.

Интерфейс явно показывает состояние соединения, чтобы отсутствие live-данных не выглядело как штатное обновление.

```text
LIVE
нет соединения
```

## REST API

REST используется для действий, инициированных пользователем.

Основные сценарные endpoints:

```http
POST /api/v1/what-if/reserve
POST /api/v1/what-if/traffic-light
```

В интерфейсе используются пользовательские названия:

```text
Резервное ТС
Приоритет светофора +15 с
```

API naming и UI copy не обязаны совпадать буквально.

## Сценарный анализ

What-if результат отображается отдельно от live ML prediction.

Основной принцип:

```text
scenario result != live state mutation
```

Frontend после сценарного запроса не изменяет локально:

```text
current_delay_s
predicted_delay_s
late_probability
risk_level
```

Он отображает отдельный блок сценарной оценки.

### Приоритет светофора

Frontend может показать:

```text
текущий прогноз
возможный выигрыш
оценку после меры
тип оценки
```

Результат обозначается как сценарная оценка, а не гарантированный эффект.

### Резервное ТС

Frontend разделяет:

```text
задержку исходного ТС
время готовности резерва
оценочный service gap
возможное сокращение разрыва
```

Выпуск резерва не должен визуально обнулять задержку исходного ТС.

### Ошибка сценарного запроса

Если Backend возвращает `HTTP 409 Conflict` из-за отсутствия валидного target в горизонте 10–15 минут, Frontend показывает ошибку сценарного расчёта и не генерирует результат самостоятельно.

## Поиск и фильтрация

Поиск для небольшого live fleet выполняется локально во Frontend.

Основные поля:

```text
unit_id
tr_id
reason
```

При переходе к выбранному ТС на карту интерфейс должен гарантировать его видимость независимо от предыдущего фильтра.

## Same-origin взаимодействие

Frontend использует same-origin API layer.

Development:

```text
browser -> Vite :5173
Vite proxy -> Backend :8080
```

Docker:

```text
browser
-> frontend/nginx
-> /api/* -> backend:8080
-> /ws/*  -> backend:8080
```

Браузер не обращается напрямую к внутреннему Docker hostname `backend`.

## Запуск

```bash
cd frontend
npm install
npm run dev
```

Development URL:

```text
http://localhost:5173/
```

## Production build

```bash
cd frontend
npm run build
```

Build должен проходить после изменений TypeScript, API layer или интерфейса.

## Источник истины

```text
live transport
-> Backend fleet state

prediction
-> ML response через Backend

current incidents
-> current vehicles with yellow/red risk

scenario result
-> Backend REST response

map coordinates
-> last_point актуального online VehicleState
```

## Границы сервиса

Frontend не должен:

- обращаться напрямую к ML Service;
- парсить NDTP;
- самостоятельно вычислять `cur_dev_s`;
- выбирать target stop;
- рассчитывать ML prediction;
- локально менять prediction после What-if;
- считать повторные `INCIDENT` events отдельными текущими проблемами;
- показывать offline-ТС как актуальные;
- рисовать синтетическую траекторию как гарантированную реальную геометрию маршрута.

## Саммари

```text
Backend REST / WebSocket
          |
          v
     Frontend state
       |       |
       |       +--> active incidents
       |
       +--> fleet / filters
       |
       +--> MapLibre
       |
       +--> scenario requests
```

Frontend показывает состояние системы и действия пользователя; расчёт транспортного состояния и прогнозов остаётся на Backend и ML Service.
