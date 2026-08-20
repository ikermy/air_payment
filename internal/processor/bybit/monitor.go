package bybit

import (
	"air_payment/internal/db"
	"air_payment/internal/metrics"
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ikermy/air_common/pkg/com"
	"github.com/ikermy/air_common/pkg/mode"
	"github.com/ikermy/air_logger/v2/pkg/logger"
)

// SSEManager интерфейс для работы с SSE соединениями в monitor.go
type SSEManager struct {
	mu          sync.RWMutex
	connections map[string]map[*fiber.Ctx]bool
}

// PaymentMonitor отслеживает статус криптоплатежей
type PaymentMonitor struct {
	db             DB
	paymentService *PaymentService
	bybitClient    *Client
	wsManager      WebSocketManagerInterface // Добавляем интерфейс для WebSocket менеджера
	ctx            context.Context
	cancel         context.CancelFunc
}

// WebSocketManagerInterface интерфейс для SSE/WebSocket менеджера
type WebSocketManagerInterface interface {
	BroadcastPaymentUpdate(orderID string, paymentStatus interface{})
}

// NewPaymentMonitor создает новый монитор платежей
func NewPaymentMonitor(paymentService *PaymentService) *PaymentMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &PaymentMonitor{
		paymentService: paymentService,
		bybitClient:    paymentService.bybitClient,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// SetSSEManager устанавливает SSE менеджер (совместим с WebSocketManagerInterface)
func (pm *PaymentMonitor) SetSSEManager(sseManager WebSocketManagerInterface) {
	pm.wsManager = sseManager
}

// Start запускает мониторинг платежей
func (pm *PaymentMonitor) Start(d *db.DB) {
	pm.db = d // Инициализируем DB
	logger.Info("Starting payment monitor...")

	// Запускаем горутину для проверки платежей каждые 30 секунд
	go pm.monitorPayments()

	// Запускаем горутину для очистки истекших платежей каждые 5 минут
	go pm.cleanupExpiredPayments()
}

// Stop останавливает мониторинг
func (pm *PaymentMonitor) Stop() {
	logger.Info("Stopping payment monitor...")
	pm.cancel()
}

// monitorPayments основной цикл мониторинга
func (pm *PaymentMonitor) monitorPayments() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			pm.checkPendingPayments()
		}
	}
}

// cleanupExpiredPayments очищает истекшие платежи
func (pm *PaymentMonitor) cleanupExpiredPayments() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			err := pm.paymentService.CheckExpiredPayments()
			if err != nil {
				metrics.ObserveMonitorError("cleanup_expired_payments")
				logger.Error("Failed to cleanup expired payments: %v", err)
			}
		}
	}
}

// checkPendingPayments проверяет статус всех ожидающих платежей
func (pm *PaymentMonitor) checkPendingPayments() {
	startedAt := time.Now()
	paymentsJSON, err := pm.paymentService.GetPendingPayments()
	if err != nil {
		metrics.ObserveMonitorError("get_pending_payments")
		metrics.ObserveMonitorCycle("error", time.Since(startedAt).Seconds())
		logger.Error("Failed to get pending payments: %v", err)
		return
	}

	// Парсим JSON в массив структур PaymentStatus
	var payments []PaymentStatus
	if err := json.Unmarshal(paymentsJSON, &payments); err != nil {
		metrics.ObserveMonitorError("parse_pending_payments")
		metrics.ObserveMonitorCycle("error", time.Since(startedAt).Seconds())
		logger.Error("Failed to parse pending payments JSON: %v", err)
		return
	}
	metrics.SetActivePayments("pending", float64(len(payments)))

	//logger.Debug("Monitor checking %d pending payments", len(payments))
	for i := range payments {
		//logger.Debug("Checking payment: orderID=%s, status=%s, amount=%.6f",
		//	payments[i].OrderID, payments[i].Status, payments[i].Amount)
		pm.checkSinglePayment(&payments[i])
	}
	metrics.ObserveMonitorCycle("success", time.Since(startedAt).Seconds())
}

// checkSinglePayment проверяет отдельный платеж
func (pm *PaymentMonitor) checkSinglePayment(payment *PaymentStatus) {
	// Получаем историю депозитов для данного адреса
	deposits, err := pm.getDepositHistory(payment.Currency, payment.OrderID)
	if err != nil {
		logger.Error("Failed to get deposit history for order %s: %v", payment.OrderID, err)
		return
	}

	// Проверяем, есть ли новые депозиты
	for _, deposit := range deposits {
		if pm.isNewDeposit(payment, &deposit) {
			pm.updatePaymentFromDeposit(payment, &deposit)
		}
	}
}

// DepositRecord запись о депозите из bybit API
type DepositRecord struct {
	Coin          string `json:"coin"`
	Chain         string `json:"chain"`
	Amount        string `json:"amount"`
	TxID          string `json:"txID"`
	Status        int    `json:"status"` // 0=pending, 1=processing, 2=success, 3=deposit failed
	ToAddress     string `json:"toAddress"`
	Tag           string `json:"tag"`
	DepositFee    string `json:"depositFee"`
	SuccessAt     string `json:"successAt"`
	Confirmations string `json:"confirmations"`
	TxIndex       string `json:"txIndex"`
	BlockHash     string `json:"blockHash"`
}

// getDepositHistory получает историю депозитов
func (pm *PaymentMonitor) getDepositHistory(currency, orderID string) ([]DepositRecord, error) {
	// Получаем адрес платежа из БД через интерфейс DB
	address, err := pm.paymentService.db.GetCryptoPaymentDepositAddress(pm.ctx, orderID)
	if err != nil {
		return nil, err
	}

	// Используем новый метод библиотеки для получения истории депозитов
	startTime := time.Now().Add(-1 * time.Hour).UnixMilli()
	endTime := time.Now().UnixMilli()
	limit := 50

	allDeposits, err := pm.bybitClient.GetDepositHistory(currency, startTime, endTime, limit)
	if err != nil {
		metrics.ObserveBybit("get_deposit_history", "error")
		return nil, err
	}
	metrics.ObserveBybit("get_deposit_history", "success")

	// Фильтруем депозиты только для нашего адреса
	var filteredDeposits []DepositRecord
	for _, deposit := range allDeposits {
		if deposit.ToAddress == address {
			filteredDeposits = append(filteredDeposits, deposit)
		}
	}

	return filteredDeposits, nil
}

// isNewDeposit проверяет, является ли депозит новым
func (pm *PaymentMonitor) isNewDeposit(payment *PaymentStatus, deposit *DepositRecord) bool {
	// Если у нас уже есть этот txHash, то это не новый депозит
	if payment.TxHash == deposit.TxID {
		return false
	}

	// КРИТИЧЕСКИ ВАЖНО: Проверяем, не был ли этот txHash уже использован другим платежом
	usedByOrderID, err := pm.paymentService.CheckIfTxHashAlreadyUsed(deposit.TxID)
	if err != nil {
		logger.Error("Failed to check if txHash already used for %s: %v", deposit.TxID, err)
		// В случае ошибки продолжаем проверку, чтобы не заблокировать обработку
	} else if usedByOrderID != "" && usedByOrderID != payment.OrderID {
		// Если txHash уже использован другим платежом, игнорируем этот депозит
		//logger.Debug("Ignoring deposit %s: already used by order %s (current order: %s)",
		//	deposit.TxID, usedByOrderID, payment.OrderID)
		return false
	}

	// Если у нас нет txHash, но есть received_amount и он совпадает, скорее всего тот же депозит
	amount, _ := strconv.ParseFloat(deposit.Amount, 64)
	if payment.TxHash == "" && payment.ReceivedAmount == amount {
		return false
	}

	// Проверяем время успешного депозита относительно времени создания платежа
	// Получаем время создания платежа
	paymentCreatedAt, err := pm.paymentService.GetPaymentCreatedAt(payment.OrderID)
	if err != nil {
		logger.Error("Failed to get payment created time for order %s: %v", payment.OrderID, err)
		// В случае ошибки лучше считать депозит новым, чем пропустить реальный платеж
		return true
	}

	// Парсим время успешного депозита (SuccessAt в формате timestamp или пустая строка)
	var depositSuccessTime int64
	if deposit.SuccessAt != "" && deposit.SuccessAt != "0" {
		// Пытаемся парсить как timestamp в миллисекундах
		depositSuccessTime, err = strconv.ParseInt(deposit.SuccessAt, 10, 64)
		if err != nil {
			logger.Debug("Failed to parse deposit SuccessAt time %s: %v", deposit.SuccessAt, err)
			// Если не удалось парсить, используем текущее время как fallback
			depositSuccessTime = time.Now().Unix() * 1000
		}
	} else {
		// Если SuccessAt пустое, используем текущее время
		depositSuccessTime = time.Now().Unix() * 1000
	}

	// Конвертируем время создания платежа в миллисекунды для сравнения
	paymentCreatedAtMs := paymentCreatedAt * 1000

	// Если депозит был успешно обработан ДО создания платежа, это старый депозит
	if depositSuccessTime < paymentCreatedAtMs {
		//logger.Debug("Ignoring old deposit: deposit_time=%d, payment_created=%d, txID=%s",
		//	depositSuccessTime, paymentCreatedAtMs, deposit.TxID)
		return false
	}

	//logger.Debug("New deposit detected: deposit_time=%d, payment_created=%d, txID=%s, order=%s",
	//	depositSuccessTime, paymentCreatedAtMs, deposit.TxID, payment.OrderID)

	return true
}

// updatePaymentFromDeposit обновляет платеж на основе информации о депозите
func (pm *PaymentMonitor) updatePaymentFromDeposit(payment *PaymentStatus, deposit *DepositRecord) {
	amount, _ := strconv.ParseFloat(deposit.Amount, 64)
	confirmations, _ := strconv.Atoi(deposit.Confirmations)

	var status string
	// Правильная интерпретация статусов согласно документации bybit:
	// 0=unknown, 1=toBeConfirmed, 2=processing, 3=success, 4=deposit failed
	switch deposit.Status {
	case 0: // unknown
		status = "pending"
	case 1: // toBeConfirmed
		status = "pending"
	case 2: // processing
		if amount >= payment.Amount {
			status = "pending" // Ждем финального подтверждения
		} else {
			status = "partial"
		}
	case 3: // success (finalised status) - ИСПРАВЛЕНО!
		// Статус 3 = SUCCESS согласно документации
		if amount >= payment.Amount {
			status = "confirmed"
		} else {
			status = "partial"
		}
	case 4: // deposit failed
		status = "failed"
	case 10011: // pending to be credited to funding pool
		status = "pending"
	case 10012: // Credited to funding pool successfully
		if amount >= payment.Amount {
			status = "confirmed"
		} else {
			status = "partial"
		}
	default:
		status = "pending"
	}

	// Дополнительная проверка по количеству подтверждений
	// Если много подтверждений, приоритизируем это над API статусом
	if confirmations >= 12 && amount > 0 {
		if amount >= payment.Amount {
			status = "confirmed"
		} else {
			status = "partial"
		}
	}

	// Обновляем статус в БД
	err := pm.paymentService.UpdatePaymentStatus(
		payment.OrderID,
		status,
		deposit.TxID,
		amount,
		confirmations,
	)

	if err != nil {
		logger.Error("Failed to update payment status for order %s: %v", payment.OrderID, err)
		return
	}
	metrics.ObserveStatusChange(payment.Status, status, payment.Currency)

	//logger.Info("Updated payment %s: status=%s, amount=%f, confirmations=%d (api_status=%d)",
	//	payment.OrderID, status, amount, confirmations, deposit.Status)

	// ИСПРАВЛЕНО: Проверяем обновленный статус, а не старый
	if status == "confirmed" {
		// Обновляем payment.Status для правильной работы логики
		payment.Status = status
		// Обновляем информацию о состоянии платежа
		go pm.onPayment(payment)
	}

	// Отправляем real-time обновление через SSE с дополнительной информацией о сумме
	pm.broadcastPaymentUpdateWithAmount(payment.OrderID, amount, status, confirmations)
}

// broadcastPaymentUpdate отправляет обновление платежа через SSE
func (pm *PaymentMonitor) broadcastPaymentUpdate(orderID string) {
	if pm.wsManager != nil {
		// Получаем обновленный статус из БД
		updatedStatusJSON, err := pm.paymentService.GetPaymentStatus(orderID)
		if err == nil {
			var updatedStatus interface{}
			if err := json.Unmarshal(updatedStatusJSON, &updatedStatus); err == nil {
				pm.wsManager.BroadcastPaymentUpdate(orderID, updatedStatus)
				logger.Info("Sent SSE update for payment %s", orderID)
			} else {
				logger.Error("Failed to unmarshal payment status for SSE: %v", err)
			}
		} else {
			logger.Error("Failed to get payment status for SSE: %v", err)
		}
	} else {
		logger.Warn("SSE manager not set, cannot send real-time updates")
	}
}

// broadcastPaymentUpdateWithAmount отправляет обновление платежа с дополнительной информацией о сумме
func (pm *PaymentMonitor) broadcastPaymentUpdateWithAmount(orderID string, receivedAmount float64, status string, confirmations int) {
	if pm.wsManager != nil {
		// Получаем обновленный статус из БД
		updatedStatusJSON, err := pm.paymentService.GetPaymentStatus(orderID)
		if err == nil {
			var updatedStatus map[string]interface{}
			if err := json.Unmarshal(updatedStatusJSON, &updatedStatus); err == nil {
				// Добавляем дополнительную информацию о частичном поступлении
				updatedStatus["currentReceivedAmount"] = receivedAmount
				updatedStatus["currentStatus"] = status
				updatedStatus["currentConfirmations"] = confirmations
				updatedStatus["lastUpdateTime"] = time.Now().Unix()
				updatedStatus["isPartialPayment"] = true // Флаг для клиента

				// ИСПРАВЛЕНО: Проверяем статус платежа и очищаем SSE соединения для завершенных платежей
				if status == "confirmed" || status == "failed" || status == "expired" {
					logger.Info("Payment %s reached final status %s, will clean up SSE connections after broadcast", orderID, status)

					// Отправляем финальное обновление
					pm.wsManager.BroadcastPaymentUpdate(orderID, updatedStatus)
					logger.Info("Sent final SSE update for payment %s: status=%s, amount=%.6f, confirmations=%d",
						orderID, status, receivedAmount, confirmations)

					// Планируем очистку SSE соединений через короткую задержку
					// чтобы клиенты успели получить финальное сообщение
					go func() {
						time.Sleep(2 * time.Second)
						logger.Info("Scheduled cleanup completed for payment %s with final status %s", orderID, status)
					}()

					return
				}

				// Отправляем ТОЛЬКО расширенное обновление (убираем дублирование)
				pm.wsManager.BroadcastPaymentUpdate(orderID, updatedStatus)
				//logger.Info("Sent enhanced SSE update for payment %s: status=%s, amount=%.6f, confirmations=%d",
				//	orderID, status, receivedAmount, confirmations)
			} else {
				logger.Error("Failed to unmarshal payment status for enhanced SSE: %v", err)
				// В случае ошибки отправляем базовое обновление
				pm.broadcastPaymentUpdate(orderID)
			}
		} else {
			logger.Error("Failed to get payment status for enhanced SSE: %v", err)
			// В случае ошибки отправляем базовое обновление
			pm.broadcastPaymentUpdate(orderID)
		}
	} else {
		logger.Warn("SSE manager not set, cannot send real-time updates")
	}
}

type UserTariff struct {
	Currency  string             `json:"Currency"`
	MonthCost float64            `json:"MonthCost"`
	Discounts map[string]float64 `json:"Discounts"`
}

// onPayment вызывается при наступлении события платежа
func (pm *PaymentMonitor) onPayment(payment *PaymentStatus) {
	logger.Info("Платёж подтвержден: OrderID=%s, Amount=%.6f, Currency=%s",
		payment.OrderID, payment.Amount, payment.Currency, payment.UserID)

	data, err := pm.db.GetUserTariff(pm.ctx, payment.UserID)
	if err != nil {
		logger.Error("Ошибка получения тарифа %v:", err, payment.UserID)
		return
	}

	var tariff UserTariff
	if err := json.Unmarshal(data, &tariff); err != nil {
		logger.Error("Ошибка парсинга тарифа: %v", err, payment.UserID)
		return
	}

	mes, discount := calcDiscount(payment.Amount, tariff.MonthCost, tariff.Discounts)

	if mes > 0 {
		err := pm.db.SetBillingData(pm.ctx, payment.UserID, payment.Amount, mes, discount)
		if err != nil {
			logger.Error("Ошибка обновления: %v", err, payment.UserID)
			return
		}

		logger.Info("Обновлен биллинг: +%d месяцев, скидка %.2f, сумма %.6f",
			mes, discount, payment.Amount, payment.UserID)
	}

	// Отправляю уведомление о статусе платежа
	paymentNotification(payment)
}

func calcDiscount(amount, monthCost float64, discounts map[string]float64) (uint8, float64) {
	var months []int
	for k := range discounts {
		m, _ := strconv.Atoi(k)
		months = append(months, m)
	}
	sort.Ints(months)

	var (
		maxMonths   uint8
		maxDiscount float64
	)

	for _, m := range months {
		discount := discounts[strconv.Itoa(m)]
		totalCost := monthCost*float64(m) - discount
		if amount >= totalCost && uint8(m) > maxMonths {
			maxMonths = uint8(m)
			maxDiscount = discount
		}
	}

	// Проверяем, можно ли купить месяцы без скидки
	withoutDiscount := uint8(amount / monthCost)
	if withoutDiscount > maxMonths {
		maxMonths = withoutDiscount
		maxDiscount = 0
	}

	return maxMonths, maxDiscount
}

// paymentNotification вызывается при подтверждении платежа
func paymentNotification(payment *PaymentStatus) {
	logger.Debug("Отправка уведомления о статусе платежа %s", payment.OrderID, payment.UserID)
	paymentRequestJSON, _ := json.Marshal(payment)
	msg := com.CarpCh{
		Event:      "usdt_pay",
		UserName:   "false",
		AssistName: payment.Status,
		Target:     string(paymentRequestJSON),
		UserID:     payment.UserID,
	}

	// Отправляем сообщение в канал для дальнейшей обработки
	select {
	case mode.GetCarpinteroChannel() <- msg:
	default:
		logger.Warn("Ошибка отправки сообщения в канал CarpinteroCh: канал переполнен", payment.UserID)
	}
}
