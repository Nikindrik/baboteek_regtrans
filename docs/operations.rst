Запуск, проверка и документация
==============================

Live demo
---------

Из корня репозитория::

   ./scripts/start_demo.sh
   sleep 20
   ./scripts/smoke_test.sh

Dashboard: ``http://localhost:8088/``.

Swagger: ``http://localhost:8088/swagger/index.html``.

Historical submission
---------------------

Если полный исторический датасет находится в ``./dataset``, а модели — в
``./models``, submission можно построить одной командой::

   ./scripts/generate_submission.sh

По умолчанию файл создается как ``./output/submission.csv``. Скрипт запускает
тот же Docker image ML-runtime и вызывает causal offline pipeline, использующий
те же production features и ONNX models.

Smoke test
----------

``scripts/smoke_test.sh`` проверяет:

* health Backend;
* доступность OpenAPI/Swagger;
* live fleet;
* invariants context/target/prediction lifecycle;
* запрет What-if без валидного прогноза;
* non-mutating What-if;
* отсутствие stale/deduplicated incident ошибок.

Сборка Sphinx
-------------

Вариант через локальный Python::

   python3 -m venv .venv-docs
   . .venv-docs/bin/activate
   pip install -r docs/requirements.txt
   sphinx-build -b html docs docs/_build/html

После сборки открыть ``docs/_build/html/index.html``.
