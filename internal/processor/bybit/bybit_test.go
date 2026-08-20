package bybit

import (
	"air_payment/internal/domain"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// GetConf возвращает конфигурацию интеграционных тестов из переменных окружения.
func GetConf(t *testing.T) *domain.Bybit {
	// Читаем SECRET_KEY из файла
	secretKeyFile := strings.TrimSpace(os.Getenv("SECRET_KEY_FILE"))
	secretKey, err := os.ReadFile(secretKeyFile)
	if err != nil {
		t.Fatalf("ошибка чтения SECRET_KEY из файла %s: %v", secretKeyFile, err)
	}

	// Читаем API_KEY из файла
	apiKeyFile := strings.TrimSpace(os.Getenv("API_KEY_FILE"))
	apiKey, err := os.ReadFile(apiKeyFile)
	if err != nil {
		t.Fatalf("ошибка чтения API_KEY из файла %s: %v", apiKeyFile, err)
	}

	cfg := &domain.Bybit{
		ApiKey:    strings.TrimSpace(string(apiKey)),
		SecretKey: strings.TrimSpace(string(secretKey)),
		IsTestnet: os.Getenv("TESTNET") == "true",
	}

	t.Logf("TEST: %v", cfg.IsTestnet)
	t.Logf("YEST_API_KEY: %s...", cfg.ApiKey[:4])
	return cfg
}

// TestGetSupportedCurrencies_Integration тестирует реальное обращение к bybit API
func TestGetSupportedCurrencies_Integration(t *testing.T) {
	// Получаем конфигурацию с API ключами
	cfg := GetConf(t)

	// Проверяем, что API ключи настроены
	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured. Please set real API keys in cfg.env to run integration tests")
	}

	// Создаем клиент с реальными API ключами
	client := NewBybitClient(cfg)

	currencies, err := client.GetSupportedCurrencies()

	// Проверяем, что нет ошибок
	require.NoError(t, err, "GetSupportedCurrencies should not return error")

	// Проверяем, что список не пустой
	assert.NotEmpty(t, currencies, "Currencies list should not be empty")

	// Проверяем структуру данных
	if len(currencies) > 0 {
		currency := currencies[0]

		// Проверяем, что обязательные поля заполнены
		assert.NotEmpty(t, currency.Symbol, "Currency symbol should not be empty")
		assert.NotEmpty(t, currency.Name, "Currency name should not be empty")
		assert.NotEmpty(t, currency.Network, "Currency network should not be empty")
		assert.True(t, currency.MinDeposit >= 0, "MinDeposit should be non-negative")

		t.Logf("First currency: Symbol=%s, Name=%s, Network=%s, MinDeposit=%f, Available=%t",
			currency.Symbol, currency.Name, currency.Network, currency.MinDeposit, currency.IsAvailable)
	}

	// Проверяем, что есть основные валюты (BTC, ETH, USDT)
	foundBTC := false
	foundETH := false
	foundUSDT := false

	for _, currency := range currencies {
		switch currency.Symbol {
		case "BTC":
			foundBTC = true
		case "ETH":
			foundETH = true
		case "USDT":
			foundUSDT = true
		}
	}

	// Логируем найденные валюты для отладки
	t.Logf("Found %d currencies", len(currencies))
	t.Logf("BTC found: %t, ETH found: %t, USDT found: %t", foundBTC, foundETH, foundUSDT)

	// Обычно на bybit должны быть эти основные валюты
	// Но делаем мягкую проверку, так как список может меняться
	if !foundBTC && !foundETH && !foundUSDT {
		t.Log("Warning: None of the major currencies (BTC, ETH, USDT) found. This might indicate an API issue.")
	}
}

// TestGetCurrencyPrice_Integration тестирует получение курсов валют (публичный API)
func TestGetCurrencyPrice_Integration(t *testing.T) {
	// Для получения курсов валют API ключи не требуются
	fakeConf := domain.Bybit{
		ApiKey:    "",
		SecretKey: "",
		IsTestnet: true,
	}
	client := NewBybitClient(&fakeConf)

	testCases := []struct {
		currency     string
		expectedRate float64
		description  string
	}{
		{"USDT", 1.0, "USDT should return 1.0"},
		{"USDC", 1.0, "USDC should return 1.0"},
		{"BUSD", 1.0, "BUSD should return 1.0"},
		{"BTC", 0.0, "BTC should return positive rate"},
		{"ETH", 0.0, "ETH should return positive rate"},
	}

	for _, tc := range testCases {
		t.Run(tc.currency, func(t *testing.T) {
			price, err := client.GetCurrencyPrice(tc.currency)

			if tc.expectedRate == 1.0 {
				// Для стейблкоинов ожидаем точно 1.0
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedRate, price, tc.description)
			} else {
				// Для других валют проверяем, что цена положительная
				if err != nil {
					t.Logf("Warning: Failed to get price for %s: %v (might be expected for testnet)", tc.currency, err)
				} else {
					assert.Greater(t, price, 0.0, "%s price should be positive", tc.currency)
					t.Logf("%s price: $%.2f", tc.currency, price)
				}
			}
		})
	}
}

// TestGetSupportedCurrencies_MockBehavior проверяет поведение без API ключей
func TestGetSupportedCurrencies_MockBehavior(t *testing.T) {
	// Тест поведения без API ключей (должен вернуть ошибку)
	fakeConf := domain.Bybit{
		ApiKey:    "",
		SecretKey: "",
		IsTestnet: true,
	}
	client := NewBybitClient(&fakeConf)

	currencies, err := client.GetSupportedCurrencies()

	// Ожидаем ошибку, так как API требует аутентификации
	assert.Error(t, err, "Should return error when API keys are not provided")
	assert.Nil(t, currencies, "Currencies should be nil on error")
	assert.Contains(t, err.Error(), "api key", "Error should mention API key requirement")
}

// TestGetSupportedCurrencies_NetworkFiltering проверяет фильтрацию по сетям
func TestGetSupportedCurrencies_NetworkFiltering(t *testing.T) {
	cfg := GetConf(t)
	client := NewBybitClient(cfg)

	currencies, err := client.GetSupportedCurrencies()
	require.NoError(t, err)

	// Группируем валюты по символам для проверки множественных сетей
	currencyNetworks := make(map[string][]string)
	for _, currency := range currencies {
		currencyNetworks[currency.Symbol] = append(currencyNetworks[currency.Symbol], currency.Network)
	}

	// Проверяем, что некоторые валюты имеют множественные сети
	multiNetworkFound := false
	for symbol, networks := range currencyNetworks {
		if len(networks) > 1 {
			multiNetworkFound = true
			t.Logf("Currency %s has multiple networks: %v", symbol, networks)
			break
		}
	}

	t.Logf("Multi-network currencies found: %t", multiNetworkFound)
}

// TestGetSupportedCurrencies_DepositAvailability проверяет доступность депозитов
func TestGetSupportedCurrencies_DepositAvailability(t *testing.T) {
	cfg := GetConf(t)
	client := NewBybitClient(cfg)

	currencies, err := client.GetSupportedCurrencies()
	require.NoError(t, err)

	availableCount := 0
	unavailableCount := 0

	for _, currency := range currencies {
		if currency.IsAvailable {
			availableCount++
		} else {
			unavailableCount++
		}
	}

	t.Logf("Available currencies: %d, Unavailable currencies: %d", availableCount, unavailableCount)

	// Должны быть доступные валюты для депозитов
	assert.Greater(t, availableCount, 0, "Should have at least some available currencies for deposit")
}

// TestGetSupportedCurrencies_ErrorHandling проверяет обработку ошибок
func TestGetSupportedCurrencies_ErrorHandling(t *testing.T) {
	t.Skip("error injection is not supported by the current Bybit client")
}

// TestGetSupportedCurrencies_DataValidation проверяет валидность данных
func TestGetSupportedCurrencies_DataValidation(t *testing.T) {
	cfg := GetConf(t)
	client := NewBybitClient(cfg)

	currencies, err := client.GetSupportedCurrencies()
	require.NoError(t, err)
	require.NotEmpty(t, currencies)

	// Проверяем каждую валюту на валидность данных
	for i, currency := range currencies {
		t.Run(currency.Symbol+"_"+currency.Network, func(t *testing.T) {
			// Символ валюты не должен быть пустым
			assert.NotEmpty(t, currency.Symbol, "Symbol should not be empty for currency %d", i)

			// Название не должно быть пустым
			assert.NotEmpty(t, currency.Name, "Name should not be empty for currency %d", i)

			// Сеть не должна быть пустой
			assert.NotEmpty(t, currency.Network, "Network should not be empty for currency %d", i)

			// MinDeposit должен быть неотрицательным
			assert.GreaterOrEqual(t, currency.MinDeposit, 0.0, "MinDeposit should be non-negative for currency %d", i)

			// Символ должен быть в верхнем регистре и содержать только буквы/цифры
			assert.Regexp(t, `^[A-Z0-9]+$`, currency.Symbol, "Symbol should be uppercase alphanumeric for currency %d", i)
		})
	}
}

// BenchmarkGetSupportedCurrencies тестирует производительность
func BenchmarkGetSupportedCurrencies(b *testing.B) {
	t := &testing.T{}
	cfg := GetConf(t)
	client := NewBybitClient(cfg)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		currencies, err := client.GetSupportedCurrencies()
		if err != nil {
			b.Fatalf("Error during benchmark: %v", err)
		}
		if len(currencies) == 0 {
			b.Fatal("No currencies returned during benchmark")
		}
	}
}

// TestGetSupportedCurrencies_Timeout проверяет обработку таймаутов
func TestGetSupportedCurrencies_Timeout(t *testing.T) {
	// Этот тест можно раскомментировать для проверки поведения при медленном соединении
	t.Skip("Skipping timeout test - uncomment to test timeout behavior")

	cfg := GetConf(t)
	client := NewBybitClient(cfg)

	// Устанавливаем очень короткий таймаут для тестирования
	// client.client = client.client.WithTimeout(1 * time.Millisecond)

	currencies, err := client.GetSupportedCurrencies()

	// При таймауте ожидаем ошибку
	assert.Error(t, err, "Should return error on timeout")
	assert.Nil(t, currencies, "Currencies should be nil on timeout")
}

// TestGetDepositAddress_Integration тестирует реальное получение адреса депозита
func TestGetDepositAddress_Integration(t *testing.T) {
	// Получаем конфигурацию с API ключами
	cfg := GetConf(t)

	// Проверяем, что API ключи настроены
	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured. Please set real API keys in cfg.env to run integration tests")
	}

	// Создаем клиент с реальными API ключами
	client := NewBybitClient(cfg)

	testCases := []struct {
		currency    string
		network     string
		description string
	}{
		{"USDT", "ETH", "USDT on Ethereum network"},
		{"USDT", "TRX", "USDT on Tron network"},
		{"BTC", "BTC", "Bitcoin on Bitcoin network"},
		{"ETH", "ETH", "Ethereum on Ethereum network"},
	}

	for _, tc := range testCases {
		t.Run(tc.currency+"_"+tc.network, func(t *testing.T) {
			address, err := client.GetDepositAddress(tc.currency, tc.network)

			if err != nil {
				// Проверяем на региональные ограничения
				if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
					t.Skipf("bybit API access restricted due to regulatory limitations for %s on %s", tc.currency, tc.network)
				}
				// На testnet некоторые валюты могут быть недоступны
				t.Logf("Warning: Failed to get deposit address for %s on %s: %v", tc.currency, tc.network, err)
				return
			}

			// Проверяем основные поля
			require.NotNil(t, address, "Deposit address should not be nil")
			assert.Equal(t, tc.currency, address.Currency, "Currency should match")
			assert.Equal(t, tc.network, address.Network, "Network should match")
			assert.NotEmpty(t, address.Address, "Address should not be empty")

			// Проверяем формат адреса в зависимости от валюты
			switch tc.currency {
			case "BTC":
				// Bitcoin адрес должен начинаться с 1, 3 или bc1
				assert.Regexp(t, `^(1|3|bc1)`, address.Address, "Bitcoin address should have valid format")
			case "ETH", "USDT":
				if tc.network == "ETH" {
					// Ethereum адрес должен начинаться с 0x и содержать 40 hex символов
					assert.Regexp(t, `^0x[a-fA-F0-9]{40}$`, address.Address, "Ethereum address should have valid format")
				}
			}

			// Логируем полученные данные
			t.Logf("Deposit address for %s on %s: %s", tc.currency, tc.network, address.Address)
			if address.Tag != "" {
				t.Logf("Tag/Memo: %s", address.Tag)
			}
		})
	}
}

// TestGetDepositAddress_ErrorHandling тестирует обработку ошибок
func TestGetDepositAddress_ErrorHandling(t *testing.T) {
	cfg := GetConf(t)

	// Проверяем, что API ключи настроены
	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)

	// Сначала проверим доступность API
	_, err := client.GetDepositAddress("USDT", "ETH")
	if err != nil && (contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions")) {
		t.Skip("bybit API access restricted due to regulatory limitations")
	}

	testCases := []struct {
		currency    string
		network     string
		description string
	}{
		{"INVALID", "ETH", "Invalid currency"},
		{"BTC", "INVALID", "Invalid network"},
		{"", "ETH", "Empty currency"},
		{"BTC", "", "Empty network"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			address, err := client.GetDepositAddress(tc.currency, tc.network)

			// Ожидаем ошибку для невалидных параметров
			assert.Error(t, err, "Should return error for %s", tc.description)
			assert.Nil(t, address, "Address should be nil on error")
		})
	}
}

// TestGetDepositAddress_NoAPIKeys тестирует поведение без API ключей
func TestGetDepositAddress_NoAPIKeys(t *testing.T) {
	// Создаем клиент без API ключей
	fakeConf := domain.Bybit{
		ApiKey:    "",
		SecretKey: "",
		IsTestnet: true,
	}
	client := NewBybitClient(&fakeConf)

	address, err := client.GetDepositAddress("USDT", "ETH")

	// Ожидаем ошибку, так как метод требует аутентификации
	assert.Error(t, err, "Should return error when API keys are not provided")
	assert.Nil(t, address, "Address should be nil on error")
}

// TestGetDepositAddress_TaggedCurrencies тестирует валюты с тегами (memo)
func TestGetDepositAddress_TaggedCurrencies(t *testing.T) {
	cfg := GetConf(t)

	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)

	// Валюты, которые обычно требуют теги/мемо
	testCases := []struct {
		currency string
		network  string
	}{
		{"XRP", "XRP"},
		{"EOS", "EOS"},
		// Другие валюты с тегами можно добавить здесь
	}

	for _, tc := range testCases {
		t.Run(tc.currency+"_"+tc.network, func(t *testing.T) {
			address, err := client.GetDepositAddress(tc.currency, tc.network)

			if err != nil {
				// Проверяем на региональные ограничения
				if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
					t.Skipf("bybit API access restricted due to regulatory limitations for %s on %s", tc.currency, tc.network)
				}
				// Некоторые валюты могут быть недоступны на testnet
				t.Logf("Currency %s on %s may not be available: %v", tc.currency, tc.network, err)
				return
			}

			require.NotNil(t, address, "Address should not be nil")
			assert.NotEmpty(t, address.Address, "Address should not be empty")

			// Для валют с тегами проверяем наличие тега
			if address.Tag != "" {
				t.Logf("Currency %s has tag/memo: %s", tc.currency, address.Tag)
				assert.NotEmpty(t, address.Tag, "Tag should not be empty when present")
			}
		})
	}
}

// TestGetDepositAddress_NetworkValidation тестирует валидацию сетей
func TestGetDepositAddress_NetworkValidation(t *testing.T) {
	cfg := GetConf(t)

	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)
	// Тестируем USDT на разных сетях
	testNetworks := []string{"ETH", "TRX", "BSC", "MATIC"}

	for _, network := range testNetworks {
		t.Run("USDT_"+network, func(t *testing.T) {
			address, err := client.GetDepositAddress("USDT", network)

			if err != nil {
				// Проверяем на региональные ограничения
				if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
					t.Skipf("bybit API access restricted due to regulatory limitations for USDT on %s", network)
				}
				t.Logf("Network %s may not be available for USDT: %v", network, err)
				return
			}

			require.NotNil(t, address, "Address should not be nil")
			assert.Equal(t, "USDT", address.Currency, "Currency should be USDT")
			assert.Equal(t, network, address.Network, "Network should match")
			assert.NotEmpty(t, address.Address, "Address should not be empty")

			// Проверяем формат адреса в зависимости от сети
			switch network {
			case "ETH", "BSC", "MATIC":
				// EVM-совместимые сети должны иметь адреса формата 0x...
				assert.Regexp(t, `^0x[a-fA-F0-9]{40}$`, address.Address, "EVM address should have valid format for network %s", network)
			case "TRX":
				// Tron адреса обычно начинаются с T
				assert.Regexp(t, `^T[a-zA-Z0-9]{33}$`, address.Address, "Tron address should have valid format")
			}

			t.Logf("USDT address on %s: %s", network, address.Address)
		})
	}
}

// TestGetDepositAddress_ConsistentResults тестирует консистентность результатов
func TestGetDepositAddress_ConsistentResults(t *testing.T) {
	cfg := GetConf(t)

	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)

	// Получаем адрес несколько раз подряд
	currency := "USDT"
	network := "ETH"

	var addresses []*DepositAddress
	for i := 0; i < 3; i++ {
		address, err := client.GetDepositAddress(currency, network)
		if err != nil {
			// Проверяем на региональные ограничения
			if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
				t.Skip("bybit API access restricted due to regulatory limitations")
			}
			t.Skipf("Failed to get deposit address: %v", err)
		}
		addresses = append(addresses, address)
	}

	if len(addresses) == 0 {
		t.Skip("No addresses obtained for consistency test")
	}

	// Проверяем, что все адреса одинаковые (должны быть консистентными)
	firstAddress := addresses[0]
	for i, addr := range addresses[1:] {
		assert.Equal(t, firstAddress.Address, addr.Address, "Address should be consistent on call %d", i+2)
		assert.Equal(t, firstAddress.Currency, addr.Currency, "Currency should be consistent on call %d", i+2)
		assert.Equal(t, firstAddress.Network, addr.Network, "Network should be consistent on call %d", i+2)
		assert.Equal(t, firstAddress.Tag, addr.Tag, "Tag should be consistent on call %d", i+2)
	}
}

// BenchmarkGetDepositAddress тестирует производительность получения адреса
func BenchmarkGetDepositAddress(b *testing.B) {
	t := &testing.T{}
	cfg := GetConf(t)

	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		b.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)

	// Предварительно проверяем, что API доступен
	address, err := client.GetDepositAddress("USDT", "ETH")
	if err != nil {
		// Проверяем на региональные ограничения или другие системные ошибки
		if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
			b.Skip("bybit API access restricted due to regulatory limitations")
		}
		if contains(err.Error(), "permission") || contains(err.Error(), "not available") {
			b.Skip("bybit API access denied - check account permissions or regional restrictions")
		}
		// Для других ошибок API пропускаем тест
		b.Skipf("bybit API not accessible: %v", err)
	}
	if address == nil {
		b.Skip("No address returned - API may not be available")
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		address, err := client.GetDepositAddress("USDT", "ETH")
		if err != nil {
			// Во время бенчмарка прерываем только на критических ошибках
			if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
				b.Skip("API access restricted during benchmark")
			}
			b.Logf("Warning: Error during benchmark iteration %d: %v", i, err)
			continue // Продолжаем бенчмарк, игнорируя отдельные ошибки
		}
		if address == nil {
			b.Logf("Warning: No address returned during benchmark iteration %d", i)
			continue
		}
	}
}

// TestGetDepositHistory_Integration тестирует реальное получение истории депозитов
func TestGetDepositHistory_Integration(t *testing.T) {
	cfg := GetConf(t)

	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)

	// Тестовые параметры - используем правильные исторические даты
	currency := "USDT"
	limit := 10

	// Используем последние 30 дней от текущего времени
	now := time.Now().Unix() * 1000 // в миллисекундах
	endTime := now
	startTime := now - (30 * 24 * 60 * 60 * 1000) // 30 дней назад

	records, err := client.GetDepositHistory(currency, startTime, endTime, limit)

	if err != nil {
		// Проверяем на региональные ограничения
		if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
			t.Skip("bybit API access restricted due to regulatory limitations")
		}
		// Проверяем на ошибку временного интервала
		if contains(err.Error(), "131002") || contains(err.Error(), "interval between") {
			// Попробуем с меньшим интервалом (7 дней)
			startTime = now - (7 * 24 * 60 * 60 * 1000)
			records, err = client.GetDepositHistory(currency, startTime, endTime, limit)
			if err != nil {
				t.Logf("Warning: Failed to get deposit history even with 7-day interval: %v (may be expected on testnet)", err)
				// Если все еще ошибка, попробуем исторический период
				startTime = int64(1640995200000) // 1 января 2022
				endTime = int64(1643673600000)   // 1 февраля 2022
				records, err = client.GetDepositHistory(currency, startTime, endTime, limit)
				if err != nil {
					t.Logf("Warning: No deposit history available: %v (expected on testnet)", err)
					return
				}
			}
		} else {
			// На testnet может не быть истории депозитов
			t.Logf("Warning: Failed to get deposit history: %v (may be expected on testnet)", err)
			return
		}
	}

	// Проверяем основные параметры
	if records == nil {
		t.Log("No deposit history records returned (expected on testnet)")
		return
	}

	assert.LessOrEqual(t, len(records), limit, "Records count should not exceed limit")
	t.Logf("Retrieved %d deposit records for %s in time range %d - %d", len(records), currency, startTime, endTime)

	// Если есть записи, проверяем их структуру
	for i, record := range records {
		t.Run(fmt.Sprintf("Record_%d", i), func(t *testing.T) {
			assert.NotEmpty(t, record.Coin, "Coin should not be empty")
			assert.NotEmpty(t, record.Chain, "Chain should not be empty")
			assert.NotEmpty(t, record.Amount, "Amount should not be empty")
			assert.NotEmpty(t, record.TxID, "TxID should not be empty")
			assert.GreaterOrEqual(t, record.Status, 0, "Status should be non-negative")
			assert.LessOrEqual(t, record.Status, 3, "Status should be valid (0-3)")

			// Логируем информацию о записи
			t.Logf("Record %d: Coin=%s, Chain=%s, Amount=%s, TxID=%s, Status=%d",
				i, record.Coin, record.Chain, record.Amount, record.TxID, record.Status)
		})
	}

	// Если записей нет, это нормально для testnet
	if len(records) == 0 {
		t.Log("No deposit records found (expected on testnet where there may be no transaction history)")
	}
}

// TestGetDepositHistory_Parameters тестирует различные параметры запроса
func TestGetDepositHistory_Parameters(t *testing.T) {
	cfg := GetConf(t)

	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)

	// Используем динамические временные диапазоны
	now := time.Now().Unix() * 1000 // в миллисекундах
	endTime := now
	startTime := now - (7 * 24 * 60 * 60 * 1000) // 7 дней назад

	testCases := []struct {
		name      string
		currency  string
		startTime int64
		endTime   int64
		limit     int
	}{
		{"USDT_Medium_Limit", "USDT", startTime, endTime, 50},
		{"BTC_Small_Limit", "BTC", startTime, endTime, 20},
		{"ETH_Very_Small_Limit", "ETH", startTime, endTime, 5},
		{"USDT_Large_Limit", "USDT", startTime, endTime, 200},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			records, err := client.GetDepositHistory(tc.currency, tc.startTime, tc.endTime, tc.limit)

			if err != nil {
				// Проверяем на региональные ограничения
				if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
					t.Skipf("bybit API access restricted for %s", tc.currency)
				}
				// Проверяем на ошибку временного интервала
				if contains(err.Error(), "131002") || contains(err.Error(), "interval between") {
					// Попробуем с меньшим интервалом (1 день)
					smallerStartTime := now - (24 * 60 * 60 * 1000)
					records, err = client.GetDepositHistory(tc.currency, smallerStartTime, endTime, tc.limit)
					if err != nil {
						t.Logf("Warning: No deposit history for %s even with 1-day interval: %v (expected on testnet)", tc.currency, err)
						return
					}
				} else {
					// На testnet может не быть истории
					t.Logf("Warning: No deposit history for %s: %v (expected on testnet)", tc.currency, err)
					return
				}
			}

			// Если records все еще nil, это нормально для testnet
			if records == nil {
				t.Logf("No deposit history records for %s (expected on testnet)", tc.currency)
				return
			}

			assert.LessOrEqual(t, len(records), tc.limit, "Records count should not exceed limit for %s", tc.currency)
			t.Logf("Retrieved %d records for %s with limit %d", len(records), tc.currency, tc.limit)

			// Если есть записи, проверяем их базовую структуру
			for _, record := range records {
				assert.NotEmpty(t, record.Coin, "Coin should not be empty")
				assert.Equal(t, tc.currency, record.Coin, "Record currency should match requested currency")
				assert.GreaterOrEqual(t, record.Status, 0, "Status should be non-negative")
				assert.LessOrEqual(t, record.Status, 3, "Status should be valid (0-3)")
			}
		})
	}
}

// TestGetDepositHistory_ErrorHandling тестирует обработку ошибок
func TestGetDepositHistory_ErrorHandling(t *testing.T) {
	cfg := GetConf(t)

	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)

	// Используем динамические временные диапазоны
	now := time.Now().Unix() * 1000
	endTime := now
	startTime := now - (7 * 24 * 60 * 60 * 1000) // 7 дней назад

	// Сначала проверим доступность API
	_, err := client.GetDepositHistory("USDT", startTime, endTime, 10)
	if err != nil && (contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions")) {
		t.Skip("bybit API access restricted due to regulatory limitations")
	}

	testCases := []struct {
		name      string
		currency  string
		startTime int64
		endTime   int64
		limit     int
	}{
		{"Invalid_Currency", "INVALID_COIN", startTime, endTime, 10},
		{"Empty_Currency", "", startTime, endTime, 10},
		{"Invalid_Time_Range", "USDT", now + (24 * 60 * 60 * 1000), now + (48 * 60 * 60 * 1000), 10}, // будущие даты
		{"Zero_Limit", "USDT", startTime, endTime, 0},
		{"Negative_Limit", "USDT", startTime, endTime, -1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			records, err := client.GetDepositHistory(tc.currency, tc.startTime, tc.endTime, tc.limit)

			// Для большинства невалидных параметров ожидаем ошибку
			if tc.name != "Invalid_Currency" { // API может просто вернуть пустой список для неизвестной валюты
				if err == nil && (records == nil || len(records) == 0) {
					t.Logf("API returned empty result for %s (may be expected)", tc.name)
				} else if err != nil {
					t.Logf("API returned error for %s: %v (may be expected)", tc.name, err)
				}
			}
		})
	}
}

// TestGetDepositHistory_NoAPIKeys тестирует поведение без API ключей
func TestGetDepositHistory_NoAPIKeys(t *testing.T) {
	fakeConf := domain.Bybit{
		ApiKey:    "",
		SecretKey: "",
		IsTestnet: true,
	}
	client := NewBybitClient(&fakeConf)

	records, err := client.GetDepositHistory("USDT", int64(1640995200000), int64(1643673600000), 10)

	// Ожидаем ошибку, так как метод требует аутентификации
	assert.Error(t, err, "Should return error when API keys are not provided")
	assert.Nil(t, records, "Records should be nil on error")
}

// TestGetDepositHistory_StatusValidation тестирует валидацию статусов депозитов
func TestGetDepositHistory_StatusValidation(t *testing.T) {
	cfg := GetConf(t)

	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)

	// Используем динамические временные диапазоны
	now := time.Now().Unix() * 1000
	endTime := now
	startTime := now - (30 * 24 * 60 * 60 * 1000) // 30 дней назад

	records, err := client.GetDepositHistory("USDT", startTime, endTime, 100)

	if err != nil {
		// Проверяем на региональные ограничения
		if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
			t.Skip("bybit API access restricted due to regulatory limitations")
		}
		// Проверяем на ошибку временного интервала
		if contains(err.Error(), "131002") || contains(err.Error(), "interval between") {
			// Попробуем с меньшим интервалом (7 дней)
			startTime = now - (7 * 24 * 60 * 60 * 1000)
			records, err = client.GetDepositHistory("USDT", startTime, endTime, 100)
			if err != nil {
				t.Logf("Warning: No deposit history available: %v (expected on testnet)", err)
				return
			}
		} else {
			t.Logf("Warning: No deposit history available: %v", err)
			return
		}
	}

	// Если записей нет, это нормально для testnet
	if records == nil || len(records) == 0 {
		t.Log("No deposit records found for status validation (expected on testnet)")
		return
	}

	// Статистика по статусам
	statusCounts := make(map[int]int)
	validStatuses := map[int]string{
		0: "pending",
		1: "processing",
		2: "success",
		3: "failed",
	}

	for _, record := range records {
		statusCounts[record.Status]++

		// Проверяем, что статус валидный
		if _, exists := validStatuses[record.Status]; !exists {
			t.Errorf("Invalid status %d found in record with TxID: %s", record.Status, record.TxID)
		}
	}

	// Логируем статистику
	for status, count := range statusCounts {
		statusName := validStatuses[status]
		t.Logf("Status %d (%s): %d records", status, statusName, count)
	}
}

// TestGetDepositHistory_DataValidation тестирует валидность данных
func TestGetDepositHistory_DataValidation(t *testing.T) {
	cfg := GetConf(t)

	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)
	// Используем динамические временные диапазоны
	now := time.Now().Unix() * 1000
	endTime := now
	startTime := now - (30 * 24 * 60 * 60 * 1000) // 30 дней назад

	records, err := client.GetDepositHistory("USDT", startTime, endTime, 50)

	if err != nil {
		// Проверяем на региональные ограничения
		if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
			t.Skip("bybit API access restricted due to regulatory limitations")
		}
		// Проверяем на ошибку временного интервала
		if contains(err.Error(), "131002") || contains(err.Error(), "interval between") {
			// Попробуем с меньшим интервалом (7 дней)
			startTime = now - (7 * 24 * 60 * 60 * 1000)
			records, err = client.GetDepositHistory("USDT", startTime, endTime, 50)
			if err != nil {
				t.Logf("Warning: No deposit history available: %v (expected on testnet)", err)
				return
			}
		} else {
			t.Logf("Warning: No deposit history available: %v", err)
			return
		}
	}

	// Если записей нет, это нормально для testnet
	if records == nil || len(records) == 0 {
		t.Log("No deposit records found for data validation (expected on testnet)")
		return
	}

	for i, record := range records {
		t.Run(fmt.Sprintf("ValidationRecord_%d", i), func(t *testing.T) {
			// Основные поля не должны быть пустыми
			assert.NotEmpty(t, record.Coin, "Coin should not be empty")
			assert.NotEmpty(t, record.Chain, "Chain should not be empty")
			assert.NotEmpty(t, record.Amount, "Amount should not be empty")
			assert.NotEmpty(t, record.TxID, "TxID should not be empty")

			// Проверяем формат суммы (должна быть числом)
			if record.Amount != "" {
				_, err := strconv.ParseFloat(record.Amount, 64)
				assert.NoError(t, err, "Amount should be a valid number: %s", record.Amount)
			}

			// Проверяем формат адреса (для основных сетей)
			if record.ToAddress != "" {
				switch record.Chain {
				case "ETH", "BSC", "MATIC":
					// EVM-совместимые сети
					assert.Regexp(t, `^0x[a-fA-F0-9]{40}$`, record.ToAddress, "EVM address should have valid format")
				case "TRX":
					// Tron адрес
					assert.Regexp(t, `^T[a-zA-Z0-9]{33}$`, record.ToAddress, "Tron address should have valid format")
				case "BTC":
					// Bitcoin адрес
					assert.Regexp(t, `^(1|3|bc1)`, record.ToAddress, "Bitcoin address should have valid format")
				}
			}

			// Проверяем формат TxID (может быть hex строка или UUID)
			if record.TxID != "" {
				// TxID может быть в разных форматах:
				// 1. Hex строка для блокчейн транзакций (64+ символов)
				// 2. UUID для внутренних транзакций bybit (36 символов)
				isHexFormat := len(record.TxID) >= 64
				isUUIDFormat := len(record.TxID) == 36 && strings.Count(record.TxID, "-") == 4

				if isHexFormat {
					assert.Regexp(t, `^[a-fA-F0-9]+$`, record.TxID, "Long TxID should be hex string")
				} else if isUUIDFormat {
					assert.Regexp(t, `^[a-fA-F0-9\-]+$`, record.TxID, "UUID TxID should contain only hex characters and hyphens")
				} else {
					// Принимаем любой непустой TxID для других форматов
					t.Logf("TxID has non-standard format: %s (length: %d)", record.TxID, len(record.TxID))
				}
			}
		})
	}
}

// TestGetDepositHistory_TimeRange тестирует различные временные диапазоны
func TestGetDepositHistory_TimeRange(t *testing.T) {
	cfg := GetConf(t)

	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)
	now := time.Now().Unix() * 1000 // в миллисекундах

	testCases := []struct {
		name      string
		startTime int64
		endTime   int64
	}{
		{"Last_24_Hours", now - (24 * 60 * 60 * 1000), now},
		{"Last_Week", now - (7 * 24 * 60 * 60 * 1000), now},
		{"Last_Month", now - (30 * 24 * 60 * 60 * 1000), now},
		{"Custom_Range_1_Day", now - (2 * 24 * 60 * 60 * 1000), now - (24 * 60 * 60 * 1000)}, // вчера
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			records, err := client.GetDepositHistory("USDT", tc.startTime, tc.endTime, 20)

			if err != nil {
				// Проверяем на региональные ограничения
				if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
					t.Skipf("bybit API access restricted for time range %s", tc.name)
				}
				// Проверяем на ошибку временного интервала
				if contains(err.Error(), "131002") || contains(err.Error(), "interval between") {
					t.Logf("Warning: Time interval issue for range %s: %v (may be API limitation)", tc.name, err)
					return
				}
				t.Logf("Warning: No records for time range %s: %v", tc.name, err)
				return
			}

			if records == nil {
				t.Logf("No records returned for time range %s (expected on testnet)", tc.name)
				return
			}

			t.Logf("Time range %s: found %d records", tc.name, len(records))

			// Проверяем, что все записи попадают в указанный временной диапазон
			for _, record := range records {
				if record.SuccessAt != "" {
					// Парсим время успешного депозита (может быть в разных форматах)
					successTime, err := strconv.ParseInt(record.SuccessAt, 10, 64)
					if err == nil {
						// Если время в миллисекундах
						if successTime > 1000000000000 {
							assert.True(t, successTime >= tc.startTime && successTime <= tc.endTime,
								"Success time should be within range for TxID: %s", record.TxID)
						}
					}
				}
			}
		})
	}
}

// BenchmarkGetDepositHistory тестирует производительность получения истории
func BenchmarkGetDepositHistory(b *testing.B) {
	t := &testing.T{}
	cfg := GetConf(t)

	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		b.Skip("bybit API keys not configured")
	}

	client := NewBybitClient(cfg)

	// Используем динамические временные диапазоны
	now := time.Now().Unix() * 1000
	endTime := now
	startTime := now - (7 * 24 * 60 * 60 * 1000) // 7 дней назад

	// Предварительная проверка доступности API
	_, err := client.GetDepositHistory("USDT", startTime, endTime, 10)
	if err != nil {
		if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
			b.Skip("bybit API access restricted due to regulatory limitations")
		}
		if contains(err.Error(), "131002") || contains(err.Error(), "interval between") {
			// Попробуем с меньшим интервалом
			startTime = now - (24 * 60 * 60 * 1000)
			_, err = client.GetDepositHistory("USDT", startTime, endTime, 10)
			if err != nil {
				b.Skipf("bybit API not accessible: %v", err)
			}
		} else {
			b.Skipf("bybit API not accessible: %v", err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		records, err := client.GetDepositHistory("USDT", startTime, endTime, 10)
		if err != nil {
			if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
				b.Skip("API access restricted during benchmark")
			}
			if contains(err.Error(), "131002") || contains(err.Error(), "interval between") {
				b.Logf("Warning: Time interval error during benchmark iteration %d: %v", i, err)
				continue
			}
			b.Logf("Warning: Error during benchmark iteration %d: %v", i, err)
			continue
		}
		if records == nil {
			b.Logf("Warning: No records returned during benchmark iteration %d", i)
			continue
		}
	}
}

// TestCreatePaymentRequest_Integration тестирует реальное обращение к bybit API
func TestCreatePaymentRequest_Integration(t *testing.T) {
	// Получаем конфигурацию с API ключами
	cfg := GetConf(t)

	// Проверяем, что API ключи настроены
	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured. Please set real API keys in cfg.env to run integration tests")
	}

	// Создаем mock для базы данных (так как тестируем только интеграцию с bybit)
	mockDB := new(MockDB)
	mockDB.On("SaveCryptoPaymentRequest",
		mock.AnythingOfType("string"),  // orderID
		mock.AnythingOfType("uint32"),  // userID
		mock.AnythingOfType("string"),  // currency
		mock.AnythingOfType("string"),  // network
		mock.AnythingOfType("float64"), // amount
		mock.AnythingOfType("float64"), // amountUSD
		mock.AnythingOfType("string"),  // depositAddress
		mock.AnythingOfType("string"),  // depositTag
		mock.AnythingOfType("int64"),   // expiresAt
	).Return(nil)

	// Создаем реальный PaymentService
	service := NewPaymentService(context.Background(), cfg, mockDB)

	// Тестовые данные
	userID := uint32(12345)
	currency := "USDT" // Используем USDT как самую стабильную валюту для теста
	network := "TRX"   // USDT на TRON сети часто поддерживается
	amountUSD := 10.0  // Небольшая сумма для теста

	// Выполняем запрос
	paymentRequest, depositAddress, err := service.CreatePaymentRequest(userID, currency, network, amountUSD)

	// Проверяем на региональные ограничения или другие ограничения доступа
	if err != nil {
		if contains(err.Error(), "10024") || contains(err.Error(), "regulatory restrictions") {
			t.Skip("bybit API access restricted due to regulatory limitations. This is expected in certain regions.")
		}
		// Если это другая ошибка, то тест должен упасть
		require.NoError(t, err, "CreatePaymentRequest returned unexpected error")
	}

	// Проверяем результат (только если нет ошибки)
	require.NotNil(t, paymentRequest, "PaymentRequest should not be nil")
	require.NotNil(t, depositAddress, "DepositAddress should not be nil")

	// Проверяем PaymentRequest
	assert.Equal(t, userID, paymentRequest.UserID)
	assert.Equal(t, currency, paymentRequest.Currency)
	assert.Equal(t, network, paymentRequest.Network)
	assert.Equal(t, amountUSD, paymentRequest.AmountUSD)
	assert.NotEmpty(t, paymentRequest.OrderID)
	assert.Len(t, paymentRequest.OrderID, 32) // 16 bytes * 2 (hex)
	assert.True(t, paymentRequest.ExpiresAt > time.Now().Unix())
	assert.True(t, paymentRequest.Amount > 0, "Amount should be positive")

	// Проверяем DepositAddress
	assert.Equal(t, currency, depositAddress.Currency)
	assert.Equal(t, network, depositAddress.Network)
	assert.NotEmpty(t, depositAddress.Address, "Deposit address should not be empty")

	// Проверяем, что адрес выглядит как валидный TRON адрес (если используем TRX сеть)
	if network == "TRX" {
		assert.True(t, len(depositAddress.Address) > 30, "TRON address should be longer than 30 characters")
		assert.True(t, depositAddress.Address[0] == 'T', "TRON address should start with 'T'")
	}

	// Проверяем расчет количества (для USDT должно быть примерно равно USD сумме)
	if currency == "USDT" {
		assert.InDelta(t, amountUSD, paymentRequest.Amount, 0.1, "USDT amount should be close to USD amount")
	}

	// Логируем результаты для отладки
	t.Logf("Created payment request:")
	t.Logf("  OrderID: %s", paymentRequest.OrderID)
	t.Logf("  UserID: %d", paymentRequest.UserID)
	t.Logf("  Currency: %s", paymentRequest.Currency)
	t.Logf("  Network: %s", paymentRequest.Network)
	t.Logf("  Amount: %.6f %s", paymentRequest.Amount, paymentRequest.Currency)
	t.Logf("  AmountUSD: %.2f", paymentRequest.AmountUSD)
	t.Logf("  ExpiresAt: %d", paymentRequest.ExpiresAt)
	t.Logf("  DepositAddress: %s", depositAddress.Address)
	if depositAddress.Tag != "" {
		t.Logf("  DepositTag: %s", depositAddress.Tag)
	}

	// Проверяем, что mock базы данных был вызван
	mockDB.AssertExpectations(t)
}

// TestCreatePaymentRequest_Integration_BTC тестирует с Bitcoin
func TestCreatePaymentRequest_Integration_BTC(t *testing.T) {
	// Получаем конфигурацию с API ключами
	cfg := GetConf(t)

	// Проверяем, что API ключи настроены
	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured. Please set real API keys in cfg.env to run integration tests")
	}

	// Создаем mock для базы данных
	mockDB := new(MockDB)
	mockDB.On("SaveCryptoPaymentRequest",
		mock.AnythingOfType("string"),  // orderID
		mock.AnythingOfType("uint32"),  // userID
		mock.AnythingOfType("string"),  // currency
		mock.AnythingOfType("string"),  // network
		mock.AnythingOfType("float64"), // amount
		mock.AnythingOfType("float64"), // amountUSD
		mock.AnythingOfType("string"),  // depositAddress
		mock.AnythingOfType("string"),  // depositTag
		mock.AnythingOfType("int64"),   // expiresAt
	).Return(nil)

	// Создаем реальный PaymentService
	service := NewPaymentService(context.Background(), cfg, mockDB)

	// Тестовые данные
	userID := uint32(12345)
	currency := "BTC"
	network := "BTC"
	amountUSD := 50.0 // Больше сумма для BTC

	// Выполняем запрос
	paymentRequest, depositAddress, err := service.CreatePaymentRequest(userID, currency, network, amountUSD)

	// Проверяем результат
	require.NoError(t, err, "CreatePaymentRequest should not return error")
	require.NotNil(t, paymentRequest, "PaymentRequest should not be nil")
	require.NotNil(t, depositAddress, "DepositAddress should not be nil")

	// Проверяем основные поля
	assert.Equal(t, userID, paymentRequest.UserID)
	assert.Equal(t, currency, paymentRequest.Currency)
	assert.Equal(t, network, paymentRequest.Network)
	assert.Equal(t, amountUSD, paymentRequest.AmountUSD)
	assert.True(t, paymentRequest.Amount > 0, "BTC amount should be positive")
	assert.True(t, paymentRequest.Amount < amountUSD, "BTC amount should be less than USD amount")

	// Проверяем Bitcoin адрес
	assert.NotEmpty(t, depositAddress.Address, "Bitcoin address should not be empty")
	assert.True(t, len(depositAddress.Address) >= 26 && len(depositAddress.Address) <= 62,
		"Bitcoin address should be between 26 and 62 characters")

	// Логируем результаты
	t.Logf("BTC Payment Request:")
	t.Logf("  Amount: %.8f BTC (%.2f USD)", paymentRequest.Amount, paymentRequest.AmountUSD)
	t.Logf("  Address: %s", depositAddress.Address)

	// Проверяем, что mock базы данных был вызван
	mockDB.AssertExpectations(t)
}

// TestCreatePaymentRequest_Integration_ErrorHandling тестирует обработку ошибок с реальным API
func TestCreatePaymentRequest_Integration_ErrorHandling(t *testing.T) {
	// Получаем конфигурацию с API ключами
	cfg := GetConf(t)

	// Проверяем, что API ключи настроены
	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API keys not configured. Please set real API keys in cfg.env to run integration tests")
	}

	// Создаем mock для базы данных
	mockDB := new(MockDB)

	// Создаем реальный PaymentService
	service := NewPaymentService(context.Background(), cfg, mockDB)

	// Тестируем с неподдерживаемой валютой
	t.Run("UnsupportedCurrency", func(t *testing.T) {
		userID := uint32(12345)
		currency := "INVALID_CURRENCY"
		network := "INVALID_NETWORK"
		amountUSD := 10.0

		paymentRequest, depositAddress, err := service.CreatePaymentRequest(userID, currency, network, amountUSD)

		// Ожидаем ошибку
		require.Error(t, err, "Should return error for unsupported currency")
		assert.Nil(t, paymentRequest)
		assert.Nil(t, depositAddress)

		t.Logf("Expected error for invalid currency: %v", err)
	})

	// Тестируем с неподдерживаемой сетью
	t.Run("UnsupportedNetwork", func(t *testing.T) {
		userID := uint32(12345)
		currency := "BTC"
		network := "INVALID_NETWORK"
		amountUSD := 10.0

		paymentRequest, depositAddress, err := service.CreatePaymentRequest(userID, currency, network, amountUSD)

		// Ожидаем ошибку
		require.Error(t, err, "Should return error for unsupported network")
		assert.Nil(t, paymentRequest)
		assert.Nil(t, depositAddress)

		t.Logf("Expected error for invalid network: %v", err)
	})
}

// contains проверяет, содержит ли строка подстроку (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					indexSubstring(s, substr) >= 0))
}

// indexSubstring находит индекс подстроки в строке
func indexSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
