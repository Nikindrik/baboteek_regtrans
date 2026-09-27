ML runtime
==========

Точка входа
-----------

``ml/cmd/ml/main.go`` запускает gRPC сервис ``mosgortrans.v1.MLInference`` и
загружает модельные артефакты из ``--model-dir``.

Обязательные артефакты
----------------------

* ``model_manifest.json``
* ``lgbm_residual_final_seed42.onnx``
* ``lgbm_late_final.onnx``

Manifest фиксирует порядок признаков и метаданные моделей. При старте сервис
проверяет наличие файлов и, если hashes указаны в manifest, их SHA-256.

``internal/core``
-----------------

Пакет содержит production feature builder и stateful inference logic.
``VehicleHistory`` хранит причинную телеметрию, ``PointHistory`` — snapshots
текущего отклонения. Вектор ``ProductionFeatures`` имеет фиксированный порядок и
передается в ONNX runners без динамической перестановки.

Прогнозирование
---------------

``Predict`` принимает уже выбранную Backend-ом schedule target. ML проверяет
валидность горизонта и формирует:

* ``predicted_delay_s`` — прогноз будущего отклонения;
* ``late_probability`` — вероятность опоздания;
* ``risk_level`` — green/yellow/red;
* текстовую причину/диагностический confidence для UI.

ONNX Runtime
------------

``internal/ort`` реализует CGO binding к ONNX Runtime. Docker image содержит
runtime libraries и собирает бинарник с build tag ``onnxruntime``.

Offline historical path
-----------------------

``internal/offline/submission.go`` использует тот же feature builder и те же
ONNX runners, что и live gRPC. Для каждой validate-точки сначала ingest-ятся
только строки с ``event_time <= T``; будущая телеметрия не используется.

Флаг ``--generate-submission`` строит ``submission.csv`` перед стартом gRPC.
Дополнительный флаг ``--submission-only`` позволяет после генерации завершить
процесс, что удобно для проверки исторического датасета и CI.
