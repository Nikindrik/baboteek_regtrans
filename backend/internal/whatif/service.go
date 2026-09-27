package whatif

import (
	"errors"
	"math"
	"time"

	"baboteek_regtrans/internal/domain"
)

var (
	ErrVehicleNotFound       = errors.New("vehicle not found")
	ErrVehicleOffline        = errors.New("vehicle is offline")
	ErrPredictionUnavailable = errors.New("prediction is unavailable for the strict 10-15 minute horizon")
	ErrInvalidExtension      = errors.New("extension_s must be between 1 and 60 seconds")
	ErrInvalidDispatchETA    = errors.New("dispatch_eta_s must be between 60 and 1800 seconds")
)

const (
	DefaultTrafficLightExtensionS = 15
	DefaultReserveDispatchETAS    = 300
)

type Service struct {
	repo domain.FleetRepository
}

func NewService(repo domain.FleetRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) TrafficLight(req domain.TrafficLightWhatIfRequest) (domain.TrafficLightWhatIfResponse, error) {
	extension := req.ExtensionSeconds
	if extension == 0 {
		extension = DefaultTrafficLightExtensionS
	}
	if extension < 1 || extension > 60 {
		return domain.TrafficLightWhatIfResponse{}, ErrInvalidExtension
	}

	vehicle, err := s.vehicleForScenario(req.UnitID)
	if err != nil {
		return domain.TrafficLightWhatIfResponse{}, err
	}

	baseline := vehicle.PredictedDelaySeconds
	// Без данных о фазах светофора и геометрии перекрёстка мы не можем честно
	// пересчитывать ML-прогноз. Поэтому показываем прозрачную верхнюю оценку:
	// максимум extension_s секунд потенциальной экономии, и только при опоздании.
	reduction := math.Min(float64(extension), math.Max(0, baseline))
	scenario := baseline - reduction

	return domain.TrafficLightWhatIfResponse{
		Scenario:                 "traffic_light",
		UnitID:                   vehicle.UnitID,
		TrID:                     vehicle.TrID,
		TargetStopID:             vehicle.TargetStopID,
		TargetTimeBegin:          vehicle.TargetTimeBegin,
		ExtensionSeconds:         extension,
		BaselinePredictedDelayS:  baseline,
		ScenarioPredictedDelayS:  scenario,
		EstimatedDelayReductionS: reduction,
		BaselineLateProbability:  vehicle.LateProbability,
		BaselineRiskLevel:        vehicle.RiskLevel,
		LiveStateChanged:         false,
		GeneratedAt:              time.Now(),
		Assumption:               "Сценарная верхняя оценка: продление зелёной фазы полностью превращается в экономию времени. Данные о фазах светофора и топологии перекрёстка отсутствуют, поэтому вероятность риска и live-state не пересчитываются.",
	}, nil
}

func (s *Service) Reserve(req domain.ReserveWhatIfRequest) (domain.ReserveWhatIfResponse, error) {
	dispatchETA := req.DispatchETASeconds
	if dispatchETA == 0 {
		dispatchETA = DefaultReserveDispatchETAS
	}
	if dispatchETA < 60 || dispatchETA > 1800 {
		return domain.ReserveWhatIfResponse{}, ErrInvalidDispatchETA
	}

	vehicle, err := s.vehicleForScenario(req.UnitID)
	if err != nil {
		return domain.ReserveWhatIfResponse{}, err
	}

	baselineVehicleDelay := vehicle.PredictedDelaySeconds
	positiveDelay := math.Max(0, baselineVehicleDelay)

	// Резервное ТС не исправляет задержку исходного автобуса. Сценарий оценивает
	// только потенциальный разрыв обслуживания: если резерв может войти в работу
	// через dispatch_eta_s, пассажирский сервис восстанавливается не позже этого ETA.
	scenarioServiceDelay := math.Min(positiveDelay, float64(dispatchETA))
	gapReduction := positiveDelay - scenarioServiceDelay

	return domain.ReserveWhatIfResponse{
		Scenario:                      "reserve_vehicle",
		UnitID:                        vehicle.UnitID,
		TrID:                          vehicle.TrID,
		TargetStopID:                  vehicle.TargetStopID,
		TargetTimeBegin:               vehicle.TargetTimeBegin,
		DispatchETASeconds:            dispatchETA,
		BaselineVehicleDelayS:         baselineVehicleDelay,
		VehicleDelayAfterActionS:      baselineVehicleDelay,
		ScenarioServiceDelayS:         scenarioServiceDelay,
		EstimatedServiceGapReductionS: gapReduction,
		BaselineLateProbability:       vehicle.LateProbability,
		BaselineRiskLevel:             vehicle.RiskLevel,
		LiveStateChanged:              false,
		GeneratedAt:                   time.Now(),
		Assumption:                    "Сценарий предполагает, что резервное ТС может войти в обслуживание того же проблемного участка через dispatch_eta_s. Задержка исходного ТС и ML-прогноз не изменяются; оценивается потенциальное сокращение разрыва обслуживания.",
	}, nil
}

func (s *Service) vehicleForScenario(unitID uint32) (domain.VehicleState, error) {
	vehicle, ok := s.repo.GetVehicleSnapshot(unitID)
	if !ok {
		return domain.VehicleState{}, ErrVehicleNotFound
	}
	if !vehicle.IsOnline {
		return domain.VehicleState{}, ErrVehicleOffline
	}
	if !vehicle.PredictionValid || vehicle.TargetStopID <= 0 || vehicle.TargetTimeBegin.IsZero() {
		return domain.VehicleState{}, ErrPredictionUnavailable
	}
	return vehicle, nil
}
