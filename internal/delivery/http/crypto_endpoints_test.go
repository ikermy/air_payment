package http

import (
	bybit2 "air_payment/internal/processor/bybit"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockDB struct {
	mock.Mock
}

func (m *MockDB) GetUserTariff(_ context.Context, userID uint64) (json.RawMessage, error) {
	args := m.Called(userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(json.RawMessage), args.Error(1)
}

type MockBybitClient struct {
	mock.Mock
}

func (m *MockBybitClient) GetSupportedCurrencies() ([]bybit2.SupportedCurrency, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]bybit2.SupportedCurrency), args.Error(1)
}

func (m *MockBybitClient) GetCurrencyPrice(currency string) (float64, error) {
	args := m.Called(currency)
	return args.Get(0).(float64), args.Error(1)
}

func (m *MockBybitClient) GetDepositAddress(currency, network string) (*bybit2.DepositAddress, error) {
	args := m.Called(currency, network)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*bybit2.DepositAddress), args.Error(1)
}

type TestablePaymentService struct {
	client *MockBybitClient
}

func (s *TestablePaymentService) GetSupportedCurrencies() ([]bybit2.SupportedCurrency, error) {
	return s.client.GetSupportedCurrencies()
}

func (s *TestablePaymentService) CalculatePayment(string, string, float64) (*bybit2.PaymentCalculation, error) {
	return nil, errors.New("not implemented in test")
}

func (s *TestablePaymentService) CreatePaymentRequest(uint64, string, string, float64) (*bybit2.PaymentRequest, *bybit2.DepositAddress, error) {
	return nil, nil, errors.New("not implemented in test")
}

func (s *TestablePaymentService) GetPaymentStatus(string) (json.RawMessage, error) {
	return nil, errors.New("not implemented in test")
}
func (s *TestablePaymentService) UpdatePaymentStatus(string, string, string, float64, int) error {
	return errors.New("not implemented in test")
}
func (s *TestablePaymentService) GetPendingPayments() (json.RawMessage, error) {
	return nil, errors.New("not implemented in test")
}
func (s *TestablePaymentService) CheckExpiredPayments() error {
	return errors.New("not implemented in test")
}
func (s *TestablePaymentService) GetDepositAddress(string) (string, error) {
	return "", errors.New("not implemented in test")
}
func (s *TestablePaymentService) GetPaymentCreatedAt(string) (int64, error) {
	return 0, errors.New("not implemented in test")
}
func (s *TestablePaymentService) CheckIfTxHashAlreadyUsed(string) (string, error) {
	return "", errors.New("not implemented in test")
}
func (s *TestablePaymentService) FindActivePaymentByAddress(uint64, string, string, string) (string, error) {
	return "", errors.New("not implemented in test")
}
func (s *TestablePaymentService) GetActivePaymentsByUserAndCurrency(uint64, string, string) (json.RawMessage, error) {
	return nil, errors.New("not implemented in test")
}
func (s *TestablePaymentService) UpdatePaymentExpiration(string, int64) error {
	return errors.New("not implemented in test")
}
func (s *TestablePaymentService) GetCurrencyPrice(currency string) (float64, error) {
	return s.client.GetCurrencyPrice(currency)
}
func (s *TestablePaymentService) GetDepositAddressDirect(currency, network string) (*bybit2.DepositAddress, error) {
	return s.client.GetDepositAddress(currency, network)
}
func (s *TestablePaymentService) GetBybitClient() *bybit2.Client { return nil }

func createTestWeb() (*Web, *MockDB, *MockBybitClient) {
	db := new(MockDB)
	client := new(MockBybitClient)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		if raw := c.Query("uid"); raw != "" {
			if uid, err := strconv.ParseUint(raw, 10, 32); err == nil {
				c.Locals(userIDLocalsKey, uint64(uid))
			}
		}
		return c.Next()
	})
	w := &Web{App: app, db: db, paymentService: &TestablePaymentService{client: client}}
	app.Get("/currencies", w.GetSupportedCurrencies)
	return w, db, client
}

func TestGetSupportedCurrenciesSuccess(t *testing.T) {
	w, _, client := createTestWeb()
	expected := []bybit2.SupportedCurrency{{Symbol: "BTC", Name: "Bitcoin", Network: "BTC", IsAvailable: true}}
	client.On("GetSupportedCurrencies").Return(expected, nil).Once()

	req := httptest.NewRequest("GET", "/currencies?uid=123", nil)
	resp, err := w.App.Test(req)

	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	client.AssertExpectations(t)
}

func TestGetSupportedCurrenciesRequiresUID(t *testing.T) {
	w, _, _ := createTestWeb()
	resp, err := w.App.Test(httptest.NewRequest("GET", "/currencies", nil))

	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestGetSupportedCurrenciesServiceError(t *testing.T) {
	w, _, client := createTestWeb()
	client.On("GetSupportedCurrencies").Return(nil, errors.New("bybit error")).Once()

	resp, err := w.App.Test(httptest.NewRequest("GET", "/currencies?uid=123", nil))

	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	client.AssertExpectations(t)
}
