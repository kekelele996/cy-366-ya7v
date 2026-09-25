package service

import (
	"testing"
	"time"

	"github.com/esportsbar/backend/internal/constants"
)

// TestCanRescheduleStatus 改约仅允许待确认/已确认状态。
func TestCanRescheduleStatus(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{constants.ReservationPending, true},
		{constants.ReservationConfirmed, true},
		{constants.ReservationCheckedIn, false},
		{constants.ReservationCompleted, false},
		{constants.ReservationCancelled, false},
	}
	for _, tc := range cases {
		if got := canRescheduleStatus(tc.status); got != tc.want {
			t.Fatalf("canRescheduleStatus(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// TestBeforeRescheduleDeadline 开始前两小时为改约截止线。
func TestBeforeRescheduleDeadline(t *testing.T) {
	start := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"三小时前可改约", start.Add(-3 * time.Hour), true},
		{"两小时零一秒前可改约", start.Add(-rescheduleDeadline - time.Second), true},
		{"恰为两小时不可改约", start.Add(-rescheduleDeadline), false},
		{"不足两小时不可改约", start.Add(-time.Hour), false},
		{"开始之后不可改约", start.Add(time.Hour), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := beforeRescheduleDeadline(start, tc.now); got != tc.want {
				t.Fatalf("beforeRescheduleDeadline(start=%v, now=%v) = %v, want %v", start, tc.now, got, tc.want)
			}
		})
	}
}

// TestSortedStationIDs 机位 ID 去重并升序，用于统一加锁顺序。
func TestSortedStationIDs(t *testing.T) {
	if got := sortedStationIDs(2, 2); len(got) != 1 || got[0] != 2 {
		t.Fatalf("sortedStationIDs(2,2) = %v, want [2]", got)
	}
	if got := sortedStationIDs(1, 2); got[0] != 1 || got[1] != 2 {
		t.Fatalf("sortedStationIDs(1,2) = %v, want [1 2]", got)
	}
	if got := sortedStationIDs(3, 1); got[0] != 1 || got[1] != 3 {
		t.Fatalf("sortedStationIDs(3,1) = %v, want [1 3]", got)
	}
}
