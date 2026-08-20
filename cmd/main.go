package main

import (
	"air_payment/internal/app"
	"air_payment/internal/domain"
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ikermy/air_common/pkg/com"
	"github.com/ikermy/air_common/pkg/mode"
	"github.com/ikermy/air_logger/v2/pkg/logger"
)

func main() {
	// Логгер: режим os.Stdout для Docker
	logSetup := logger.StdOut()
	// Можно установить через mode.SetLogLevel иначе установится из env в InitFromEnv
	// Если не устанавливать ничего = info
	// Уровень логирования читается из env.LOG_LEVEL
	logSetup.WithLogLevel(logSetup.FromString(mode.GetLogLevel()))
	logSetup.Apply()

	logger.Debug(com.GetVersionInfo())

	// Корневой контекст процесса, отменяется по сигналам ОС
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	a := app.New(ctx)
	a.Run()

	// Ожидание завершения работы
	<-domain.Exit

	logger.Infoln("Приложение air_payment завершено")
}
