package db

import (
	"air_payment/internal/domain"
	"air_payment/internal/repository"
	repoMysql "air_payment/internal/repository/mysql"
	"context"

	"github.com/ikermy/air_common/pkg/comdb"
	"github.com/ikermy/air_logger/v2/pkg/logger"

	_ "github.com/go-sql-driver/mysql"
)

// DB обёртка соединения с базой данных и репозиториями
type DB struct {
	*comdb.DB
	repository.InternalDBRepository
}

// New создаёт подключение к БД и инициализирует репозитории
func New(parent context.Context) (*DB, error) {
	base, err := comdb.New(parent)
	if err != nil {
		return nil, err
	}
	repo, err := repoMysql.New(base)
	if err != nil {
		return nil, err
	}
	return &DB{
		DB:                   base,
		InternalDBRepository: repo.Internal,
	}, nil
}

func (d *DB) HandlerClose() {
	go func() {
		<-d.MainCTX().Done()
		logger.Info("DB: контекст отменен, ожидаю завершения всех операций...")
		<-domain.UsersDB
		logger.Info("DB: все модули завершили работу, закрываю соединение...")
		if err := d.Close(); err != nil {
			logger.Error("DB: ошибка при закрытии: %v", err)
		}
		close(domain.Exit)
	}()
}
