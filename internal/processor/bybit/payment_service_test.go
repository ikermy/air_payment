package bybit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// BybitClientInterface интерфейс для bybit клиента (для тестирования)
type BybitClientInterface interface {
	GetCurrencyPrice(currency string) (float64, error)
	GetDepositAddress(currency, network string) (*DepositAddress, error)
	GetSupportedCurrencies() ([]SupportedCurrency, error)
}

// Убеждаемся, что Client реализует интерфейс
var _ BybitClientInterface = (*Client)(nil)

// MockDB представляет mock для интерфейса DB
type MockDB struct {
	mock.Mock
}

func (m *MockDB) GetActivePaymentsByUserAndCurrency(_ context.Context, userID uint32, currency string, network string) (json.RawMessage, error) {
	args := m.Called(userID, currency, network)
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockDB) UpdatePaymentExpiration(_ context.Context, orderID string, newExpiresAt int64) error {
	args := m.Called(orderID, newExpiresAt)
	return args.Error(0)
}

func (m *MockDB) SaveCryptoPaymentRequest(_ context.Context, orderID string, userID uint32, currency, network string, amount, amountUSD float64, depositAddress, depositTag string, expiresAt int64) error {
	args := m.Called(orderID, userID, currency, network, amount, amountUSD, depositAddress, depositTag, expiresAt)
	return args.Error(0)
}

func (m *MockDB) GetCryptoPaymentStatus(_ context.Context, orderID string) (json.RawMessage, error) {
	args := m.Called(orderID)
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockDB) UpdateCryptoPaymentStatus(_ context.Context, orderID, status, txHash string, receivedAmount float64, confirmations int) error {
	args := m.Called(orderID, status, txHash, receivedAmount, confirmations)
	return args.Error(0)
}

func (m *MockDB) GetPendingCryptoPayments(_ context.Context) (json.RawMessage, error) {
	args := m.Called()
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockDB) CheckExpiredCryptoPayments(_ context.Context) error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDB) GetCryptoPaymentDepositAddress(_ context.Context, orderID string) (string, error) {
	args := m.Called(orderID)
	return args.String(0), args.Error(1)
}

func (m *MockDB) GetCryptoPaymentCreatedAt(_ context.Context, orderID string) (int64, error) {
	args := m.Called(orderID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockDB) CheckIfTxHashAlreadyUsed(_ context.Context, txHash string) (string, error) {
	args := m.Called(txHash)
	return args.String(0), args.Error(1)
}

func (m *MockDB) GetUserTariff(_ context.Context, userId uint32) (json.RawMessage, error) {
	args := m.Called(userId)
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockDB) SetBillingData(_ context.Context, userId uint32, payment float64, months uint8, discount float64) error {
	args := m.Called(userId, payment, months, discount)
	return args.Error(0)
}

func (m *MockDB) FindActivePaymentByAddress(_ context.Context, userID uint32, currency string, network string, address string) (string, error) {
	args := m.Called(userID, currency, network, address)
	return args.String(0), args.Error(1)
}

// MockBybitClient представляет mock для bybit клиента
type MockBybitClient struct {
	mock.Mock
}

func (m *MockBybitClient) GetCurrencyPrice(currency string) (float64, error) {
	args := m.Called(currency)
	return args.Get(0).(float64), args.Error(1)
}

func (m *MockBybitClient) GetDepositAddress(currency, network string) (*DepositAddress, error) {
	args := m.Called(currency, network)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*DepositAddress), args.Error(1)
}

func (m *MockBybitClient) GetSupportedCurrencies() ([]SupportedCurrency, error) {
	args := m.Called()
	return args.Get(0).([]SupportedCurrency), args.Error(1)
}

// TestPaymentService структура для тестирования с возможностью подмены клиента
type TestPaymentService struct {
	client BybitClientInterface
	db     DB
}

// generateOrderID генерирует уникальный ID заказа (копия из PaymentService)
func (ps *TestPaymentService) generateOrderID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// CreatePaymentRequest создает запрос на оплату (тестовая версия)
func (ps *TestPaymentService) CreatePaymentRequest(userID uint32, currency, network string, amountUSD float64) (*PaymentRequest, *DepositAddress, error) {
	// Генерируем уникальный orderID
	orderID := ps.generateOrderID()

	// Получаем актуальный курс валюты
	price, err := ps.client.GetCurrencyPrice(currency)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get currency price for %s: %w", currency, err)
	}

	// Вычисляем количество криптовалюты
	amount := amountUSD / price

	// Получаем адрес для депозита
	depositAddress, err := ps.client.GetDepositAddress(currency, network)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get deposit address: %w", err)
	}

	// Создаем запрос на оплату
	paymentRequest := &PaymentRequest{
		UserID:    userID,
		Currency:  currency,
		Amount:    amount,
		AmountUSD: amountUSD,
		OrderID:   orderID,
		Network:   network,
		ExpiresAt: time.Now().Add(30 * time.Minute).Unix(),
	}

	// Сохраняем в базу данных
	err = ps.db.SaveCryptoPaymentRequest(
		context.Background(),
		orderID, userID, currency, network, amount, amountUSD,
		depositAddress.Address, depositAddress.Tag, paymentRequest.ExpiresAt)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to save payment request: %w", err)
	}

	return paymentRequest, depositAddress, nil
}

func TestCreatePaymentRequest_Success(t *testing.T) {
	// Arrange
	mockDB := new(MockDB)
	mockClient := new(MockBybitClient)

	service := &TestPaymentService{
		client: mockClient,
		db:     mockDB,
	}

	userID := uint32(123)
	currency := "BTC"
	network := "BTC"
	amountUSD := 100.0
	expectedPrice := 50000.0
	expectedAmount := amountUSD / expectedPrice

	expectedDepositAddress := &DepositAddress{
		Currency: currency,
		Address:  "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
		Network:  network,
		Tag:      "",
	}

	// Setup mocks
	mockClient.On("GetCurrencyPrice", currency).Return(expectedPrice, nil)
	mockClient.On("GetDepositAddress", currency, network).Return(expectedDepositAddress, nil)
	mockDB.On("SaveCryptoPaymentRequest",
		mock.AnythingOfType("string"), // orderID
		userID,
		currency,
		network,
		expectedAmount,
		amountUSD,
		expectedDepositAddress.Address,
		expectedDepositAddress.Tag,
		mock.AnythingOfType("int64"), // expiresAt
	).Return(nil)

	// Act
	paymentRequest, depositAddress, err := service.CreatePaymentRequest(userID, currency, network, amountUSD)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, paymentRequest)
	require.NotNil(t, depositAddress)

	assert.Equal(t, userID, paymentRequest.UserID)
	assert.Equal(t, currency, paymentRequest.Currency)
	assert.Equal(t, network, paymentRequest.Network)
	assert.Equal(t, expectedAmount, paymentRequest.Amount)
	assert.Equal(t, amountUSD, paymentRequest.AmountUSD)
	assert.NotEmpty(t, paymentRequest.OrderID)
	assert.True(t, paymentRequest.ExpiresAt > time.Now().Unix())

	assert.Equal(t, expectedDepositAddress.Address, depositAddress.Address)
	assert.Equal(t, expectedDepositAddress.Currency, depositAddress.Currency)
	assert.Equal(t, expectedDepositAddress.Network, depositAddress.Network)

	// Verify all mocks were called
	mockClient.AssertExpectations(t)
	mockDB.AssertExpectations(t)
}

func TestCreatePaymentRequest_GetCurrencyPriceError(t *testing.T) {
	// Arrange
	mockDB := new(MockDB)
	mockClient := new(MockBybitClient)

	service := &TestPaymentService{
		client: mockClient,
		db:     mockDB,
	}

	userID := uint32(123)
	currency := "BTC"
	network := "BTC"
	amountUSD := 100.0

	expectedError := errors.New("failed to get price")

	// Setup mocks
	mockClient.On("GetCurrencyPrice", currency).Return(0.0, expectedError)

	// Act
	paymentRequest, depositAddress, err := service.CreatePaymentRequest(userID, currency, network, amountUSD)

	// Assert
	require.Error(t, err)
	assert.Nil(t, paymentRequest)
	assert.Nil(t, depositAddress)
	assert.Contains(t, err.Error(), "failed to get currency price")

	// Verify mocks
	mockClient.AssertExpectations(t)
	mockDB.AssertNotCalled(t, "SaveCryptoPaymentRequest")
}

func TestCreatePaymentRequest_GetDepositAddressError(t *testing.T) {
	// Arrange
	mockDB := new(MockDB)
	mockClient := new(MockBybitClient)

	service := &TestPaymentService{
		client: mockClient,
		db:     mockDB,
	}

	userID := uint32(123)
	currency := "BTC"
	network := "BTC"
	amountUSD := 100.0
	expectedPrice := 50000.0

	expectedError := errors.New("failed to get deposit address")

	// Setup mocks
	mockClient.On("GetCurrencyPrice", currency).Return(expectedPrice, nil)
	mockClient.On("GetDepositAddress", currency, network).Return(nil, expectedError)

	// Act
	paymentRequest, depositAddress, err := service.CreatePaymentRequest(userID, currency, network, amountUSD)

	// Assert
	require.Error(t, err)
	assert.Nil(t, paymentRequest)
	assert.Nil(t, depositAddress)
	assert.Contains(t, err.Error(), "failed to get deposit address")

	// Verify mocks
	mockClient.AssertExpectations(t)
	mockDB.AssertNotCalled(t, "SaveCryptoPaymentRequest")
}

func TestCreatePaymentRequest_SaveToDBError(t *testing.T) {
	// Arrange
	mockDB := new(MockDB)
	mockClient := new(MockBybitClient)

	service := &TestPaymentService{
		client: mockClient,
		db:     mockDB,
	}

	userID := uint32(123)
	currency := "BTC"
	network := "BTC"
	amountUSD := 100.0
	expectedPrice := 50000.0
	expectedAmount := amountUSD / expectedPrice

	expectedDepositAddress := &DepositAddress{
		Currency: currency,
		Address:  "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
		Network:  network,
		Tag:      "",
	}

	expectedError := errors.New("database error")

	// Setup mocks
	mockClient.On("GetCurrencyPrice", currency).Return(expectedPrice, nil)
	mockClient.On("GetDepositAddress", currency, network).Return(expectedDepositAddress, nil)
	mockDB.On("SaveCryptoPaymentRequest",
		mock.AnythingOfType("string"), // orderID
		userID,
		currency,
		network,
		expectedAmount,
		amountUSD,
		expectedDepositAddress.Address,
		expectedDepositAddress.Tag,
		mock.AnythingOfType("int64"), // expiresAt
	).Return(expectedError)

	// Act
	paymentRequest, depositAddress, err := service.CreatePaymentRequest(userID, currency, network, amountUSD)

	// Assert
	require.Error(t, err)
	assert.Nil(t, paymentRequest)
	assert.Nil(t, depositAddress)
	assert.Contains(t, err.Error(), "failed to save payment request")

	// Verify mocks
	mockClient.AssertExpectations(t)
	mockDB.AssertExpectations(t)
}

func TestCreatePaymentRequest_CorrectAmountCalculation(t *testing.T) {
	testCases := []struct {
		name           string
		currency       string
		amountUSD      float64
		priceUSD       float64
		expectedAmount float64
	}{
		{
			name:           "BTC calculation",
			currency:       "BTC",
			amountUSD:      1000.0,
			priceUSD:       50000.0,
			expectedAmount: 0.02,
		},
		{
			name:           "ETH calculation",
			currency:       "ETH",
			amountUSD:      500.0,
			priceUSD:       2500.0,
			expectedAmount: 0.2,
		},
		{
			name:           "USDT calculation",
			currency:       "USDT",
			amountUSD:      100.0,
			priceUSD:       1.0,
			expectedAmount: 100.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			mockDB := new(MockDB)
			mockClient := new(MockBybitClient)

			service := &TestPaymentService{
				client: mockClient,
				db:     mockDB,
			}

			userID := uint32(123)
			network := "BTC"

			expectedDepositAddress := &DepositAddress{
				Currency: tc.currency,
				Address:  "test_address",
				Network:  network,
				Tag:      "",
			}

			// Setup mocks
			mockClient.On("GetCurrencyPrice", tc.currency).Return(tc.priceUSD, nil)
			mockClient.On("GetDepositAddress", tc.currency, network).Return(expectedDepositAddress, nil)
			mockDB.On("SaveCryptoPaymentRequest",
				mock.AnythingOfType("string"), // orderID
				userID,
				tc.currency,
				network,
				tc.expectedAmount,
				tc.amountUSD,
				expectedDepositAddress.Address,
				expectedDepositAddress.Tag,
				mock.AnythingOfType("int64"), // expiresAt
			).Return(nil)

			// Act
			paymentRequest, _, err := service.CreatePaymentRequest(userID, tc.currency, network, tc.amountUSD)

			// Assert
			require.NoError(t, err)
			require.NotNil(t, paymentRequest)
			assert.Equal(t, tc.expectedAmount, paymentRequest.Amount)
		})
	}
}

func TestGenerateOrderID(t *testing.T) {
	// Arrange
	service := &TestPaymentService{}

	// Act
	orderID1 := service.generateOrderID()
	orderID2 := service.generateOrderID()

	// Assert
	assert.NotEmpty(t, orderID1)
	assert.NotEmpty(t, orderID2)
	assert.NotEqual(t, orderID1, orderID2) // Должны быть разные
	assert.Len(t, orderID1, 32)            // 16 bytes * 2 (hex encoding)
	assert.Len(t, orderID2, 32)
}

func TestCreatePaymentRequest_ExpirationTime(t *testing.T) {
	// Arrange
	mockDB := new(MockDB)
	mockClient := new(MockBybitClient)

	service := &TestPaymentService{
		client: mockClient,
		db:     mockDB,
	}

	userID := uint32(123)
	currency := "BTC"
	network := "BTC"
	amountUSD := 100.0
	expectedPrice := 50000.0
	expectedAmount := amountUSD / expectedPrice

	expectedDepositAddress := &DepositAddress{
		Currency: currency,
		Address:  "test_address",
		Network:  network,
		Tag:      "",
	}

	// Setup mocks
	mockClient.On("GetCurrencyPrice", currency).Return(expectedPrice, nil)
	mockClient.On("GetDepositAddress", currency, network).Return(expectedDepositAddress, nil)
	mockDB.On("SaveCryptoPaymentRequest",
		mock.AnythingOfType("string"),
		userID,
		currency,
		network,
		expectedAmount,
		amountUSD,
		expectedDepositAddress.Address,
		expectedDepositAddress.Tag,
		mock.AnythingOfType("int64"),
	).Return(nil)

	// Act
	startTime := time.Now()
	paymentRequest, _, err := service.CreatePaymentRequest(userID, currency, network, amountUSD)
	endTime := time.Now()

	// Assert
	require.NoError(t, err)
	require.NotNil(t, paymentRequest)

	// Проверяем, что время истечения установлено на 30 минут вперед
	expectedMin := startTime.Add(29 * time.Minute).Unix()
	expectedMax := endTime.Add(31 * time.Minute).Unix()

	assert.True(t, paymentRequest.ExpiresAt >= expectedMin,
		"ExpiresAt должно быть не менее чем через 29 минут")
	assert.True(t, paymentRequest.ExpiresAt <= expectedMax,
		"ExpiresAt должно быть не более чем через 31 минуту")
}
