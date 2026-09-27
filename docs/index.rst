Mosgortrans Delay Predictor
===========================

Документация рабочего решения хакатона по прогнозированию изменений графика
городского транспорта. Система состоит из трех прикладных модулей: Backend,
ML-runtime и BI-дашборд, которые запускаются в Docker Compose. Официальный NDTP
эмулятор используется как источник live-телеметрии.

.. toctree::
   :maxdepth: 2
   :caption: Содержание

   architecture
   backend
   ml
   frontend
   api
   operations

Ключевая семантика
------------------

* Прогноз строится для первой плановой остановки в строгом горизонте
  ``(T+10 мин, T+15 мин]``.
* Для прогноза используются только данные, доступные к моменту ``T``.
* ТС без расписания принимаются и отображаются в режиме мониторинга, но для них
  не создаются искусственные остановки и прогнозы.
* Backend является единственной точкой истины для live-состояния. Frontend не
  обращается к ML-сервису напрямую.
* ``prediction_source=ml`` означает реальный ONNX-прогноз; fallback явно
  маркируется отдельно.

Быстрые ссылки
--------------

* REST/OpenAPI: ``backend/docs/swagger.yaml`` и ``backend/docs/swagger.json``.
* Swagger UI после запуска: ``http://localhost:8088/swagger/index.html``.
* Backend health: ``http://localhost:8080/healthz``.
* Dashboard: ``http://localhost:8088/``.
* gRPC contract: ``ml/GRPC_CONTRACT.md`` и ``ml/proto/mosgortrans.proto``.
