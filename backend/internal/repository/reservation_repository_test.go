package repository

import (
	"regexp"
	"testing"
	"time"
)

func TestReservationRepositoryCountConflictTx(t *testing.T) {
	gdb, mock := newMockDB(t)
	repo := NewReservationRepository(gdb)
	start := time.Date(2026, 8, 18, 14, 0, 0, 0, time.Local)
	end := start.Add(2 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM `reservations` WHERE (station_id = ? AND status IN (?,?,?)) AND (start_time < ? AND end_time > ?) AND id <> ?")).
		WithArgs(2, "pending", "confirmed", "checked_in", end, start, 9).
		WillReturnRows(sqlmockRowsCount(0))
	cnt, err := repo.CountConflictTx(gdb, 2, start, end, 9)
	if err != nil {
		t.Fatalf("CountConflictTx error: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("unexpected conflict count: %d", cnt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations not met: %v", err)
	}
}

func TestReservationRepositoryCountActiveByStationTx(t *testing.T) {
	gdb, mock := newMockDB(t)
	repo := NewReservationRepository(gdb)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM `reservations` WHERE station_id = ? AND status IN (?,?,?)")).
		WithArgs(1, "pending", "confirmed", "checked_in").
		WillReturnRows(sqlmockRowsCount(1))
	cnt, err := repo.CountActiveByStationTx(gdb, 1)
	if err != nil {
		t.Fatalf("CountActiveByStationTx error: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("unexpected active count: %d", cnt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations not met: %v", err)
	}
}
