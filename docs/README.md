# Документация решения

Эта папка — точка входа для **Sphinx-документации по коду и архитектуре**. REST API дополнительно документирован в OpenAPI/Swagger.

## Что открыть жюри

- Sphinx source: [`index.rst`](./index.rst)
- Архитектура: [`architecture.rst`](./architecture.rst)
- Backend: [`backend.rst`](./backend.rst)
- ML runtime: [`ml.rst`](./ml.rst)
- Frontend: [`frontend.rst`](./frontend.rst)
- API/OpenAPI/gRPC: [`api.rst`](./api.rst)
- Запуск и проверки: [`operations.rst`](./operations.rst)
- OpenAPI YAML: [`../backend/docs/swagger.yaml`](../backend/docs/swagger.yaml)
- OpenAPI JSON: [`../backend/docs/swagger.json`](../backend/docs/swagger.json)

После запуска системы Swagger UI доступен на `http://localhost:8088/swagger/index.html`.

## Собрать HTML Sphinx

```bash
python3 -m venv .venv-docs
source .venv-docs/bin/activate
pip install -r docs/requirements.txt
sphinx-build -b html docs docs/_build/html
```

Затем открыть `docs/_build/html/index.html`.

Документация намеренно описывает production-код как Go/ONNX runtime. PyDoc не используется, потому что исполняемые Backend и ML-модули написаны на Go; требование закрывается Sphinx-документацией и OpenAPI/Swagger.
