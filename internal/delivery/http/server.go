package http

import (
	"air_payment/internal/db"
	"air_payment/internal/domain"
	"air_payment/internal/metrics"
	bybit2 "air_payment/internal/processor/bybit"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/ikermy/air_logger/v2/pkg/logger"
)

type DB interface {
	GetUserTariff(ctx context.Context, userID uint32) (json.RawMessage, error)
}

// PaymentServiceInterface интерфейс для сервиса платежей
type PaymentServiceInterface interface {
	GetSupportedCurrencies() ([]bybit2.SupportedCurrency, error)
	CalculatePayment(currency, network string, amountUSD float64) (*bybit2.PaymentCalculation, error)
	CreatePaymentRequest(userID uint32, currency, network string, amountUSD float64) (*bybit2.PaymentRequest, *bybit2.DepositAddress, error)
	GetPaymentStatus(orderID string) (json.RawMessage, error)
	UpdatePaymentStatus(orderID, status, txHash string, receivedAmount float64, confirmations int) error
	GetPendingPayments() (json.RawMessage, error)
	CheckExpiredPayments() error
	GetDepositAddress(orderID string) (string, error)
	GetPaymentCreatedAt(orderID string) (int64, error)
	CheckIfTxHashAlreadyUsed(txHash string) (string, error)

	// Методы для работы с повторным использованием платежей
	FindActivePaymentByAddress(userID uint32, currency, network, address string) (string, error)
	GetActivePaymentsByUserAndCurrency(userID uint32, currency, network string) (json.RawMessage, error)
	UpdatePaymentExpiration(orderID string, newExpiresAt int64) error

	// Методы для доступа к bybit API
	GetCurrencyPrice(currency string) (float64, error)
	GetDepositAddressDirect(currency, network string) (*bybit2.DepositAddress, error)
	GetBybitClient() *bybit2.Client
}

type Web struct {
	cancel         context.CancelFunc // Функция для отмены контекста
	ctx            context.Context    // Контекст для управления жизненным циклом
	App            *fiber.App
	db             DB
	paymentService PaymentServiceInterface
	paymentMonitor *bybit2.PaymentMonitor
	sseManager     *SSEManager
}

func New(parent context.Context, d *db.DB) *Web {
	ctx, cancel := context.WithCancel(parent)
	g := fiber.New(fiber.Config{
		DisableStartupMessage: true, // Отключаем стартовые сообщения Fiber
	})

	// Читаем API_KEY из файла
	apiKeyFile := strings.TrimSpace(os.Getenv("API_KEY_FILE"))
	apiKey, err := os.ReadFile(apiKeyFile)
	if err != nil {
		logger.Fatal("ошибка чтения API_KEY из файла %s: %v", apiKeyFile, err)
	}

	// Читаем SECRET_KEY из файла
	secretKeyFile := strings.TrimSpace(os.Getenv("SECRET_KEY_FILE"))
	secretKey, err := os.ReadFile(secretKeyFile)
	if err != nil {
		logger.Fatal("ошибка чтения SECRET_KEY из файла %s: %v", secretKeyFile, err)
	}

	conf := &domain.Bybit{
		ApiKey:    strings.TrimSpace(string(apiKey)),
		SecretKey: strings.TrimSpace(string(secretKey)),
		IsTestnet: os.Getenv("TESTNET") == "true",
	}

	if conf.ApiKey == "" || conf.SecretKey == "" {
		logger.Fatal("API_KEY and SECRET_KEY must be set")
	}

	// Создаем сервис платежей bybit
	paymentService := bybit2.NewPaymentService(ctx, conf, d)

	// Создаем SSE менеджер
	sseManager := NewSSEManager()

	// Создаем и запускаем PaymentMonitor
	paymentMonitor := bybit2.NewPaymentMonitor(paymentService)
	paymentMonitor.SetSSEManager(sseManager) // Связываем с SSE менеджером
	paymentMonitor.Start(d)

	web := &Web{
		ctx:            ctx,
		cancel:         cancel,
		App:            g,
		db:             d,
		paymentService: paymentService,
		paymentMonitor: paymentMonitor,
		sseManager:     sseManager,
	}

	// Добавляем graceful shutdown для PaymentMonitor
	go func() {
		<-domain.Exit
		logger.Info("Shutting down PaymentMonitor...")
		paymentMonitor.Stop()
	}()

	return web
}

func (w *Web) Shutdown() {
	w.cancel()
}

func (w *Web) Handler() error {
	logger.Info("Web server air_payment started")

	w.App.Use(cors.New(cors.Config{
		AllowOrigins:     "*",   // Работает за Envoy, Envoy сам обрабатывает CORS
		AllowCredentials: false, // Должно быть false при AllowOrigins: "*"
		AllowMethods:     "POST, GET, OPTIONS",
		AllowHeaders:     "Content-Type, Authorization, Cache-Control",
		ExposeHeaders:    "Content-Type, Cache-Control, Connection",
	}))

	w.App.Use(metrics.MetricsMiddleware())

	// Маршрут отдачи метрик
	w.App.Get("/metrics", metrics.Handler())

	w.App.Get("/pay/available", w.AvailableService) // Проверка доступности сервиса

	// Криптоплатежи - REST API
	w.App.Get("/pay/tariff", userIDMiddleware, w.GetTariff)                  // Получение тарифа пользователя
	w.App.Get("/pay/currencies", userIDMiddleware, w.GetSupportedCurrencies) // Список поддерживаемых валют
	w.App.Get("/pay/donation-currencies", w.GetDonationCurrencies)           // Валюты для пожертвований без авторизации
	//w.App.Post("/calculate-payment", w.CalculatePayment) // Предварительный расчёт платежа при использовании криптовалюты отличной от USDT
	w.App.Post("/pay/create-payment", w.CreateBybitPayment)   // Создание платежа через ByBit с QR-кодами
	w.App.Post("/pay/donation-address", w.GetDonationAddress) // Получение адреса для пожертвования без создания платежа
	// Real-time обновления статуса платежей
	w.App.Get("/pay/payment-status-stream", userIDMiddleware, w.HandlePaymentStatusSSE) // SSE для real-time обновлений
	w.App.Get("/pay/payment-status", userIDMiddleware, w.GetPaymentStatus)              // Обычный запрос статуса (fallback)

	//Запуск сервера без SSL для работы за Envoy
	if err := w.App.Listen(":8080"); err != nil {
		return fmt.Errorf("ошибка запуска WEB сервера AiR_Payment: %e", err)
	}

	return nil
}
