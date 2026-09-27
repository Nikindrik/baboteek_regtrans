package schedule

import (
	"bufio"
	"encoding/csv"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

type UnitMapping struct {
	UnitID int64
	TrID   int64
	Start  *time.Time
	End    *time.Time
}

type UnitResolver struct {
	ByUnit map[int64][]UnitMapping
}

func NewUnitResolver() *UnitResolver {
	return &UnitResolver{ByUnit: make(map[int64][]UnitMapping)}
}

// LoadUnitResolver читает mapping из CSV (unit_id, tr_id)
func LoadUnitResolver(path string) (*UnitResolver, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReaderSize(f, 1<<20))
	r.ReuseRecord = false
	hrow, err := r.Read()
	if err != nil {
		return nil, err
	}
	h := headerMap(hrow)

	out := NewUnitResolver()
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		uid, err1 := parseInt64(getCol(row, h, "unit_id"))
		tid, err2 := parseInt64(getCol(row, h, "tr_id"))
		if err1 != nil || err2 != nil {
			continue
		}

		var st, en *time.Time
		if s := getCol(row, h, "t_start"); strings.TrimSpace(s) != "" {
			if t, e := ParseTime(s); e == nil {
				st = &t
			}
		}
		if s := getCol(row, h, "t_end"); strings.TrimSpace(s) != "" {
			if t, e := ParseTime(s); e == nil {
				en = &t
			}
		}

		out.ByUnit[uid] = append(out.ByUnit[uid], UnitMapping{UnitID: uid, TrID: tid, Start: st, End: en})
	}
	return out, nil
}

// Resolve возвращает tr_id. Если mapping не найден — строго false (никакого хардкода!)
func (u *UnitResolver) Resolve(unitID int64, T time.Time) (int64, bool) {
	rows := u.ByUnit[unitID]
	if len(rows) == 0 {
		return 0, false
	}
	if len(rows) == 1 {
		return rows[0].TrID, true
	}

	for _, r := range rows {
		if r.Start != nil && T.Before(*r.Start) {
			continue
		}
		if r.End != nil && T.After(*r.End) {
			continue
		}
		return r.TrID, true
	}
	return rows[0].TrID, true
}

func ParseTime(s string) (time.Time, error) {
	return ParseTimeInLocation(s, time.UTC)
}
