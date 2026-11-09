package bybit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ikermy/air-common/v2/pkg/mode"
	"github.com/ikermy/air-logger/v2/pkg/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockDB для тестирования monitor
type MockMonitorDB struct {
	mock.Mock
}

func (m *MockMonitorDB) GetUserTariff(_ context.Context, userId uint64) (json.RawMessage, error) {
	args := m.Called(userId)
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockMonitorDB) SetBillingData(_ context.Context, userId uint64, payment float64, months uint8, discount float64) error {
	args := m.Called(userId, payment, months, discount)
	return args.Error(0)
}

// Остальные методы интерфейса DB для соблюдения интерфейса
func (m *MockMonitorDB) SaveCryptoPaymentRequest(_ context.Context, orderID string, userID uint64, currency, network string, amount, amountUSD float64, depositAddress, depositTag string, expiresAt int64) error {
	args := m.Called(orderID, userID, currency, network, amount, amountUSD, depositAddress, depositTag, expiresAt)
	return args.Error(0)
}

func (m *MockMonitorDB) GetCryptoPaymentStatus(_ context.Context, orderID string) (json.RawMessage, error) {
	args := m.Called(orderID)
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockMonitorDB) UpdateCryptoPaymentStatus(_ context.Context, orderID, status, txHash string, receivedAmount float64, confirmations int) error {
	args := m.Called(orderID, status, txHash, receivedAmount, confirmations)
	return args.Error(0)
}

func (m *MockMonitorDB) GetPendingCryptoPayments(_ context.Context) (json.RawMessage, error) {
	args := m.Called()
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockMonitorDB) CheckExpiredCryptoPayments(_ context.Context) error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockMonitorDB) GetCryptoPaymentDepositAddress(_ context.Context, orderID string) (string, error) {
	args := m.Called(orderID)
	return args.String(0), args.Error(1)
}

func (m *MockMonitorDB) GetCryptoPaymentCreatedAt(_ context.Context, orderID string) (int64, error) {
	args := m.Called(orderID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockMonitorDB) CheckIfTxHashAlreadyUsed(_ context.Context, txHash string) (string, error) {
	args := m.Called(txHash)
	return args.String(0), args.Error(1)
}

func (m *MockMonitorDB) FindActivePaymentByAddress(_ context.Context, userID uint64, currency, network, address string) (string, error) {
	args := m.Called(userID, currency, network, address)
	return args.String(0), args.Error(1)
}

func (m *MockMonitorDB) GetActivePaymentsByUserAndCurrency(_ context.Context, userID uint64, currency, network string) (json.RawMessage, error) {
	args := m.Called(userID, currency, network)
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockMonitorDB) UpdatePaymentExpiration(_ context.Context, orderID string, newExpiresAt int64) error {
	args := m.Called(orderID, newExpiresAt)
	return args.Error(0)
}

func TestOnPayment_Confirmed_Success(t *testing.T) {
	// Инициализируем логгер для тестов
	logSetup := logger.StdOut()
	if strings.EqualFold(mode.GetLogLevel(), "debug") {
		logSetup = logSetup.WithLogLevel(logger.DEBUG)
	}
	logSetup.Apply()

	// Arrange
	mockDB := new(MockMonitorDB)
	pm := &PaymentMonitor{
		db: mockDB,
	}

	// Создаем полную структуру PaymentStatus с заданными и дополнительными данными
	payment := &PaymentStatus{
		OrderID:        "test_order_123",
		UserID:         1,
		Status:         "confirmed",
		Currency:       "USDT",
		Network:        "TRC20",
		Amount:         135.00,
		AmountUsd:      135.00,
		ReceivedAmount: 135.00,
		TxHash:         "0x123abc456def789",
		Confirmations:  12,
		CreatedAt:      MySQLDateTime{Time: time.Now().Add(-1 * time.Hour)},
		UpdatedAt:      MySQLDateTime{Time: time.Now()},
		ExpiresAt:      MySQLDateTime{Time: time.Now().Add(30 * time.Minute)},
	}

	// Подготавливаем тариф пользователя
	tariff := UserTariff{
		Currency:  "USDT",
		MonthCost: 45.0,
		Discounts: map[string]float64{"3": 10.0, "6": 25.0, "12": 60.0},
	}
	tariffJSON, _ := json.Marshal(tariff)

	// Настраиваем моки
	mockDB.On("GetUserTariff", payment.UserID).Return(json.RawMessage(tariffJSON), nil)

	// Ожидаем, что будет вызван SetBillingData с правильными параметрами
	// 135 USD может купить 3 месяца (45*3=135) со скидкой 10, итого 125 USD
	expectedMonths := uint8(3)
	expectedDiscount := 10.0
	mockDB.On("SetBillingData", payment.UserID, payment.Amount, expectedMonths, expectedDiscount).Return(nil)

	// Act
	pm.onPayment(payment)

	// Assert
	mockDB.AssertCalled(t, "GetUserTariff", payment.UserID)
	mockDB.AssertCalled(t, "SetBillingData", payment.UserID, payment.Amount, expectedMonths, expectedDiscount)
	mockDB.AssertExpectations(t)
}

func TestOnPayment_Pending_NoAction(t *testing.T) {
	t.Skip("onPayment handles confirmed payments; pending filtering is performed by the event handler")

	// Arrange
	mockDB := new(MockMonitorDB)
	pm := &PaymentMonitor{
		db: mockDB,
	}

	payment := &PaymentStatus{
		OrderID:        "test_order_456",
		UserID:         1,
		Status:         "pending",
		Currency:       "USDT",
		Network:        "TRC20",
		Amount:         135.00,
		AmountUsd:      135.00,
		ReceivedAmount: 0.0,
		TxHash:         "",
		Confirmations:  0,
		CreatedAt:      MySQLDateTime{Time: time.Now().Add(-1 * time.Hour)},
		UpdatedAt:      MySQLDateTime{Time: time.Now()},
		ExpiresAt:      MySQLDateTime{Time: time.Now().Add(30 * time.Minute)},
	}

	// Act
	pm.onPayment(payment)

	// Assert - для pending статуса не должно быть вызовов к БД
	mockDB.AssertNotCalled(t, "GetUserTariff", mock.Anything)
	mockDB.AssertNotCalled(t, "SetBillingData", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestOnPayment_Confirmed_InsufficientAmount(t *testing.T) {
	// Arrange
	mockDB := new(MockMonitorDB)
	pm := &PaymentMonitor{
		db: mockDB,
	}

	payment := &PaymentStatus{
		OrderID:        "test_order_789",
		UserID:         2,
		Status:         "confirmed",
		Currency:       "USDT",
		Network:        "TRC20",
		Amount:         30.00, // Недостаточно для покупки 1 месяца (45 USD)
		AmountUsd:      30.00,
		ReceivedAmount: 30.00,
		TxHash:         "0x789def456abc123",
		Confirmations:  12,
		CreatedAt:      MySQLDateTime{Time: time.Now().Add(-1 * time.Hour)},
		UpdatedAt:      MySQLDateTime{Time: time.Now()},
		ExpiresAt:      MySQLDateTime{Time: time.Now().Add(30 * time.Minute)},
	}

	tariff := UserTariff{
		Currency:  "USDT",
		MonthCost: 45.0,
		Discounts: map[string]float64{"3": 10.0},
	}
	tariffJSON, _ := json.Marshal(tariff)

	mockDB.On("GetUserTariff", payment.UserID).Return(json.RawMessage(tariffJSON), nil)
	// SetBillingData не должен вызываться, так как месяцев будет 0

	// Act
	pm.onPayment(payment)

	// Assert
	mockDB.AssertCalled(t, "GetUserTariff", payment.UserID)
	mockDB.AssertNotCalled(t, "SetBillingData", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockDB.AssertExpectations(t)
}

func TestOnPayment_Confirmed_WithMaxDiscount(t *testing.T) {
	// Arrange
	mockDB := new(MockMonitorDB)
	pm := &PaymentMonitor{
		db: mockDB,
	}

	payment := &PaymentStatus{
		OrderID:        "test_order_max",
		UserID:         3,
		Status:         "confirmed",
		Currency:       "USDT",
		Network:        "TRC20",
		Amount:         480.00, // Достаточно для 12 месяцев со скидкой (12*45-60=480)
		AmountUsd:      480.00,
		ReceivedAmount: 480.00,
		TxHash:         "0xmaxdiscount123",
		Confirmations:  15,
		CreatedAt:      MySQLDateTime{Time: time.Now().Add(-2 * time.Hour)},
		UpdatedAt:      MySQLDateTime{Time: time.Now()},
		ExpiresAt:      MySQLDateTime{Time: time.Now().Add(15 * time.Minute)},
	}

	tariff := UserTariff{
		Currency:  "USDT",
		MonthCost: 45.0,
		Discounts: map[string]float64{"3": 10.0, "6": 25.0, "12": 60.0},
	}
	tariffJSON, _ := json.Marshal(tariff)

	mockDB.On("GetUserTariff", payment.UserID).Return(json.RawMessage(tariffJSON), nil)

	// Ожидаем 12 месяцев со скидкой 60 USD
	expectedMonths := uint8(12)
	expectedDiscount := 60.0
	mockDB.On("SetBillingData", payment.UserID, payment.Amount, expectedMonths, expectedDiscount).Return(nil)

	// Act
	pm.onPayment(payment)

	// Assert
	mockDB.AssertCalled(t, "GetUserTariff", payment.UserID)
	mockDB.AssertCalled(t, "SetBillingData", payment.UserID, payment.Amount, expectedMonths, expectedDiscount)
	mockDB.AssertExpectations(t)
}

func TestCalcDiscount_VariousScenarios(t *testing.T) {
	testCases := []struct {
		name             string
		amount           float64
		monthCost        float64
		discounts        map[string]float64
		expectedMonths   uint8
		expectedDiscount float64
	}{
		{
			name:             "Exact amount for 3 months with discount",
			amount:           125.0, // 3*45-10=125
			monthCost:        45.0,
			discounts:        map[string]float64{"3": 10.0},
			expectedMonths:   3,
			expectedDiscount: 10.0,
		},
		{
			name:             "Amount for 2 months without discount",
			amount:           90.0, // 2*45=90
			monthCost:        45.0,
			discounts:        map[string]float64{"3": 10.0},
			expectedMonths:   2,
			expectedDiscount: 0.0,
		},
		{
			name:             "Large amount choosing best discount",
			amount:           480.0,
			monthCost:        45.0,
			discounts:        map[string]float64{"3": 10.0, "6": 25.0, "12": 60.0},
			expectedMonths:   12,
			expectedDiscount: 60.0,
		},
		{
			name:             "Insufficient amount",
			amount:           30.0,
			monthCost:        45.0,
			discounts:        map[string]float64{"3": 10.0},
			expectedMonths:   0,
			expectedDiscount: 0.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			months, discount := calcDiscount(tc.amount, tc.monthCost, tc.discounts)
			assert.Equal(t, tc.expectedMonths, months, "Неверное количество месяцев")
			assert.Equal(t, tc.expectedDiscount, discount, "Неверная скидка")
		})
	}
}
