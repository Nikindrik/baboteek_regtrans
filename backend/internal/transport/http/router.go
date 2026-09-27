package http

import (
	"net/http"

	"baboteek_regtrans/internal/transport/ws"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

func NewRouter(handler *Handler, hub *ws.Hub) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/healthz", handler.Healthz)

	// Swagger UI
	r.Get("/swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/index.html", http.StatusMovedPermanently)
	})
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	// WebSocket для диспетчерского дашборда
	r.Get("/ws/fleet", hub.ServeWS)

	// REST API
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/fleet", handler.GetFleet)
		r.Get("/fleet/{unit_id}", handler.GetVehicle)
		r.Get("/incidents", handler.GetIncidents)
		r.Post("/what-if/traffic-light", handler.WhatIfTrafficLight)
		r.Post("/what-if/reserve", handler.WhatIfReserve)
	})

	return r
}
