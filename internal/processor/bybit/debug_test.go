//go:build integration

package bybit

import (
	"testing"

	"github.com/hirokisan/bybit/v2"
	"github.com/ikermy/air-logger/v2/pkg/logger"
)

// TestDebugAvailableChains тестирует получение доступных сетей для USDT
func TestDebugAvailableChains(t *testing.T) {
	// Загружаем конфигурацию
	cfg := GetConf(t)

	// Пропускаем тест если нет API ключей
	if cfg.ApiKey == "" || cfg.SecretKey == "" || cfg.IsTestnet == false {
		t.Skip("bybit API credentials not configured")
	}

	// Создаем клиент
	client := NewBybitClient(cfg)

	// Тестируем получение всех доступных сетей для USDT
	t.Run("GetAllChainsForUSDT", func(t *testing.T) {
		resp, err := client.client.V5().Asset().GetMasterDepositAddress(bybit.V5GetMasterDepositAddressParam{
			Coin: bybit.Coin("USDT"),
			// Без указания chainType - получим все доступные
		})
		if err != nil {
			t.Fatalf("Failed to get all chains for USDT: %v", err)
		}

		logger.Info("=== Available chains for USDT ===")
		for i, chain := range resp.Result.Chains {
			logger.Info("Chain %d:", i)
			logger.Info("  ChainType: %s", chain.ChainType)
			logger.Info("  Chain: %s", chain.Chain)
			logger.Info("  Address: %s", chain.AddressDeposit)
			logger.Info("  Tag: %s", chain.TagDeposit)
			logger.Info("  BatchReleaseLimit: %s", chain.BatchReleaseLimit)
		}
	})

	// Тестируем получение конкретной сети TRC20
	t.Run("GetTRC20ChainForUSDT", func(t *testing.T) {
		chainType := "TRC20"
		resp, err := client.client.V5().Asset().GetMasterDepositAddress(bybit.V5GetMasterDepositAddressParam{
			Coin:      bybit.Coin("USDT"),
			ChainType: &chainType,
		})
		if err != nil {
			t.Fatalf("Failed to get TRC20 chain for USDT: %v", err)
		}

		logger.Info("=== TRC20 chain for USDT ===")
		logger.Info("Chains count: %d", len(resp.Result.Chains))
		for i, chain := range resp.Result.Chains {
			logger.Info("Chain %d:", i)
			logger.Info("  ChainType: %s", chain.ChainType)
			logger.Info("  Chain: %s", chain.Chain)
			logger.Info("  Address: %s", chain.AddressDeposit)
			logger.Info("  Tag: %s", chain.TagDeposit)
		}
	})

	// Тестируем различные варианты chainType
	t.Run("TestDifferentChainTypes", func(t *testing.T) {
		chainTypes := []string{"TRC20", "TRON", "TRX"}

		for _, ct := range chainTypes {
			logger.Info("=== Testing chainType: %s ===", ct)
			resp, err := client.client.V5().Asset().GetMasterDepositAddress(bybit.V5GetMasterDepositAddressParam{
				Coin:      bybit.Coin("USDT"),
				ChainType: &ct,
			})
			if err != nil {
				logger.Error("Error for chainType %s: %v", ct, err)
				continue
			}
			logger.Info("Success for chainType %s, chains count: %d", ct, len(resp.Result.Chains))
		}
	})
}
