package bybit

import (
	"air_payment/internal/domain"
	"fmt"
	"strconv"
	"strings"

	"github.com/hirokisan/bybit/v2"
	"github.com/ikermy/air_logger/v2/pkg/logger"
)

// Client обертка над hirokisan/bybit клиентом
type Client struct {
	client *bybit.Client
}

// NewBybitClient создает новый клиент bybit используя библиотеку hirokisan/bybit
func NewBybitClient(conf *domain.Bybit) *Client {
	client := bybit.NewClient().WithAuth(conf.ApiKey, conf.SecretKey)

	if conf.IsTestnet {
		client = client.WithBaseURL(bybit.TestNetBaseURL)
	}

	return &Client{
		client: client,
	}
}

// GetSupportedCurrencies получает список поддерживаемых валют
func (c *Client) GetSupportedCurrencies() ([]SupportedCurrency, error) {
	// Используем V5 Asset API для получения информации о монетах
	resp, err := c.client.V5().Asset().GetCoinInfo(bybit.V5GetCoinInfoParam{})
	if err != nil {
		return nil, fmt.Errorf("failed to get coin info: %w", err)
	}

	var currencies []SupportedCurrency
	for _, row := range resp.Result.Rows {
		for _, chain := range row.Chains {
			// Проверяем, доступны ли депозиты
			if chain.ChainDeposit == "1" {
				minDeposit, _ := strconv.ParseFloat(chain.DepositMin, 64)
				currencies = append(currencies, SupportedCurrency{
					Symbol:      string(row.Coin), // Конвертируем bybit.Coin в string
					Name:        row.Name,
					MinDeposit:  minDeposit,
					Network:     chain.Chain,
					IsAvailable: true,
				})
			}
		}
	}

	return currencies, nil
}

// GetDepositAddress получает адрес для депозита
func (c *Client) GetDepositAddress(currency, network string) (*DepositAddress, error) {
	//logger.Debug("Get Deposit Address currency: %s network: %s", currency, network)

	// Список возможных chainType для каждой сети
	var possibleChainTypes []string
	switch network {
	case "TRX":
		possibleChainTypes = []string{"TRX", "TRON (TRC20)", "TRC20", "TRON"}
	case "ETH":
		possibleChainTypes = []string{"Ethereum (ERC20)", "ERC20", "ETH", "Ethereum"}
	case "BSC":
		possibleChainTypes = []string{"BSC (BEP20)", "BEP20", "BSC", "BNB"}
	case "POLYGON":
		possibleChainTypes = []string{"Polygon PoS", "POLYGON", "MATIC"}
	case "BTC":
		possibleChainTypes = []string{"BTC", "Bitcoin"}
	case "SOL":
		possibleChainTypes = []string{"SOL"}
	case "TON":
		possibleChainTypes = []string{"TON"}
	default:
		possibleChainTypes = []string{network}
	}

	// Пробуем каждый возможный chainType
	coin := bybit.Coin(currency)
	for _, chainType := range possibleChainTypes {
		//logger.Debug("Trying chainType: %s for network: %s", chainType, network)

		resp, err := c.client.V5().Asset().GetMasterDepositAddress(bybit.V5GetMasterDepositAddressParam{
			Coin:      coin,
			ChainType: &chainType,
		})
		if err != nil {
			logger.Debug("Failed chainType %s: %v", chainType, err)
			continue
		}

		if len(resp.Result.Chains) > 0 {
			chain := resp.Result.Chains[0]
			//logger.Info("Successfully found deposit address using chainType: %s", chainType)
			//logger.Debug("Chain details: chainType=%s, address=%s, tag=%s",
			//	chain.ChainType, chain.AddressDeposit, chain.TagDeposit)

			return &DepositAddress{
				Currency: currency,
				Address:  chain.AddressDeposit,
				Tag:      chain.TagDeposit,
				Network:  network,
			}, nil
		}
	}

	// Если все chainType не сработали, попробуем без указания chainType
	logger.Debug("All specific chainTypes failed, trying without chainType filter")
	resp, err := c.client.V5().Asset().GetMasterDepositAddress(bybit.V5GetMasterDepositAddressParam{
		Coin: coin,
	})
	if err != nil {
		logger.Error("Failed to get any deposit address for %s: %v", currency, err)
		return nil, fmt.Errorf("failed to get deposit address for %s: %w", currency, err)
	}

	logger.Debug("Available chains count: %d", len(resp.Result.Chains))
	for i, chain := range resp.Result.Chains {
		logger.Debug("Available chain %d: chainType=%s, chain=%s, address=%s",
			i, chain.ChainType, chain.Chain, chain.AddressDeposit)
	}

	// Ищем подходящую сеть среди всех доступных
	for _, chain := range resp.Result.Chains {
		// Проверяем совпадение по различным критериям
		if c.isMatchingNetwork(chain.ChainType, chain.Chain, network) {
			logger.Info("Found matching chain: chainType=%s, chain=%s for network=%s",
				chain.ChainType, chain.Chain, network)

			return &DepositAddress{
				Currency: currency,
				Address:  chain.AddressDeposit,
				Tag:      chain.TagDeposit,
				Network:  network,
			}, nil
		}
	}

	// Если ничего не найдено, возвращаем первую доступную сеть (fallback)
	if len(resp.Result.Chains) > 0 {
		chain := resp.Result.Chains[0]
		logger.Warn("No exact match found, using fallback chain: %s for requested network: %s",
			chain.ChainType, network)

		return &DepositAddress{
			Currency: currency,
			Address:  chain.AddressDeposit,
			Tag:      chain.TagDeposit,
			Network:  network,
		}, nil
	}

	return nil, fmt.Errorf("no deposit address available for %s", currency)
}

// isMatchingNetwork проверяет, соответствует ли chainType/chain запрашиваемой сети
func (c *Client) isMatchingNetwork(chainType, chain, requestedNetwork string) bool {
	// Приводим к нижнему регистру для сравнения
	chainTypeLower := strings.ToLower(chainType)
	chainLower := strings.ToLower(chain)
	networkLower := strings.ToLower(requestedNetwork)

	// Прямые совпадения
	if chainTypeLower == networkLower || chainLower == networkLower {
		return true
	}

	// Специальные случаи
	switch networkLower {
	case "trx":
		return strings.Contains(chainTypeLower, "trc") ||
			strings.Contains(chainTypeLower, "tron") ||
			strings.Contains(chainLower, "tron")
	case "eth":
		return strings.Contains(chainTypeLower, "erc") ||
			strings.Contains(chainTypeLower, "eth") ||
			strings.Contains(chainLower, "eth")
	case "bsc":
		return strings.Contains(chainTypeLower, "bep") ||
			strings.Contains(chainTypeLower, "bsc") ||
			strings.Contains(chainLower, "bsc")
	case "btc":
		return strings.Contains(chainTypeLower, "btc") ||
			strings.Contains(chainLower, "btc")
	}

	return false
}

// GeneratePaymentURI генерирует URI для QR-кода в соответствии с стандартами кошельков
//func (c *Client) GeneratePaymentURI(currency, network, address string, amount float64) string {
//	switch network {
//	case "TRX", "TRC20":
//		// Для TRON сети используем tron: схему
//		if currency == "TRX" {
//			return fmt.Sprintf("tron:%s?amount=%.6f", address, amount)
//		} else {
//			// Для токенов TRC20 (например USDT)
//			return fmt.Sprintf("tron:%s", address)
//		}
//	case "ETH", "ERC20":
//		// Для Ethereum используем ethereum: схему
//		if currency == "ETH" {
//			return fmt.Sprintf("ethereum:%s?value=%.18f", address, amount*1e18) // ETH в wei
//		} else {
//			// Для токенов ERC20
//			return fmt.Sprintf("ethereum:%s", address)
//		}
//	case "BSC", "BEP20":
//		// Для Binance Smart Chain
//		if currency == "BNB" {
//			return fmt.Sprintf("bnb:%s?amount=%.8f", address, amount)
//		} else {
//			return fmt.Sprintf("bnb:%s", address)
//		}
//	case "BTC":
//		// Для Bitcoin используем bitcoin: схему
//		return fmt.Sprintf("bitcoin:%s?amount=%.8f", address, amount)
//	default:
//		// По умолчанию возвращаем просто адрес
//		return address
//	}
//}

// GetDepositHistory получает историю депозитов (используется в мониторе)
func (c *Client) GetDepositHistory(currency string, startTime, endTime int64, limit int) ([]DepositRecord, error) {
	coin := bybit.Coin(currency)
	resp, err := c.client.V5().Asset().GetDepositRecords(bybit.V5GetDepositRecordsParam{
		Coin:      &coin,
		StartTime: &startTime,
		EndTime:   &endTime,
		Limit:     &limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get deposit history: %w", err)
	}

	var records []DepositRecord
	for _, row := range resp.Result.Rows {
		records = append(records, DepositRecord{
			Coin:          string(row.Coin),
			Chain:         row.Chain,
			Amount:        row.Amount,
			TxID:          row.TxID,
			Status:        int(row.Status),
			ToAddress:     row.ToAddress,
			Tag:           row.Tag,
			DepositFee:    row.DepositFee,
			SuccessAt:     row.SuccessAt,
			Confirmations: row.Confirmations,
			TxIndex:       row.TxIndex,
			BlockHash:     row.BlockHash,
		})
	}

	return records, nil
}

// GetCurrencyPrice получает актуальный курс валюты к USDT
func (c *Client) GetCurrencyPrice(currency string) (float64, error) {
	// Если это уже стейблкоин, возвращаем 1
	if currency == "USDT" || currency == "USDC" || currency == "BUSD" {
		return 1.0, nil
	}

	// Формируем символ для запроса (например, BTCUSDT)
	symbolStr := currency + "USDT"
	symbol := bybit.SymbolV5(symbolStr)

	// Используем V5 Market API для получения ticker информации
	resp, err := c.client.V5().Market().GetTickers(bybit.V5GetTickersParam{
		Category: bybit.CategoryV5Spot, // Спотовый рынок
		Symbol:   &symbol,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get ticker for %s: %w", symbolStr, err)
	}

	// Получаем последнюю цену из спот данных
	var lastPrice string
	if resp.Result.Spot != nil && len(resp.Result.Spot.List) > 0 {
		lastPrice = resp.Result.Spot.List[0].LastPrice
	} else if resp.Result.LinearInverse != nil && len(resp.Result.LinearInverse.List) > 0 {
		lastPrice = resp.Result.LinearInverse.List[0].LastPrice
	} else {
		// Попробуем альтернативный подход - получить цену через другой метод
		logger.Debug("No price found in standard response, trying alternative method")
		return c.getCurrencyPriceAlternative(currency)
	}

	if lastPrice == "" {
		return 0, fmt.Errorf("empty price data for %s", symbolStr)
	}

	price, err := strconv.ParseFloat(lastPrice, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price %s: %w", lastPrice, err)
	}

	return price, nil
}

// getCurrencyPriceAlternative альтернативный метод получения цены через Kline данные
func (c *Client) getCurrencyPriceAlternative(currency string) (float64, error) {
	symbolStr := currency + "USDT"
	symbol := bybit.SymbolV5(symbolStr)

	// Получаем последние kline данные (1-минутные свечи)
	resp, err := c.client.V5().Market().GetKline(bybit.V5GetKlineParam{
		Category: bybit.CategoryV5Spot,
		Symbol:   symbol,
		Interval: bybit.Interval1,
		Limit:    &[]int{1}[0], // Получаем только последнюю свечу
	})
	if err != nil {
		return 0, fmt.Errorf("alternative method failed to get kline for %s: %w", symbolStr, err)
	}

	if len(resp.Result.List) == 0 {
		return 0, fmt.Errorf("no kline data found for %s", symbolStr)
	}

	// Берем цену закрытия последней свечи - используем поле Close вместо индекса
	closePrice := resp.Result.List[0].Close

	price, err := strconv.ParseFloat(closePrice, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse alternative price %s: %w", closePrice, err)
	}

	logger.Debug("Alternative method price for %s: %f\n", currency, price)
	return price, nil
}
