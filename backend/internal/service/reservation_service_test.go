package service

import (
	"testing"
	"time"

	"github.com/esportsbar/backend/internal/constants"
	"github.com/esportsbar/backend/internal/model"
	"github.com/esportsbar/backend/internal/util"
)

// TestCanReschedule 改约资格白盒测试：本人/角色、状态机、开始前 2 小时窗口。
func TestCanReschedule(t *testing.T) {
	now := time.Now()
	in3h := now.Add(3 * time.Hour)
	in1h := now.Add(1 * time.Hour)
	cases := []struct {
		name      string
		res       *model.Reservation
		userID    uint
		role      string
		wantCode  int
		wantError bool
	}{
		{name: "member_own_pending_ok", res: &model.Reservation{UserID: 7, Status: constants.ReservationPending, StartTime: in3h}, userID: 7, role: constants.RoleMember},
		{name: "member_own_confirmed_ok", res: &model.Reservation{UserID: 7, Status: constants.ReservationConfirmed, StartTime: in3h}, userID: 7, role: constants.RoleMember},
		{name: "member_others_forbidden", res: &model.Reservation{UserID: 8, Status: constants.ReservationConfirmed, StartTime: in3h}, userID: 7, role: constants.RoleMember, wantCode: constants.CodeForbidden, wantError: true},
		{name: "staff_can_reschedule_others", res: &model.Reservation{UserID: 8, Status: constants.ReservationConfirmed, StartTime: in3h}, userID: 7, role: constants.RoleStaff},
		{name: "admin_can_reschedule_others", res: &model.Reservation{UserID: 8, Status: constants.ReservationPending, StartTime: in3h}, userID: 7, role: constants.RoleAdmin},
		{name: "checked_in_rejected", res: &model.Reservation{UserID: 7, Status: constants.ReservationCheckedIn, StartTime: in3h}, userID: 7, role: constants.RoleMember, wantCode: constants.CodeReservation, wantError: true},
		{name: "completed_rejected", res: &model.Reservation{UserID: 7, Status: constants.ReservationCompleted, StartTime: in3h}, userID: 7, role: constants.RoleMember, wantCode: constants.CodeReservation, wantError: true},
		{name: "cancelled_rejected", res: &model.Reservation{UserID: 7, Status: constants.ReservationCancelled, StartTime: in3h}, userID: 7, role: constants.RoleMember, wantCode: constants.CodeReservation, wantError: true},
		{name: "within_2h_rejected", res: &model.Reservation{UserID: 7, Status: constants.ReservationConfirmed, StartTime: in1h}, userID: 7, role: constants.RoleMember, wantCode: constants.CodeReservation, wantError: true},
		{name: "already_started_rejected", res: &model.Reservation{UserID: 7, Status: constants.ReservationConfirmed, StartTime: now.Add(-time.Hour)}, userID: 7, role: constants.RoleMember, wantCode: constants.CodeReservation, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := canReschedule(tc.res, tc.userID, tc.role, now)
			if tc.wantError {
				if err == nil {
					t.Fatalf("canReschedule() = nil, want error code %d", tc.wantCode)
				}
				appErr, ok := err.(*util.AppError)
				if !ok {
					t.Fatalf("canReschedule() error type = %T, want *util.AppError", err)
				}
				if appErr.Code != tc.wantCode {
					t.Fatalf("canReschedule() error code = %d, want %d", appErr.Code, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("canReschedule() = %v, want nil", err)
			}
		})
	}
}
