package bybit

import (
	"air_payment/internal/domain"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/ikermy/air-common/v2/pkg/com"
	"github.com/ikermy/air-common/v2/pkg/mode"
	"github.com/ikermy/air-logger/v2/pkg/logger"
)

type DB interface {
	// GetUserTariff Реализация в comdb.Exterior
	GetUserTariff(ctx context.Context, userId uint64) (json.RawMessage, error)

	// Методы для установки платеже при его поступлении
	SetBillingData(ctx context.Context, userId uint64, payment float64, months uint8, discount float64) error

	SaveCryptoPaymentRequest(ctx context.Context, orderID string, userID uint64, currency, network string, amount, amountUSD float64, depositAddress, depositTag string, expiresAt int64) error
	GetCryptoPaymentStatus(ctx context.Context, orderID string) (json.RawMessage, error)
	UpdateCryptoPaymentStatus(ctx context.Context, orderID, status, txHash string, receivedAmount float64, confirmations int) error
	GetPendingCryptoPayments(ctx context.Context) (json.RawMessage, error)
	CheckExpiredCryptoPayments(ctx context.Context) error
	GetCryptoPaymentDepositAddress(ctx context.Context, orderID string) (string, error) // Новый метод
	GetCryptoPaymentCreatedAt(ctx context.Context, orderID string) (int64, error)       // Новый метод для получения времени создания
	CheckIfTxHashAlreadyUsed(ctx context.Context, txHash string) (string, error)        // Новый метод для проверки использования txHash

	// Методы для работы с повторным использованием платежей
	FindActivePaymentByAddress(ctx context.Context, userID uint64, currency, network, address string) (string, error)         // Возвращает orderID активного платежа
	GetActivePaymentsByUserAndCurrency(ctx context.Context, userID uint64, currency, network string) (json.RawMessage, error) // Получает все активные платежи пользователя
	UpdatePaymentExpiration(ctx context.Context, orderID string, newExpiresAt int64) error                                    // Продлевает срок действия платежа
}

// PaymentService сервис для управления криптоплатежами
type PaymentService struct {
	ctx         context.Context
	cancel      context.CancelFunc
	bybitClient *Client
	db          DB
}

// NewPaymentService создает новый сервис платежей
func NewPaymentService(parent context.Context, conf *domain.Bybit, db DB,
) *PaymentService {
	ctx, cancel := context.WithCancel(parent)
	return &PaymentService{
		ctx:         ctx,
		cancel:      cancel,
		bybitClient: NewBybitClient(conf),
		db:          db,
	}
}

// GetBybitClient возвращает bybit клиент для прямого доступа к API
func (ps *PaymentService) GetBybitClient() *Client {
	return ps.bybitClient
}

// generateOrderID генерирует уникальный ID заказа
func (ps *PaymentService) generateOrderID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes) // Игнорируем ошибку, так как crypto/rand.Read практически никогда не возвращает ошибку
	return hex.EncodeToString(bytes)
}

// CalculatePayment выполняет предварительный расчёт суммы криптовалюты без создания платежа
func (ps *PaymentService) CalculatePayment(currency, network string, amountUSD float64) (*PaymentCalculation, error) {
	// Получаем актуальный курс валюты через bybit API
	price, err := ps.bybitClient.GetCurrencyPrice(currency)
	if err != nil {
		logger.Error("Failed to get currency price for %s: %v", currency, err)
		return nil, fmt.Errorf("failed to get currency price for %s: %w", currency, err)
	}

	// Вычисляем количество криптовалюты на основе актуального курса
	amount := amountUSD / price

	logger.Info("Currency conversion: %s price=%.6f, USD amount=%.2f, crypto amount=%.6f",
		currency, price, amountUSD, amount)

	// Создаем структуру с расчётом
	calculation := &PaymentCalculation{
		Currency:         currency,
		Network:          network,
		AmountUSD:        amountUSD,
		CryptoAmount:     amount,
		CurrencyPrice:    price,
		ExpiresInMinutes: 30, // Время жизни будущего платежа
		CalculatedAt:     time.Now().Unix(),
	}

	return calculation, nil
}

// CreatePaymentRequest создает запрос на оплату или повторно использует существующий активный платеж
func (ps *PaymentService) CreatePaymentRequest(userID uint64, currency, network string, amountUSD float64) (*PaymentRequest, *DepositAddress, error) {
	// Получаем актуальный курс валюты через bybit API
	price, err := ps.bybitClient.GetCurrencyPrice(currency)
	if err != nil {
		logger.Error("Failed to get currency price for %s: %v", currency, err)
		return nil, nil, fmt.Errorf("failed to get currency price for %s: %w", currency, err)
	}

	// Вычисляем количество криптовалюты на основе актуального курса
	amount := amountUSD / price

	logger.Info("Currency conversion: %s price=%.6f, USD amount=%.2f, crypto amount=%.6f",
		currency, price, amountUSD, amount, userID)

	// Получаем адрес для депозита через bybit API
	depositAddress, err := ps.bybitClient.GetDepositAddress(currency, network)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get deposit address: %w", err)
	}

	// УЛУЧШЕННАЯ ЛОГИКА: Проверяем активные платежи с приоритетом на SSE соединения
	//logger.Debug("Checking for existing payments: userID=%d, currency=%s, network=%s, address=%s",
	//	userID, currency, network, depositAddress.Address)

	var existingOrderID string

	// ПРИОРИТЕТ 1: Проверяем, есть ли активные SSE соединения для платежей с тем же адресом
	// Этот функционал будет реализован через внедрение зависимости SSE менеджера

	// ПРИОРИТЕТ 2: Ищем активный платеж в БД с тем же адресом
	existingOrderID, err = ps.db.FindActivePaymentByAddress(ps.ctx, userID, currency, network, depositAddress.Address)
	//if err != nil {
	//	logger.Debug("Error checking for existing active payment: %v", err)
	//} else {
	//	logger.Debug("FindActivePaymentByAddress returned: orderID=%s", existingOrderID)
	//}

	if existingOrderID != "" {
		// Найден активный платеж с тем же адресом - повторно используем его
		logger.Info("Найден активный платеж с тем же адресом - используем его: orderID=%s, address=%s",
			existingOrderID, depositAddress.Address, userID)

		// Продлеваем срок действия существующего платежа на 30 минут от текущего времени
		newExpiresAt := time.Now().Add(30 * time.Minute).Unix()
		err = ps.db.UpdatePaymentExpiration(ps.ctx, existingOrderID, newExpiresAt)
		if err != nil {
			logger.Error("Failed to extend payment expiration: %v", err)
			// Не возвращаем ошибку, так как платеж все еще можем использовать
		} else {
			logger.Info("Extended payment expiration: orderID=%s, new_expires_at=%d", existingOrderID, newExpiresAt)
		}

		// Создаем структуру PaymentRequest для существующего платежа
		paymentRequest := &PaymentRequest{
			UserID:    userID,
			Currency:  currency,
			Amount:    amount,
			AmountUSD: amountUSD,
			OrderID:   existingOrderID, // Используем существующий orderID
			Network:   network,
			ExpiresAt: newExpiresAt,
		}

		depositAddress.QRCode = depositAddress.Address

		logger.Info("Reusing existing payment: orderID=%s, userID=%d, currency=%s, amount=%f, address=%s",
			existingOrderID, userID, currency, amount, depositAddress.Address)

		// Отправляем уведомление о старом платеже
		go sendNotification(userID, false, *paymentRequest)

		return paymentRequest, depositAddress, nil
	}

	// Если активного платежа не найдено, создаем новый
	logger.Info("No existing active payment found, creating new payment")

	// Генерируем уникальный orderID
	orderID := ps.generateOrderID()

	depositAddress.QRCode = depositAddress.Address

	logger.Debug("Generated payment URI for QR code: %s", depositAddress.Address)

	// Создаем запрос на оплату
	paymentRequest := &PaymentRequest{
		UserID:    userID,
		Currency:  currency,
		Amount:    amount,
		AmountUSD: amountUSD,
		OrderID:   orderID,
		Network:   network,
		ExpiresAt: time.Now().Add(30 * time.Minute).Unix(), // 30 минут на оплату
	}

	// Сохраняем в базу данных через интерфейс DB
	err = ps.db.SaveCryptoPaymentRequest(ps.ctx,
		orderID, userID, currency, network, amount, amountUSD,
		depositAddress.Address, depositAddress.Tag, paymentRequest.ExpiresAt)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to save payment request: %w", err)
	}

	logger.Info("Created new payment request: orderID=%s, userID=%d, currency=%s, amount=%f, address=%s",
		orderID, userID, currency, amount, depositAddress.Address)

	// Отправляем уведомление о новом платеже
	go sendNotification(userID, true, *paymentRequest)

	return paymentRequest, depositAddress, nil
}

// sendNotification отправляет уведомление о новом платеже в CarpinteroCh
func sendNotification(userID uint64, new bool, paymentRequest PaymentRequest) {
	paymentRequestJSON, _ := json.Marshal(paymentRequest)
	msg := com.CarpCh{
		Event:      "usdt_pay",
		UserName:   strconv.FormatBool(new),
		AssistName: "init",
		Target:     string(paymentRequestJSON),
		UserID:     userID,
	}

	// Отправляем сообщение в канал для дальнейшей обработки
	select {
	case mode.GetCarpinteroChannel() <- msg:
	default:
		logger.Warn("Ошибка отправки сообщения в канал CarpinteroCh: канал переполнен", userID)
	}
}

// GetPaymentStatus получает статус платежа через интерфейс DB
func (ps *PaymentService) GetPaymentStatus(orderID string) (json.RawMessage, error) {
	return ps.db.GetCryptoPaymentStatus(ps.ctx, orderID)
}

// UpdatePaymentStatus обновляет статус платежа через интерфейс DB
func (ps *PaymentService) UpdatePaymentStatus(orderID, status, txHash string, receivedAmount float64, confirmations int) error {
	err := ps.db.UpdateCryptoPaymentStatus(ps.ctx, orderID, status, txHash, receivedAmount, confirmations)
	if err != nil {
		return err
	}

	//logger.Info("Updated payment status: orderID=%s, status=%s, confirmations=%d",
	//	orderID, status, confirmations)

	return nil
}

// GetPendingPayments получает список платежей в ожидании через интерфейс DB
func (ps *PaymentService) GetPendingPayments() (json.RawMessage, error) {
	return ps.db.GetPendingCryptoPayments(ps.ctx)
}

// CheckExpiredPayments проверяет и помечает истекшие платежи через интерфейс DB
func (ps *PaymentService) CheckExpiredPayments() error {
	err := ps.db.CheckExpiredCryptoPayments(ps.ctx)
	if err != nil {
		return err
	}

	//logger.Debug("Checked and marked expired payments")
	return nil
}

// GetSupportedCurrencies получает список поддерживаемых валют через bybit API
func (ps *PaymentService) GetSupportedCurrencies() ([]SupportedCurrency, error) {
	return ps.bybitClient.GetSupportedCurrencies()
}

// GetDepositAddress получает адрес депозита по orderID
func (ps *PaymentService) GetDepositAddress(orderID string) (string, error) {
	return ps.db.GetCryptoPaymentDepositAddress(ps.ctx, orderID)
}

// GetPaymentCreatedAt получает время создания платежа по orderID
func (ps *PaymentService) GetPaymentCreatedAt(orderID string) (int64, error) {
	return ps.db.GetCryptoPaymentCreatedAt(ps.ctx, orderID)
}

// CheckIfTxHashAlreadyUsed проверяет, не был ли txHash уже использован другим платежом
func (ps *PaymentService) CheckIfTxHashAlreadyUsed(txHash string) (string, error) {
	return ps.db.CheckIfTxHashAlreadyUsed(ps.ctx, txHash)
}

// FindActivePaymentByAddress ищет активный платеж по адресу депозита
func (ps *PaymentService) FindActivePaymentByAddress(userID uint64, currency, network, address string) (string, error) {
	return ps.db.FindActivePaymentByAddress(ps.ctx, userID, currency, network, address)
}

// GetActivePaymentsByUserAndCurrency получает все активные платежи пользователя по валюте и сети
func (ps *PaymentService) GetActivePaymentsByUserAndCurrency(userID uint64, currency, network string) (json.RawMessage, error) {
	return ps.db.GetActivePaymentsByUserAndCurrency(ps.ctx, userID, currency, network)
}

// UpdatePaymentExpiration продлевает срок действия платежа
func (ps *PaymentService) UpdatePaymentExpiration(orderID string, newExpiresAt int64) error {
	return ps.db.UpdatePaymentExpiration(ps.ctx, orderID, newExpiresAt)
}

// GetCurrencyPrice получает актуальную цену валюты (делегирует к bybit клиенту)
func (ps *PaymentService) GetCurrencyPrice(currency string) (float64, error) {
	return ps.bybitClient.GetCurrencyPrice(currency)
}

// GetDepositAddressDirect получает адрес депозита напрямую через bybit клиент
func (ps *PaymentService) GetDepositAddressDirect(currency, network string) (*DepositAddress, error) {
	return ps.bybitClient.GetDepositAddress(currency, network)
}
