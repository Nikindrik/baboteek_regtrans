API, OpenAPI и gRPC
===================

REST API
--------

OpenAPI спецификация уже хранится в репозитории:

* ``backend/docs/swagger.yaml``
* ``backend/docs/swagger.json``

После запуска Swagger UI доступен по адресу
``http://localhost:8088/swagger/index.html``.

Основные endpoints
------------------

``GET /healthz``
  Health check Backend.

``GET /api/v1/fleet``
  Полный live snapshot парка.

``GET /api/v1/fleet/{unit_id}``
  Состояние одного ТС и краткий breadcrumb track.

``GET /api/v1/incidents``
  Текущие активные incidents по валидным yellow/red прогнозам.

``POST /api/v1/what-if/traffic-light``
  Scenario-only оценка эффекта продления зеленой фазы. Не изменяет live state.

``POST /api/v1/what-if/reserve``
  Scenario-only оценка эффекта выпуска резервного ТС. Не изменяет live state.

WebSocket
---------

Backend публикует realtime обновления fleet state через WebSocket; nginx
проксирует соединение для dashboard.

gRPC ML API
-----------

Контракт хранится в ``ml/proto/mosgortrans.proto`` и дублируется в
``ml/GRPC_CONTRACT.md``. Сервис: ``mosgortrans.v1.MLInference``.

Основные RPC:

* ``Ingest`` / ``IngestStream`` — передача причинной телеметрии;
* ``Predict`` — прогноз для уже выбранной строгой target stop;
* ``Health`` — model/runtime status и счетчики telemetry/predictions.

Server reflection включен, поэтому при наличии ``grpcurl`` можно выполнить::

   grpcurl -plaintext 127.0.0.1:50051 list
   grpcurl -plaintext -d '{}' 127.0.0.1:50051 mosgortrans.v1.MLInference/Health
