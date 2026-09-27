package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"baboteek_regtrans/internal/domain"
	"baboteek_regtrans/internal/whatif"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

type Handler struct {
	repo   domain.FleetRepository
	whatIf *whatif.Service
	logger zerolog.Logger
}

func NewHandler(repo domain.FleetRepository, logger zerolog.Logger) *Handler {
	return &Handler{
		repo:   repo,
		whatIf: whatif.NewService(repo),
		logger: logger.With().Str("component", "http-handler").Logger(),
	}
}

// Healthz godoc
// @Summary Health check
// @Description Returns the service health status
// @Tags system
// @Produce json
// @Success 200 {object} map[string]string
// @Router /healthz [get]
func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
		"mode":   "in-memory",
	})
}

// GetFleet godoc
// @Summary Get all vehicles
// @Description Returns the live state of all vehicles in the network
// @Tags fleet
// @Produce json
// @Success 200 {array} domain.VehicleState
// @Router /api/v1/fleet [get]
func (h *Handler) GetFleet(w http.ResponseWriter, r *http.Request) {
	vehicles := h.repo.GetAllVehiclesSnapshot()
	h.writeJSON(w, http.StatusOK, vehicles)
}

// GetIncidents godoc
// @Summary Get active incidents
// @Description Returns the list of detected schedule deviation incidents
// @Tags incidents
// @Produce json
// @Success 200 {array} domain.IncidentCard
// @Router /api/v1/incidents [get]
func (h *Handler) GetIncidents(w http.ResponseWriter, r *http.Request) {
	incidents := h.repo.GetRecentIncidents()
	h.writeJSON(w, http.StatusOK, incidents)
}

// GetVehicle godoc
// @Summary Get vehicle by unit_id
// @Description Returns details for a single vehicle including its 10-point breadcrumb track
// @Tags fleet
// @Param unit_id path int true "Unit ID"
// @Produce json
// @Success 200 {object} domain.VehicleState
// @Failure 404 {object} map[string]string
// @Router /api/v1/fleet/{unit_id} [get]
func (h *Handler) GetVehicle(w http.ResponseWriter, r *http.Request) {
	unitIDStr := chi.URLParam(r, "unit_id")
	unitID, err := strconv.ParseUint(unitIDStr, 10, 32)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid unit_id"})
		return
	}

	vehicle, ok := h.repo.GetVehicleSnapshot(uint32(unitID))
	if !ok {
		h.writeJSON(w, http.StatusNotFound, map[string]string{"error": "vehicle not found"})
		return
	}

	h.writeJSON(w, http.StatusOK, vehicle)
}

// WhatIfTrafficLight godoc
// @Summary What-if: extend green phase
// @Description Returns a scenario-only upper estimate of the potential delay reduction. Does not mutate live fleet state or the ML prediction.
// @Tags what-if
// @Accept json
// @Produce json
// @Param request body domain.TrafficLightWhatIfRequest true "Traffic-light scenario"
// @Success 200 {object} domain.TrafficLightWhatIfResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /api/v1/what-if/traffic-light [post]
func (h *Handler) WhatIfTrafficLight(w http.ResponseWriter, r *http.Request) {
	var req domain.TrafficLightWhatIfRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	result, err := h.whatIf.TrafficLight(req)
	if err != nil {
		h.writeWhatIfError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, result)
}

// WhatIfReserve godoc
// @Summary What-if: dispatch reserve vehicle
// @Description Estimates potential service-gap reduction if a reserve vehicle can enter the affected segment after dispatch_eta_s. The delayed vehicle prediction is not rewritten.
// @Tags what-if
// @Accept json
// @Produce json
// @Param request body domain.ReserveWhatIfRequest true "Reserve vehicle scenario"
// @Success 200 {object} domain.ReserveWhatIfResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /api/v1/what-if/reserve [post]
func (h *Handler) WhatIfReserve(w http.ResponseWriter, r *http.Request) {
	var req domain.ReserveWhatIfRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	result, err := h.whatIf.Reserve(req)
	if err != nil {
		h.writeWhatIfError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, result)
}

func (h *Handler) writeWhatIfError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, whatif.ErrVehicleNotFound):
		status = http.StatusNotFound
	case errors.Is(err, whatif.ErrVehicleOffline), errors.Is(err, whatif.ErrPredictionUnavailable):
		status = http.StatusConflict
	case errors.Is(err, whatif.ErrInvalidExtension), errors.Is(err, whatif.ErrInvalidDispatchETA):
		status = http.StatusBadRequest
	}

	if status == http.StatusInternalServerError {
		h.logger.Error().Err(err).Msg("what-if calculation failed")
	}
	h.writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
