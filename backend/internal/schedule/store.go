package schedule

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"baboteek_regtrans/internal/domain"

	"github.com/rs/zerolog"
)

type Store struct {
	mu            sync.RWMutex
	tripSchedules map[int64][]domain.ScheduleStop // tr_id -> отсортированные по времени остановки
	unitToTrip    map[uint32]int64                // unit_id -> tr_id
	logger        zerolog.Logger
}

func NewStore(logger zerolog.Logger) *Store {
	return &Store{
		tripSchedules: make(map[int64][]domain.ScheduleStop),
		unitToTrip:    make(map[uint32]int64),
		logger:        logger.With().Str("component", "schedule-store").Logger(),
	}
}

// LoadSchedulePlan парсит validate/schedule_plan.csv
func (s *Store) LoadSchedulePlan(csvPath string) error {
	file, err := os.Open(csvPath)
	if err != nil {
		return fmt.Errorf("failed to open schedule plan CSV: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// Читаем заголовок: tt_action_item_id,time_begin,order_date,manual_fill,tr_id,geom,building_address
	_, err = reader.Read()
	if err != nil {
		return fmt.Errorf("failed to read CSV header: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) < 6 {
			continue
		}

		actionID, _ := strconv.ParseInt(record[0], 10, 64)
		// Парсим время вида "2026-01-06 06:36:00"
		timeBegin, err := time.Parse("2006-01-02 15:04:05", strings.Split(record[1], ".")[0])
		if err != nil {
			continue
		}
		trID, _ := strconv.ParseInt(record[4], 10, 64)
		lon, lat := parseWKTPoint(record[5])
		address := ""
		if len(record) > 6 {
			address = record[6]
		}

		s.tripSchedules[trID] = append(s.tripSchedules[trID], domain.ScheduleStop{
			ActionItemID: actionID,
			TrID:         trID,
			TimeBegin:    timeBegin,
			Lat:          lat,
			Lon:          lon,
			Address:      address,
		})
		count++
	}

	// Сортируем остановки каждого рейса по плановому времени
	for trID := range s.tripSchedules {
		stops := s.tripSchedules[trID]
		sort.Slice(stops, func(i, j int) bool {
			return stops[i].TimeBegin.Before(stops[j].TimeBegin)
		})
		s.tripSchedules[trID] = stops
	}

	s.logger.Info().
		Int("stops_loaded", count).
		Int("unique_trips", len(s.tripSchedules)).
		Msg("schedule_plan.csv successfully indexed in memory")

	return nil
}

// PreloadUnitMapping связывает unit_id (терминал эмулятора) с tr_id (рейс)
// Читает первые записи из traffic.csv для автоматической привязки
func (s *Store) PreloadUnitMapping(trafficCSVPath string) error {
	file, err := os.Open(trafficCSVPath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	_, _ = reader.Read() // skip header

	s.mu.Lock()
	defer s.mu.Unlock()

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) < 3 {
			continue
		}
		trID, _ := strconv.ParseInt(record[1], 10, 64)
		unitID64, _ := strconv.ParseUint(record[2], 10, 32)
		if trID > 0 && unitID64 > 0 {
			s.unitToTrip[uint32(unitID64)] = trID
		}
	}
	s.logger.Info().Int("mapped_units", len(s.unitToTrip)).Msg("unit_id to tr_id mapping loaded")
	return nil
}

func (s *Store) GetTripStops(trID int64) ([]domain.ScheduleStop, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stops, ok := s.tripSchedules[trID]
	return stops, ok
}

func (s *Store) GetTrIDByUnit(unitID uint32) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	trID, ok := s.unitToTrip[unitID]
	return trID, ok
}

func (s *Store) SetTripMapping(unitID uint32, trID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unitToTrip[unitID] = trID
}
