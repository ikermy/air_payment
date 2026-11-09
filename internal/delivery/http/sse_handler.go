package http

import (
	"air_payment/internal/metrics"
	"air_payment/internal/processor/bybit"
	"bufio"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ikermy/air-logger/v2/pkg/logger"
)

// SSEManager управляет Server-Sent Events соединениями
type SSEManager struct {
	mu          sync.RWMutex
	connections map[string]map[*fiber.Ctx]*SSEConnection // orderID -> connections с каналами
}

// SSEConnection хранит информацию о SSE соединении
type SSEConnection struct {
	ctx       *fiber.Ctx
	messageCh chan string   // канал для передачи сообщений в heartbeat цикл
	closeCh   chan struct{} // канал для закрытия соединения
	startedAt time.Time
}

// Убеждаемся, что SSEManager реализует WebSocketManagerInterface из bybit пакета
var _ bybit.WebSocketManagerInterface = (*SSEManager)(nil)

// NewSSEManager создает новый менеджер SSE
func NewSSEManager() *SSEManager {
	return &SSEManager{
		connections: make(map[string]map[*fiber.Ctx]*SSEConnection),
	}
}

// AddConnection добавляет SSE соединение
func (sm *SSEManager) AddConnection(orderID string, c *fiber.Ctx, messageCh chan string, closeCh chan struct{}) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.connections[orderID] == nil {
		sm.connections[orderID] = make(map[*fiber.Ctx]*SSEConnection)
	}
	sm.connections[orderID][c] = &SSEConnection{
		ctx:       c,
		messageCh: messageCh,
		closeCh:   closeCh,
		startedAt: time.Now(),
	}
	metrics.SSEConnections.Inc()

	logger.Info("Added SSE connection for order %s (total connections: %d)", orderID, len(sm.connections[orderID]))
}

// RemoveConnection удаляет SSE соединение
func (sm *SSEManager) RemoveConnection(orderID string, c *fiber.Ctx) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if connections, exists := sm.connections[orderID]; exists {
		if _, exists := connections[c]; exists {
			metrics.SSEDuration.WithLabelValues("closed").Observe(time.Since(connections[c].startedAt).Seconds())
			delete(connections, c)
			metrics.SSEConnections.Dec()
		}
		if len(connections) == 0 {
			delete(sm.connections, orderID)
			logger.Debug("Removed empty connection pool for order %s", orderID)
		}
	} else {
		logger.Warn("Attempted to remove non-existent SSE connection for order %s", orderID)
	}
}

// GetActiveOrderIDs возвращает список всех orderID с активными SSE соединениями
func (sm *SSEManager) GetActiveOrderIDs() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var orderIDs []string
	for orderID, connections := range sm.connections {
		if len(connections) > 0 {
			orderIDs = append(orderIDs, orderID)
		}
	}

	return orderIDs
}

// FindActiveOrderIDByAddress ищет orderID с активными SSE соединениями по адресу депозита
func (sm *SSEManager) FindActiveOrderIDByAddress(paymentService PaymentServiceInterface, userID uint64, address string) (string, error) {
	activeOrderIDs := sm.GetActiveOrderIDs()

	//logger.Debug("SSE Manager: Checking %d active order IDs for matching address", len(activeOrderIDs))

	for _, orderID := range activeOrderIDs {
		// Получаем адрес депозита для каждого активного orderID
		orderAddress, err := paymentService.GetDepositAddress(orderID)
		if err != nil {
			logger.Debug("SSE Manager: Failed to get address for order %s: %v", orderID, err, userID)
			continue
		}

		// Проверяем, совпадает ли адрес
		if orderAddress == address {
			logger.Info("SSE Manager: Found active SSE connection for order %s with matching address %s", orderID, address, userID)
			return orderID, nil
		}
	}

	//logger.Debug("SSE Manager: No active SSE connection found for address %s", address, userID)
	return "", nil
}

// convertTimeFieldsToUnix конвертирует поля времени в Unix timestamp для корректной обработки на клиенте
func convertTimeFieldsToUnix(data map[string]interface{}) {
	// Список полей, которые могут содержать время в формате MySQL DATETIME
	timeFields := []string{"createdAt", "expiresAt", "updatedAt", "confirmedAt"}

	// Регулярное выражение для формата MySQL DATETIME: YYYY-MM-DD HH:mm:ss
	mysqlDateTimeRegex := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)

	for _, field := range timeFields {
		if value, exists := data[field]; exists {
			if timeStr, ok := value.(string); ok && mysqlDateTimeRegex.MatchString(timeStr) {
				// Парсим время как локальное время сервера (не UTC!)
				// MySQL DATETIME хранится без таймзоны, поэтому интерпретируем как локальное время
				if parsedTime, err := time.ParseInLocation("2006-01-02 15:04:05", timeStr, time.Local); err == nil {
					// Конвертируем в Unix timestamp
					data[field+"Unix"] = parsedTime.Unix()
					// Оставляем исходное поле для обратной совместимости
					data[field] = timeStr
				} else {
					logger.Error("Failed to parse time field %s: %s, error: %v", field, timeStr, err)
				}
			}
		}
	}
}

// BroadcastPaymentUpdate отправляет обновление статуса платежа всем подписчикам
func (sm *SSEManager) BroadcastPaymentUpdate(orderID string, paymentStatus interface{}) {
	sm.mu.RLock()
	connections := sm.connections[orderID]
	connectionsCount := len(connections)
	sm.mu.RUnlock()

	if connectionsCount == 0 {
		//logger.Debug("No active SSE connections for order %s", orderID)
		return
	}

	data, err := json.Marshal(paymentStatus)
	if err != nil {
		logger.Error("Failed to marshal payment status for order %s: %v", orderID, err)
		return
	}

	// Парсим JSON для конвертации полей времени
	var statusMap map[string]interface{}
	if err := json.Unmarshal(data, &statusMap); err == nil {
		// Конвертируем поля времени в Unix timestamp
		convertTimeFieldsToUnix(statusMap)

		// Перемаршалируем с конвертированными полями времени
		if updatedData, err := json.Marshal(statusMap); err == nil {
			data = updatedData
		}
	}

	// Определяем тип события
	var eventType = "payment_update"
	if statusMap != nil {
		if isPartial, exists := statusMap["isPartialPayment"]; exists && isPartial.(bool) {
			eventType = "partial_payment"
		}
		if status, exists := statusMap["status"].(string); exists {
			switch status {
			case "confirmed":
				eventType = "payment_confirmed"
			case "failed":
				eventType = "payment_failed"
			case "partial":
				eventType = "partial_payment"
			case "pending":
				eventType = "payment_pending"
			}
		}
	}

	// Формируем SSE-сообщение. Поле event обязательно: фронтенд подписывается
	// на partial_payment/payment_confirmed и другие типы через addEventListener.
	message := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(data))

	logger.Info("Broadcasting payment update to %d SSE connections (order %s): %s",
		connectionsCount, orderID, eventType)

	// Отправляем обновление всем активным соединениям
	sm.mu.RLock()
	connectionsList := make(map[*fiber.Ctx]bool)
	for conn := range connections {
		connectionsList[conn] = true
	}
	sm.mu.RUnlock()

	// Используем существующий метод sendToConnections для отправки
	sm.sendToConnections(orderID, connectionsList, message, eventType)
}

// sendToConnections вспомогательная функция для отправки сообщений всем соединениям
func (sm *SSEManager) sendToConnections(orderID string, connections map[*fiber.Ctx]bool, message string, eventType string) {
	sm.mu.RLock()
	sseConnections := sm.connections[orderID]
	sm.mu.RUnlock()

	var deadConnections []*fiber.Ctx
	successCount := 0

	for conn := range connections {
		if conn == nil {
			deadConnections = append(deadConnections, conn)
			continue
		}

		// Ищем SSEConnection для данного контекста
		if sseConn, exists := sseConnections[conn]; exists && sseConn != nil {
			// Отправляем сообщение через канал
			select {
			case sseConn.messageCh <- message:
				successCount++
				metrics.ObserveSSEMessage(eventType, "success")
				logger.Debug("SSE message queued for connection in order %s", orderID)
			default:
				metrics.ObserveSSEMessage(eventType, "error")
				metrics.ObserveSSEDrop("channel_full")
				// Канал заблокирован, соединение мертвое
				deadConnections = append(deadConnections, conn)
				logger.Debug("SSE message channel blocked for order %s, marking connection as dead", orderID)
			}
		} else {
			// Соединение не найдено, помечаем как мертвое
			deadConnections = append(deadConnections, conn)
		}
	}

	logger.Info("Sent SSE %s to %d/%d connections for order %s",
		eventType, successCount, len(connections), orderID)

	// Безопасное удаление мертвых соединений
	if len(deadConnections) > 0 {
		sm.mu.Lock()
		actuallyRemoved := 0
		for _, conn := range deadConnections {
			if _, exists := sm.connections[orderID][conn]; exists {
				// Закрываем канал закрытия для соединения
				if sseConn := sm.connections[orderID][conn]; sseConn != nil {
					select {
					case sseConn.closeCh <- struct{}{}:
					default:
					}
				}
				delete(sm.connections[orderID], conn)
				actuallyRemoved++
			}
		}
		if len(sm.connections[orderID]) == 0 {
			delete(sm.connections, orderID)
			logger.Debug("Removed empty connection pool for order %s", orderID)
		}
		sm.mu.Unlock()

		if actuallyRemoved > 0 {
			logger.Debug("Removed %d dead SSE connections for order %s", actuallyRemoved, orderID)
		}
	}
}

// HandlePaymentStatusSSE обрабатывает SSE запросы для статуса платежей
func (w *Web) HandlePaymentStatusSSE(c *fiber.Ctx) error {
	// Извлекаем параметры из query string
	orderID := c.Query("orderId")

	logger.Debug("SSE connection request: orderID=%s, token=%s", orderID)

	userID, ok := getUserID(c)
	if !ok {
		return nil
	}
	if orderID == "" {
		logger.Error("SSE request missing parameters: orderID=%s", orderID)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Token and orderId are required",
		})
	}

	// Проверяем, что платеж принадлежит пользователю
	statusJSON, err := w.paymentService.GetPaymentStatus(orderID)
	if err != nil {
		logger.Error("SSE payment not found: orderID=%s, error=%v", orderID, err)
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Payment not found",
		})
	}

	var paymentStatus map[string]interface{}
	if err := json.Unmarshal(statusJSON, &paymentStatus); err != nil {
		logger.Error("SSE failed to parse payment status: orderID=%s, error=%v", orderID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to parse payment status",
		})
	}

	// Проверяем владельца платежа
	if paymentUserID, ok := paymentStatus["userId"].(float64); !ok || uint64(paymentUserID) != userID {
		logger.Error("SSE access denied: orderID=%s, expectedUserID=%d, actualUserID=%v",
			orderID, userID, paymentStatus["userId"])
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Access denied",
		})
	}

	logger.Info("SSE connection authorized for userID=%d, orderID=%s", userID, orderID)

	// Устанавливаем SSE заголовки
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Access-Control-Allow-Origin", "*")
	c.Set("Access-Control-Allow-Headers", "Cache-Control")
	c.Set("X-Accel-Buffering", "no")

	// Создаем каналы для передачи сообщений и закрытия соединения
	messageCh := make(chan string)
	closeCh := make(chan struct{})

	// Добавляем соединение в менеджер
	w.sseManager.AddConnection(orderID, c, messageCh, closeCh)

	// ИСПРАВЛЕНО: Кардинально упрощаем логику без избыточных проверок
	c.Context().SetBodyStreamWriter(func(writer *bufio.Writer) {
		defer func() {
			// Убеждаемся, что соединение удаляется при выходе
			w.sseManager.RemoveConnection(orderID, c)
			logger.Debug("SSE connection closed for orderID=%s", orderID)
		}()

		// Конвертируем поля времени в Unix timestamp для начального статуса
		convertTimeFieldsToUnix(paymentStatus)

		// Отправляем начальный статус
		currentData, _ := json.Marshal(paymentStatus)
		initialMessage := fmt.Sprintf("data: %s\n\n", currentData)

		if _, err := writer.WriteString(initialMessage); err != nil {
			logger.Error("SSE failed to write initial message for orderID=%s: %v", orderID, err)
			return
		}

		if err := writer.Flush(); err != nil {
			logger.Error("SSE failed to flush initial message for orderID=%s: %v", orderID, err)
			return
		}

		// Поддерживаем соединение с heartbeat - УПРОЩЕННАЯ ВЕРСИЯ
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		// ИСПРАВЛЕНО: Убираем все избыточные проверки, полагаемся только на ошибки записи
		// ИСПРАВЛЕНО: Heartbeat цикл теперь обрабатывает как heartbeat, так и внешние сообщения
		for {
			select {
			case <-ticker.C:
				// Отправляем heartbeat с текущим временем сервера для диагностики
				serverTime := time.Now()
				heartbeat := fmt.Sprintf("data: {\"type\": \"heartbeat\", \"timestamp\": %d, \"serverTimeString\": \"%s\", \"serverTimeZone\": \"%s\"}\n\n",
					serverTime.Unix(),
					serverTime.Format("2006-01-02 15:04:05"),
					serverTime.Location().String())

				if _, err := writer.WriteString(heartbeat); err != nil {
					logger.Info("SSE heartbeat write failed for orderID=%s (client disconnected): %v", orderID, err)
					return
				}

				if err := writer.Flush(); err != nil {
					logger.Info("SSE heartbeat flush failed for orderID=%s (client disconnected): %v", orderID, err)
					return
				}

				logger.Debug("SSE heartbeat sent successfully for orderID=%s (server time: %s)", orderID, serverTime.Format("2006-01-02 15:04:05"))

			case message := <-messageCh:
				// Получили внешнее сообщение (например, обновление платежа)
				if _, err := writer.WriteString(message); err != nil {
					logger.Info("SSE message write failed for orderID=%s (client disconnected): %v", orderID, err)
					return
				}

				if err := writer.Flush(); err != nil {
					logger.Info("SSE message flush failed for orderID=%s (client disconnected): %v", orderID, err)
					return
				}

				logger.Debug("SSE external message sent successfully for orderID=%s", orderID)

			case <-closeCh:
				// Получили сигнал о закрытии соединения
				logger.Info("SSE connection closing for orderID=%s (received close signal)", orderID)
				return
			}
		}
	})

	return nil
}
