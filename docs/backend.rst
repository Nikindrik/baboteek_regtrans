Backend: пакеты и ответственность
=================================

Точка входа
-----------

``backend/cmd/server/main.go`` запускает приложение, HTTP/WebSocket слой и NDTP
TCP-сервер через пакет ``internal/app``.

Основные пакеты
---------------

``internal/ndtp``
  Чтение NPL/NPH framing, CRC16/Modbus и декодирование телематических ячеек,
  включая Nav00. ``server.go`` принимает TCP-соединения эмулятора и передает
  декодированную навигационную запись в application layer.

``internal/schedule``
  ``resolver.go`` сопоставляет ``unit_id`` и ``tr_id``. ``store.go`` и
  ``matcher.go`` загружают расписание, выравнивают demo-clock, вычисляют текущее
  отклонение и ищут первую остановку в строгом интервале ``(T+10m, T+15m]``.
  Поиск target не зависит от синтетического GPS cursor.

``internal/app``
  Оркестрирует основной поток: NDTP -> schedule match -> fleet update -> ML
  ingest/predict. Здесь также реализована cadence логика и немедленный запрос
  прогноза при появлении/смене target.

``internal/mlclient``
  gRPC-клиент к ML runtime. Методы: ``Health``, ``Ingest`` и ``Predict``.

``internal/state``
  Потокобезопасное live-состояние парка. Хранит последние точки, диагностические
  показатели, прогнозы и активные инциденты. Здесь проверяется идентичность
  target при применении ML-ответа и очищаются stale forecasts.

``internal/transport/http``
  REST endpoints и Swagger metadata. Основные ресурсы: fleet, incidents,
  health и два What-if сценария.

``internal/transport/ws``
  WebSocket hub для realtime обновлений dashboard.

``internal/whatif``
  Два сценария поддержки диспетчера: продление зеленой фазы и выпуск резервного
  ТС. Расчеты являются scenario-only и не изменяют live ML state.

Основные состояния ТС
---------------------

``forecast_eligible=false``
  ТС принимается и отображается, но его рейс не имеет расписания в текущем
  наборе данных. Пользовательский статус — «Мониторинг».

``forecast_eligible=true`` + ``prediction_valid=false``
  Расписание есть, но в текущий момент отсутствует валидная остановка в строгом
  горизонте 10–15 минут либо прогноз еще не получен.

``prediction_valid=true``
  Прогноз привязан к конкретной тройке ``tr_id``, ``target_stop_id`` и
  ``target_time_begin``.
