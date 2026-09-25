package service

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/esportsbar/backend/internal/constants"
	"github.com/esportsbar/backend/internal/dto"
	"github.com/esportsbar/backend/internal/model"
	"github.com/esportsbar/backend/internal/repository"
	"github.com/esportsbar/backend/internal/util"
)

// ReservationService 机位预约服务。
type ReservationService struct {
	reservationRepo *repository.ReservationRepository
	stationService  *StationService
	db              *gorm.DB
	logger          *slog.Logger
}

// NewReservationService 构造预约服务。
func NewReservationService(
	reservationRepo *repository.ReservationRepository,
	stationService *StationService,
	db *gorm.DB,
	logger *slog.Logger,
) *ReservationService {
	return &ReservationService{reservationRepo: reservationRepo, stationService: stationService, db: db, logger: logger}
}

// Create 创建预约：校验时段冲突，事务内锁定机位并落库。
func (s *ReservationService) Create(userID uint, req *dto.CreateReservationReq) (*model.Reservation, error) {
	if !req.EndTime.After(req.StartTime) {
		return nil, util.NewAppError(constants.CodeValidation, "预约结束时间必须晚于开始时间")
	}
	station, err := s.stationService.GetByID(req.StationID)
	if err != nil {
		return nil, err
	}
	if station.Status != constants.StationIdle && station.Status != constants.StationReserved {
		return nil, util.NewAppError(constants.CodeStationBusy, "机位当前不可预约，请选择其他机位")
	}
	cnt, err := s.reservationRepo.CountConflict(req.StationID, req.StartTime, req.EndTime, 0)
	if err != nil {
		return nil, fmt.Errorf("reservation count conflict: %w", err)
	}
	if cnt > 0 {
		return nil, util.NewAppError(constants.CodeReservation, "该机位时段已被预约，请更换时段")
	}
	res := &model.Reservation{
		UserID:    userID,
		StationID: req.StationID,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
		Status:    constants.ReservationConfirmed,
		Remark:    req.Remark,
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		locked, err := s.stationService.LockForUpdate(tx, req.StationID)
		if err != nil {
			return err
		}
		if locked.Status == constants.StationIdle {
			locked.Status = constants.StationReserved
			if err := tx.Save(locked).Error; err != nil {
				return err
			}
		}
		return s.reservationRepo.Create(res)
	})
	if err != nil {
		return nil, fmt.Errorf("reservation create tx: %w", err)
	}
	s.logger.Info(fmt.Sprintf(constants.LogTemplates["reservation_create_ok"], userID, req.StationID, req.StartTime.Format("2006-01-02 15:04")))
	return res, nil
}

// Confirm 确认预约（staff/admin）。
func (s *ReservationService) Confirm(id uint) (*model.Reservation, error) {
	res, err := s.getReservation(id)
	if err != nil {
		return nil, err
	}
	if res.Status != constants.ReservationPending && res.Status != constants.ReservationConfirmed {
		return nil, util.NewAppError(constants.CodeReservation, "仅待确认或已确认的预约可以确认")
	}
	res.Status = constants.ReservationConfirmed
	if err := s.reservationRepo.Update(res); err != nil {
		return nil, fmt.Errorf("reservation confirm: %w", err)
	}
	s.logger.Info(fmt.Sprintf(constants.LogTemplates["reservation_confirm_ok"], id))
	return res, nil
}

// Cancel 取消预约：释放机位预约状态。
func (s *ReservationService) Cancel(id uint, userID uint) (*model.Reservation, error) {
	res, err := s.getReservation(id)
	if err != nil {
		return nil, err
	}
	if res.Status != constants.ReservationPending && res.Status != constants.ReservationConfirmed && res.Status != constants.ReservationCheckedIn {
		return nil, util.NewAppError(constants.CodeReservation, "当前状态不可取消")
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		res.Status = constants.ReservationCancelled
		if err := s.reservationRepo.Update(res); err != nil {
			return err
		}
		return releaseStationReserved(tx, s.stationService, res.StationID)
	})
	if err != nil {
		return nil, fmt.Errorf("reservation cancel tx: %w", err)
	}
	s.logger.Info(fmt.Sprintf(constants.LogTemplates["reservation_cancel_ok"], id))
	return res, nil
}

// Reschedule 改约：本人待确认/已确认的预约，可在开始前两小时更换到无冲突的机位与时段。
// 预约编号保留不变；新机位立即占用，旧机位只在没有其他有效预约时恢复空闲；
// 若新时段冲突则整体回滚，原预约照旧可用。
func (s *ReservationService) Reschedule(id uint, userID uint, role string, req *dto.RescheduleReservationReq) (*model.Reservation, error) {
	res, err := s.getReservation(id)
	if err != nil {
		return nil, err
	}
	if err := canReschedule(res, userID, role, time.Now()); err != nil {
		return nil, err
	}
	if !req.EndTime.After(req.StartTime) {
		return nil, util.NewAppError(constants.CodeValidation, "改约结束时间必须晚于开始时间")
	}
	station, err := s.stationService.GetByID(req.StationID)
	if err != nil {
		return nil, err
	}
	if station.Status != constants.StationIdle && station.Status != constants.StationReserved {
		return nil, util.NewAppError(constants.CodeStationBusy, "新机位当前不可预约，请选择其他机位")
	}
	// 冲突预检（排除本预约自身）：新时段撞上别人的有效预约时直接失败，原预约照旧可用。
	cnt, err := s.reservationRepo.CountConflict(req.StationID, req.StartTime, req.EndTime, res.ID)
	if err != nil {
		return nil, fmt.Errorf("reservation reschedule count conflict: %w", err)
	}
	if cnt > 0 {
		return nil, util.NewAppError(constants.CodeReservation, "新机位时段已被预约，请更换时段")
	}
	oldStationID := res.StationID
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// 锁定新机位并复检冲突，防止并发下时段被抢占。
		locked, err := s.stationService.LockForUpdate(tx, req.StationID)
		if err != nil {
			return err
		}
		if locked.Status != constants.StationIdle && locked.Status != constants.StationReserved {
			return util.NewAppError(constants.CodeStationBusy, "新机位当前不可预约，请选择其他机位")
		}
		conflict, err := s.reservationRepo.CountConflictTx(tx, req.StationID, req.StartTime, req.EndTime, res.ID)
		if err != nil {
			return err
		}
		if conflict > 0 {
			return util.NewAppError(constants.CodeReservation, "新机位时段已被预约，请更换时段")
		}
		// 新机位马上占用。
		if locked.Status == constants.StationIdle {
			locked.Status = constants.StationReserved
			if err := tx.Save(locked).Error; err != nil {
				return err
			}
		}
		// 预约编号保留，仅更新机位与时段。
		res.StationID = req.StationID
		res.StartTime = req.StartTime
		res.EndTime = req.EndTime
		if err := tx.Save(res).Error; err != nil {
			return err
		}
		// 旧机位只在没有其他有效预约时恢复空闲。
		if oldStationID != req.StationID {
			remain, err := s.reservationRepo.CountActiveByStationTx(tx, oldStationID)
			if err != nil {
				return err
			}
			if remain == 0 {
				if err := releaseStationReserved(tx, s.stationService, oldStationID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reservation reschedule tx: %w", err)
	}
	s.logger.Info(fmt.Sprintf(constants.LogTemplates["reservation_reschedule_ok"], id, oldStationID, req.StationID, req.StartTime.Format("2006-01-02 15:04")))
	return res, nil
}

// CheckIn 到店扫码开机：预约状态流转为 checked_in，机位置为使用中。
func (s *ReservationService) CheckIn(id uint) (*model.Reservation, error) {
	res, err := s.getReservation(id)
	if err != nil {
		return nil, err
	}
	if res.Status != constants.ReservationConfirmed {
		return nil, util.NewAppError(constants.CodeReservation, "仅已确认的预约可以开机")
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		res.Status = constants.ReservationCheckedIn
		if err := s.reservationRepo.Update(res); err != nil {
			return err
		}
		station, err := s.stationService.LockForUpdate(tx, res.StationID)
		if err != nil {
			return err
		}
		station.Status = constants.StationUsing
		return tx.Save(station).Error
	})
	if err != nil {
		return nil, fmt.Errorf("reservation checkin tx: %w", err)
	}
	s.logger.Info(fmt.Sprintf(constants.LogTemplates["reservation_checkin_ok"], id))
	return res, nil
}

// List 分页查询预约。
func (s *ReservationService) List(query *dto.ReservationQuery) ([]model.Reservation, int64, error) {
	page := query.Page
	pageSize := query.PageSize
	if page <= 0 {
		page = constants.DefaultPage
	}
	if pageSize <= 0 {
		pageSize = constants.DefaultPageSize
	}
	return s.reservationRepo.List(page, pageSize, query.Status, query.UserID)
}

// getReservation 查询预约并统一处理错误。
func (s *ReservationService) getReservation(id uint) (*model.Reservation, error) {
	res, err := s.reservationRepo.FindByID(id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodeNotFound, "预约记录不存在")
		}
		return nil, fmt.Errorf("reservation find: %w", err)
	}
	return res, nil
}

// canReschedule 改约资格校验：本人（或店员/管理员代办）、待确认/已确认、距开始满 2 小时。
func canReschedule(res *model.Reservation, userID uint, role string, now time.Time) error {
	if role != constants.RoleAdmin && role != constants.RoleStaff && res.UserID != userID {
		return util.NewAppError(constants.CodeForbidden, "只能改约本人的预约")
	}
	if res.Status != constants.ReservationPending && res.Status != constants.ReservationConfirmed {
		return util.NewAppError(constants.CodeReservation, "仅待确认或已确认的预约可以改约")
	}
	if res.StartTime.Sub(now) < constants.RescheduleLeadHours*time.Hour {
		return util.NewAppError(constants.CodeReservation, "距开始不足2小时，无法改约，请联系店员处理")
	}
	return nil
}

// releaseStationReserved 将机位从预约状态释放为空闲。
func releaseStationReserved(tx *gorm.DB, svc *StationService, stationID uint) error {
	station, err := svc.LockForUpdate(tx, stationID)
	if err != nil {
		return err
	}
	if station.Status == constants.StationReserved {
		station.Status = constants.StationIdle
		return tx.Save(station).Error
	}
	return nil
}
