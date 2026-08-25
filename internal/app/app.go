package app

import (
	"air_payment/internal/db"
	"air_payment/internal/delivery/http"
	"air_payment/internal/domain"
	"context"
	"time"

	"github.com/ikermy/air-common/pkg/com"
	"github.com/ikermy/air-common/pkg/endpoint"
	"github.com/ikermy/air-logger/v2/pkg/logger"
)

type Web interface {
	Handler() error
	Shutdown()
}

type DB interface {
	HandlerClose()
}

type End interface {
	Shutdown(shutCh chan<- com.LogMsg)
	NotificationListener(notifCh chan<- com.LogMsg)
}

type App struct {
	ctx    context.Context
	cancel context.CancelFunc
	db     DB
	web    Web
	end    End
}

func New(parent context.Context) *App {
	ctx, cancel := context.WithCancel(parent)

	d, err := db.New(ctx)
	if err != nil {
		logger.Fatal("Ошибка инициализации базы данных: %v", err)
	}

	w := http.New(ctx, d)
	e := endpoint.New(ctx, d)
	return &App{
		ctx:    ctx,
		cancel: cancel,
		db:     d,
		web:    w,
		end:    e,
	}
}

func (a *App) Run() {
	// Запускаем сервер
	go func() {
		err := a.web.Handler()
		if err != nil {
			//errCh <- err
			logger.Fatal(err)
		}
	}()

	// Создаю шину для логирования сообщений от модулей, которая будет использоваться в горутинах для отправки логов в uReader
	bus := com.NewBus(10)

	// Ждёт сигнала закрытия БД
	go a.db.HandlerClose()

	// читатель ошибок
	go uReader(bus.MsgCh)

	// Запускаю слушателя уведомлений
	bus.Add(func(ch chan<- com.LogMsg) { a.end.NotificationListener(ch) })

	// Обработка сигнала завершения
	go func() {
		<-a.ctx.Done()
		// Аварийный таймаут на случай, если что-то пойдет не так с завершением, чтобы гарантировать закрытие канала и освобождение ресурсов
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			<-ticker.C
			close(domain.UsersDB)
		}()

		logger.Info("App: получен сигнал завершения, начинаю shutdown")

		// Останавливаем Web серве что бы он не принимал новые сообщения
		a.web.Shutdown()

		bus.Add(func(ch chan<- com.LogMsg) { a.end.Shutdown(ch) })

		logger.Info("App: все модули завершены, отправляю сигнал завершения БД")
		// ждём всех producers и закрываем канал
		bus.WaitAndClose()
		// Отправляем сигнал о завершении работы с БД
		close(domain.UsersDB)
	}()
}

func uReader(readCh <-chan com.LogMsg) {
	for info := range readCh {
		switch info.Log {
		case 0: // Info
			logger.Info("%s: %v", info.Mod, info.Msg, info.UID)
		case 1: // Info
			logger.Error("%s: %v", info.Mod, info.Msg, info.UID)
		case 2: // Info
			logger.Warn("%s: %v", info.Mod, info.Msg, info.UID)
		case 3: // Info
			logger.Debug("%s: %v", info.Mod, info.Msg, info.UID)
		}
	}
}
