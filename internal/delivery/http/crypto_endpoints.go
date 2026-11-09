package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/ikermy/air-logger/v2/pkg/logger"
)

// GetSupportedCurrencies возвращает список поддерживаемых криптовалют
func (w *Web) GetSupportedCurrencies(c *fiber.Ctx) error {
	userID, ok := getUserID(c)
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "uid is required"})
	}

	currencies, err := w.paymentService.GetSupportedCurrencies()
	if err != nil {
		logger.Error("'GetSupportedCurrencies' Service error for userID=%d: %v", userID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to get supported currencies"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"currencies": currencies,
	})
}

// GetDonationCurrencies возвращает список валют и сетей для пожертвований.
// Endpoint публичный: пожертвование не связано с пользователем.
func (w *Web) GetDonationCurrencies(c *fiber.Ctx) error {
	currencies, err := w.paymentService.GetSupportedCurrencies()
	if err != nil {
		logger.Error("'GetDonationCurrencies' Service error: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to get supported currencies"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"currencies": currencies})
}

// GetDonationAddress возвращает адрес для пожертвования без создания платежа.
func (w *Web) GetDonationAddress(c *fiber.Ctx) error {
	var req struct {
		Currency string `json:"currency"`
		Network  string `json:"network"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid donation data"})
	}
	if req.Currency == "" || req.Network == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "currency and network are required"})
	}

	address, err := w.paymentService.GetDepositAddressDirect(req.Currency, req.Network)
	if err != nil {
		logger.Error("'GetDonationAddress' Service error: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to get donation address"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"currency":       req.Currency,
		"network":        req.Network,
		"depositAddress": address.Address,
		"depositTag":     address.Tag,
		"qrCodeUri":      address.QRCode,
	})
}

// CalculatePayment выполняет предварительный расчет платежа
func (w *Web) CalculatePayment(c *fiber.Ctx) error {
	var req struct {
		UserId uint64  `json:"user_id"`
		Currency string  `json:"currency"`
		Network  string  `json:"network"`
		Amount   float64 `json:"amount"`
	}
	if err := c.BodyParser(&req); err != nil {
		logger.Error("'CalculatePayment' JSON parse error: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if req.Currency == "" || req.Network == "" || req.Amount <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid payment data"})
	}

	calculation, err := w.paymentService.CalculatePayment(req.Currency, req.Network, req.Amount)
	if err != nil {
		logger.Error("'CalculatePayment' Service error for userID=%d: %v", req.UserId, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to calculate payment"})
	}

	return c.Status(fiber.StatusOK).JSON(calculation)
}
