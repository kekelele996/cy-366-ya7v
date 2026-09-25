package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/esportsbar/backend/internal/model"
)

// ReservationRepository 预约仓储。
type ReservationRepository struct {
	db *gorm.DB
}

// NewReservationRepository 构造预约仓储。
func NewReservationRepository(db *gorm.DB) *ReservationRepository {
	return &ReservationRepository{db: db}
}

// Create 创建预约。
func (r *ReservationRepository) Create(res *model.Reservation) error {
	return r.db.Create(res).Error
}

// FindByID 查询预约。
func (r *ReservationRepository) FindByID(id uint) (*model.Reservation, error) {
	var res model.Reservation
	err := r.db.First(&res, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &res, err
}

// Update 更新预约。
func (r *ReservationRepository) Update(res *model.Reservation) error {
	return r.db.Save(res).Error
}

// UpdateWithTx 在指定事务内更新预约（改约使用）。
func (r *ReservationRepository) UpdateWithTx(tx *gorm.DB, res *model.Reservation) error {
	return tx.Save(res).Error
}

// LockByID 行锁查询预约（改约/并发状态流转使用）。
func (r *ReservationRepository) LockByID(tx *gorm.DB, id uint) (*model.Reservation, error) {
	var res model.Reservation
	err := tx.Clauses(clauseLocking()).First(&res, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &res, err
}

// CountActiveByStation 统计机位上除指定预约外的有效预约数（用于旧机位释放判定）。
func (r *ReservationRepository) CountActiveByStation(tx *gorm.DB, stationID, excludeID uint) (int64, error) {
	var cnt int64
	query := tx.Model(&model.Reservation{}).
		Where("station_id = ? AND status IN ?", stationID, []string{"pending", "confirmed", "checked_in"})
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	err := query.Count(&cnt).Error
	return cnt, err
}

// CountConflictWithTx 与 CountConflict 相同，但在指定事务内执行（改约事务内二次校验）。
func (r *ReservationRepository) CountConflictWithTx(tx *gorm.DB, stationID uint, start, end time.Time, excludeID uint) (int64, error) {
	var cnt int64
	query := tx.Model(&model.Reservation{}).
		Where("station_id = ? AND status IN ?", stationID, []string{"pending", "confirmed", "checked_in"}).
		Where("start_time < ? AND end_time > ?", end, start)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	err := query.Count(&cnt).Error
	return cnt, err
}

// List 分页查询预约。
func (r *ReservationRepository) List(page, pageSize int, status string, userID uint) ([]model.Reservation, int64, error) {
	var list []model.Reservation
	var total int64
	query := r.db.Model(&model.Reservation{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("start_time DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// CountConflict 统计机位在时段内的冲突预约数。
func (r *ReservationRepository) CountConflict(stationID uint, start, end time.Time, excludeID uint) (int64, error) {
	var cnt int64
	query := r.db.Model(&model.Reservation{}).
		Where("station_id = ? AND status IN ?", stationID, []string{"pending", "confirmed", "checked_in"}).
		Where("start_time < ? AND end_time > ?", end, start)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	err := query.Count(&cnt).Error
	return cnt, err
}
