package model

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TronSerialLock is the single row every TRON ledger write takes first, so
// order creation, settlement, claims and ticket handling share one total order.
type TronSerialLock struct {
	Name string `json:"name" gorm:"type:varchar(64);primaryKey"`
	Seq  int64  `json:"seq" gorm:"type:bigint"`
}

const tronSerialLockName = "tron_usdt_ledger"

var ErrTronSerialLockUnavailable = errors.New("TRON ledger serial lock is unavailable")

// EnsureTronSerialLock pre-creates the mutex row. It is idempotent.
func EnsureTronSerialLock() error {
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&TronSerialLock{Name: tronSerialLockName}).Error
}

// lockTronSerialTx must be the first statement of every TRON ledger
// transaction, before any user, order, top-up, deposit or ticket row is read.
//
// It is a real write rather than SELECT ... FOR UPDATE: SQLite has no row
// locks, so only a write makes it serialize here too, and a missing row then
// shows up as zero affected rows and fails closed instead of running
// unserialized. Taking it first also means the MySQL read snapshot is opened
// only after the lock is held.
func lockTronSerialTx(tx *gorm.DB) error {
	result := tx.Model(&TronSerialLock{}).Where("name = ?", tronSerialLockName).UpdateColumn("seq", gorm.Expr("seq + ?", 1))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTronSerialLockUnavailable
	}
	return nil
}
