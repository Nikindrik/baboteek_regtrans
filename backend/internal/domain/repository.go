package domain

import (
	pb "baboteek_regtrans/pkg/api/v1"
	"time"
)

// FleetRepository — контракт хранилища данных флота.
// Сейчас реализуется через In-Memory (state.FleetStore),
// но может быть легко заменен на PostgreSQL/TimescaleDB.
type FleetRepository interface {
	UpdateFromTelemetry(pt TelemetryPoint, match *MatchResult) *VehicleState
	UpdateFromML(unitID uint32, pred *pb.PredictionResponse) *VehicleState
	GetAllVehiclesSnapshot() []VehicleState
	GetVehicleSnapshot(unitID uint32) (VehicleState, bool)
	GetRecentIncidents() []IncidentCard
	CheckStaleVehicles(threshold time.Duration)
}
