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

// rescheduleDeadline 允许改约的最晚时间：开始前两小时（不含），之后锁定。
const rescheduleDeadline = 2 * time.Hour

// Reschedule 会员本人改约：开始前两小时之前，可将待确认/已确认的预约换到无冲突的新机位与时段。
// 改约成功后预约编号与状态保留；新机位立即占用，旧机位仅在没有其他有效预约时恢复空闲。
// 若新机位时段存在冲突，事务回滚，原预约照旧可用。
func (s *ReservationService) Reschedule(id uint, userID uint, req *dto.RescheduleReservationReq) (*model.Reservation, error) {
	if !req.EndTime.After(req.StartTime) {
		return nil, util.NewAppError(constants.CodeValidation, "改约结束时间必须晚于开始时间")
	}
	res, err := s.getReservation(id)
	if err != nil {
		return nil, err
	}
	if res.UserID != userID {
		return nil, util.NewAppError(constants.CodeForbidden, "只能修改本人的预约")
	}
	if !canRescheduleStatus(res.Status) {
		return nil, util.NewAppError(constants.CodeReservation, "仅待确认或已确认的预约可以改约")
	}
	if !beforeRescheduleDeadline(res.StartTime, time.Now()) {
		return nil, util.NewAppError(constants.CodeReservation, "距开始不足两小时，已锁定无法改约")
	}
	if req.StationID == res.StationID && req.StartTime.Equal(res.StartTime) && req.EndTime.Equal(res.EndTime) {
		return nil, util.NewAppError(constants.CodeValidation, "新机位与时段和当前预约一致，无需改约")
	}
	newStation, err := s.stationService.GetByID(req.StationID)
	if err != nil {
		return nil, err
	}
	if newStation.Status != constants.StationIdle && newStation.Status != constants.StationReserved {
		return nil, util.NewAppError(constants.CodeStationBusy, "新机位当前不可预约，请选择其他机位")
	}
	cnt, err := s.reservationRepo.CountConflict(req.StationID, req.StartTime, req.EndTime, id)
	if err != nil {
		return nil, fmt.Errorf("reschedule count conflict: %w", err)
	}
	if cnt > 0 {
		return nil, util.NewAppError(constants.CodeReservation, "新机位该时段已被预约，请更换机位或时段")
	}

	oldStationID := res.StationID
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// 锁定预约行，防止与取消/开机/并发改约竞争。
		lockedRes, err := s.reservationRepo.LockByID(tx, id)
		if err != nil {
			return err
		}
		if lockedRes.UserID != userID || !canRescheduleStatus(lockedRes.Status) ||
			lockedRes.StationID != oldStationID || !lockedRes.StartTime.Equal(res.StartTime) || !lockedRes.EndTime.Equal(res.EndTime) {
			return util.NewAppError(constants.CodeReservation, "预约状态已变化，请刷新后重试")
		}
		if !beforeRescheduleDeadline(lockedRes.StartTime, time.Now()) {
			return util.NewAppError(constants.CodeReservation, "距开始不足两小时，已锁定无法改约")
		}
		// 按机位 ID 升序加锁，避免多事务交叉加锁导致死锁。
		stationIDs := sortedStationIDs(oldStationID, req.StationID)
		lockedStations := make(map[uint]*model.Station, 2)
		for _, sid := range stationIDs {
			station, lerr := s.stationService.LockForUpdate(tx, sid)
			if lerr != nil {
				return lerr
			}
			lockedStations[sid] = station
		}
		dst := lockedStations[req.StationID]
		if dst.Status != constants.StationIdle && dst.Status != constants.StationReserved {
			return util.NewAppError(constants.CodeStationBusy, "新机位当前不可预约，请选择其他机位")
		}
		// 事务内二次校验冲突：新机位时段不能撞上别人的有效预约。
		cnt, cerr := s.reservationRepo.CountConflictWithTx(tx, req.StationID, req.StartTime, req.EndTime, id)
		if cerr != nil {
			return cerr
		}
		if cnt > 0 {
			return util.NewAppError(constants.CodeReservation, "新机位该时段已被预约，请更换机位或时段")
		}
		// 新机位马上占用。
		dst.Status = constants.StationReserved
		if err := tx.Save(dst).Error; err != nil {
			return err
		}
		// 预约编号保留，仅更新机位与时段，状态不变。
		lockedRes.StationID = req.StationID
		lockedRes.StartTime = req.StartTime
		lockedRes.EndTime = req.EndTime
		if err := s.reservationRepo.UpdateWithTx(tx, lockedRes); err != nil {
			return err
		}
		// 旧机位只在没有其他有效预约时恢复空闲。
		if req.StationID != oldStationID {
			restCnt, rerr := s.reservationRepo.CountActiveByStation(tx, oldStationID, id)
			if rerr != nil {
				return rerr
			}
			if restCnt == 0 {
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
	res.StationID = req.StationID
	res.StartTime = req.StartTime
	res.EndTime = req.EndTime
	s.logger.Info(fmt.Sprintf(constants.LogTemplates["reservation_reschedule_ok"],
		id, oldStationID, req.StationID, req.StartTime.Format("2006-01-02 15:04"), req.EndTime.Format("2006-01-02 15:04")))
	return res, nil
}

// canRescheduleStatus 判断预约状态是否允许改约（仅待确认/已确认）。
func canRescheduleStatus(status string) bool {
	return status == constants.ReservationPending || status == constants.ReservationConfirmed
}

// beforeRescheduleDeadline 判断 now 是否早于开始前两小时的改约截止时间。
func beforeRescheduleDeadline(start, now time.Time) bool {
	return now.Before(start.Add(-rescheduleDeadline))
}

// sortedStationIDs 返回去重后升序排列的机位 ID，用于统一加锁顺序。
func sortedStationIDs(a, b uint) []uint {
	if a == b {
		return []uint{a}
	}
	if a < b {
		return []uint{a, b}
	}
	return []uint{b, a}
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
