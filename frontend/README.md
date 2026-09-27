# Mosgortrans dispatcher dashboard

React + TypeScript + Vite + MapLibre dashboard backed by the real Go Backend. There is no production mock-server path and no local mutation of ML state.

## Local development

```bash
npm ci
npm run dev
```

Vite proxies `/api/*` and `/ws/*` to `http://localhost:8080`, so the browser uses same-origin URLs.

## UI semantics

- Green/yellow/red are shown only for a valid current forecast.
- `Контекст` means telemetry-only vehicle with no schedule; prediction is intentionally not required.
- `Нет цели 10–15 мин` means the vehicle has a schedule but currently has no target stop in the strict `(T+10,T+15]` window.
- `Резервный прогноз` is shown only when Backend is in transparent degraded fallback mode.
- Active incidents are derived from current vehicle state, not from an ever-growing WebSocket event history.
- What-if calls real Backend endpoints and never changes the live ML prediction.

## Production container

Nginx serves the built SPA and reverse-proxies `/api`, `/ws` and `/swagger` to the `backend` Compose service.

MapLibre styles/tiles currently come from OpenFreeMap. Internet access is therefore required for the basemap; fleet data and dashboard controls themselves are served locally.
