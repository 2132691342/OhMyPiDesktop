// Package repo 是 GORM 数据访问层。这里只有界面设置 KV 一张表。
package repo

import (
	"errors"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/pkg"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store 是设置 KV 的仓库。
type Store struct {
	db *gorm.DB
}

// Open 打开（必要时创建）数据库并迁移。
func Open(path string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeStartup, "打不开应用数据库", err)
	}
	if err := db.AutoMigrate(&domain.Setting{}); err != nil {
		return nil, pkg.Wrap(pkg.CodeStartup, "数据库迁移失败", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭连接。
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Get 读一个键，不存在返回空串（设置项缺失是正常状态，不是错误）。
func (s *Store) Get(key string) string {
	var row domain.Setting
	err := s.db.First(&row, "key = ?", key).Error
	if err != nil {
		return ""
	}
	return row.Value
}

// Set 写一个键。
func (s *Store) Set(key, value string) error {
	err := s.db.Save(&domain.Setting{Key: key, Value: value}).Error
	if err != nil {
		return pkg.Wrap(pkg.CodeInternal, "保存设置失败", err)
	}
	return nil
}

// All 读全部设置。
func (s *Store) All() (map[string]string, error) {
	var rows []domain.Setting
	if err := s.db.Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(pkg.CodeInternal, "读取设置失败", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Key] = r.Value
	}
	return out, nil
}

// SetMany 批量写，单事务。
func (s *Store) SetMany(kv map[string]string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		for k, v := range kv {
			if err := tx.Save(&domain.Setting{Key: k, Value: v}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ErrEmptyKey 在 key 为空时返回。
var ErrEmptyKey = errors.New("空 key")
