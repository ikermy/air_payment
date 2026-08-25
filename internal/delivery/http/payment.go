package http

import (
	"air_payment/internal/metrics"
	"bytes"
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ikermy/air-logger/v2/pkg/logger"
)

func (w *Web) AvailableService(c *fiber.Ctx) error {
	return c.SendStatus(fiber.StatusOK)
}

// CreateBybitPayment создает платеж через bybit Pay
func (w *Web) CreateBybitPayment(c *fiber.Ctx) error {
	startedAt := time.Now()
	defer func() { metrics.ObservePaymentDuration("create_payment", time.Since(startedAt).Seconds()) }()
	var req struct {
		UserID   uint32  `json:"user_id"`
		Currency string  `json:"currency"`
		Network  string  `json:"network"`
		Amount   float64 `json:"amount"`
	}
	if err := c.BodyParser(&req); err != nil {
		logger.Error("'CreateBybitPayment' JSON parse error: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if req.Currency == "" || req.Network == "" || req.Amount <= 0 {
		logger.Error("'CreateBybitPayment' JSON parse error: %v", req)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid payment data"})
	}

	// Сначала получаем адрес депозита для проверки существующих SSE соединений
	// Получаем курс валюты
	price, err := w.paymentService.GetCurrencyPrice(req.Currency)
	if err != nil {
		metrics.ObservePaymentOperation("get_currency_price", "error")
		logger.Error("'CreateBybitPayment' Failed to get currency price: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to get currency price"})
	}
	metrics.ObservePaymentOperation("get_currency_price", "success")

	// Получаем адрес для депозита
	depositAddressStruct, err := w.paymentService.GetDepositAddressDirect(req.Currency, req.Network)
	if err != nil {
		metrics.ObservePaymentOperation("get_deposit_address", "error")
		logger.Error("'CreateBybitPayment' Failed to get deposit address: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to get deposit address"})
	}
	metrics.ObservePaymentOperation("get_deposit_address", "success")

	//logger.Debug("CreateBybitPayment: Got deposit address %s for %s/%s", depositAddressStruct.Address, req.Currency, req.Network)

	// Проверяем, есть ли активные SSE соединения для этого адреса
	sseOrderID, err := w.sseManager.FindActiveOrderIDByAddress(w.paymentService, req.UserID, depositAddressStruct.Address)
	if err != nil {
		logger.Debug("CreateBybitPayment: Error checking SSE connections: %v", err)
	} else if sseOrderID != "" {
		// ДОПОЛНИТЕЛЬНАЯ ПРОВЕРКА: убеждаемся, что найденный платеж еще не истек
		existingStatusJSON, err := w.paymentService.GetPaymentStatus(sseOrderID)
		if err != nil {
			logger.Debug("CreateBybitPayment: Failed to get status of existing payment %s: %v", sseOrderID, err)
		} else {
			var existingStatus map[string]interface{}
			if err := json.Unmarshal(existingStatusJSON, &existingStatus); err == nil {
				// Проверяем статус и время истечения
				status := existingStatus["status"].(string)
				expiresAt, _ := existingStatus["expiresAt"].(float64)

				// Если платеж уже завершен или истек - не переиспользуем его
				if status == "confirmed" || status == "failed" || status == "expired" ||
					(expiresAt > 0 && time.Now().Unix() > int64(expiresAt)) {
					logger.Info("CreateBybitPayment: Found SSE connection for expired/completed payment %s (status=%s), creating new payment", sseOrderID, status)

					// Принудительно удаляем мертвое SSE соединение
					w.sseManager.mu.Lock()
					if connections, exists := w.sseManager.connections[sseOrderID]; exists {
						for conn := range connections {
							delete(connections, conn)
						}
						delete(w.sseManager.connections, sseOrderID)
					}
					w.sseManager.mu.Unlock()

					sseOrderID = "" // Сбрасываем, чтобы создать новый платеж
				} else {
					metrics.ObservePaymentReuse("reused")
					logger.Info("CreateBybitPayment: Found active SSE connection for order %s, reusing", sseOrderID)

					// Продлеваем срок действия найденного платежа
					newExpiresAt := time.Now().Add(30 * time.Minute).Unix()
					err = w.paymentService.UpdatePaymentExpiration(sseOrderID, newExpiresAt)
					if err != nil {
						logger.Error("CreateBybitPayment: Failed to extend payment expiration: %v", err)
					} else {
						logger.Info("CreateBybitPayment: Extended payment expiration for order %s", sseOrderID)
					}

					// Возвращаем данные существующего платежа
					amount := req.Amount / price
					return c.Status(fiber.StatusOK).JSON(fiber.Map{
						"orderId":        sseOrderID,
						"currency":       req.Currency,
						"network":        req.Network,
						"amount":         amount,
						"amountUsd":      req.Amount,
						"depositAddress": depositAddressStruct.Address,
						"depositTag":     depositAddressStruct.Tag,
						"qrCodeUri":      depositAddressStruct.Address,
						"expiresAt":      newExpiresAt,
						"reused":         true, // Флаг для клиента
					})
				}
			} else {
				logger.Debug("CreateBybitPayment: Failed to parse existing payment status: %v", err)
			}
		}
	}

	// Если нет активных SSE соединений, создаем обычный криптоплатеж
	pr, da, err := w.paymentService.CreatePaymentRequest(req.UserID, req.Currency, req.Network, req.Amount)
	if err != nil {
		metrics.ObservePaymentOperation("create_payment", "error")
		logger.Error("'CreateBybitPayment' Service error: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create payment"})
	}
	metrics.ObservePaymentOperation("create_payment", "success")
	metrics.ObservePaymentReuse("new")
	metrics.ObservePaymentStatus("created", req.Currency)
	metrics.ObservePaymentAmount(req.Currency, req.Amount)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"orderId":        pr.OrderID,
		"currency":       pr.Currency,
		"network":        pr.Network,
		"amount":         pr.Amount,
		"amountUsd":      pr.AmountUSD,
		"depositAddress": da.Address,
		"depositTag":     da.Tag,
		"qrCodeUri":      da.QRCode,
		"expiresAt":      pr.ExpiresAt,
		"reused":         false, // Новый платеж
	})
}

// GetPaymentStatus возвращает статус платежа для fallback-проверки с клиента.
func (w *Web) GetPaymentStatus(c *fiber.Ctx) error {
	orderID := c.Query("orderId")
	if orderID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "orderId is required"})
	}

	userID, ok := getUserID(c)
	if !ok {
		return nil
	}

	statusJSON, err := w.paymentService.GetPaymentStatus(orderID)
	if err != nil {
		metrics.ObservePaymentOperation("get_payment_status", "error")
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "payment not found"})
	}
	metrics.ObservePaymentOperation("get_payment_status", "success")

	var status map[string]interface{}
	if err := json.Unmarshal(statusJSON, &status); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "invalid payment status"})
	}

	paymentUserID, ok := status["userId"].(float64)
	if !ok || uint32(paymentUserID) != userID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "access denied"})
	}

	convertTimeFieldsToUnix(status)
	return c.Status(fiber.StatusOK).JSON(status)
}

func (w *Web) GetTariff(c *fiber.Ctx) error {
	userID, ok := getUserID(c)
	if !ok {
		return nil
	}

	// Получаю данные пользователя
	jsonRaw, err := w.db.GetUserTariff(w.ctx, userID)
	if err != nil {
		logger.Error("'GetTariff' Ошибка при получении данных пользователя: %v", err, userID)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var prettyJSON bytes.Buffer
	err = json.Indent(&prettyJSON, jsonRaw, "", "  ")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusOK).JSON(json.RawMessage(prettyJSON.Bytes()))
}
