# Документация решения

Эта папка — точка входа в Sphinx-документацию по архитектуре и production-коду. Go-пакеты дополнительно снабжены package/code comments, REST API описан OpenAPI/Swagger, а ML-интерфейс — protobuf/gRPC contract.

## Что открыть жюри

- Sphinx index: [`index.rst`](./index.rst)
- Архитектура: [`architecture.rst`](./architecture.rst)
- Backend: [`backend.rst`](./backend.rst)
- ML runtime: [`ml.rst`](./ml.rst)
- Frontend: [`frontend.rst`](./frontend.rst)
- API/OpenAPI/gRPC: [`api.rst`](./api.rst)
- Запуск и проверки: [`operations.rst`](./operations.rst)
- OpenAPI YAML: [`../backend/docs/swagger.yaml`](../backend/docs/swagger.yaml)
- OpenAPI JSON: [`../backend/docs/swagger.json`](../backend/docs/swagger.json)
- gRPC contract: [`../ml/GRPC_CONTRACT.md`](../ml/GRPC_CONTRACT.md)

После запуска Swagger UI доступен на:

```text
http://localhost:8088/swagger/index.html
```

## Сборка HTML Sphinx

```bash
python3 -m venv .venv-docs
source .venv-docs/bin/activate
pip install -r docs/requirements.txt
sphinx-build -b html docs docs/_build/html
```

После сборки открыть `docs/_build/html/index.html`.
