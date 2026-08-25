package repository

import (
	"context"
	"encoding/json"

	"github.com/ikermy/air-common/pkg/comdb"
)

type InternalDBRepository interface {
	GetUserTariff(ctx context.Context, userId uint32) (json.RawMessage, error)
	SetBillingData(ctx context.Context, userId uint32, payment float64, months uint8, discount float64) error
	SaveCryptoPaymentRequest(ctx context.Context, orderID string, userID uint32, currency, network string, amount, amountUSD float64, depositAddress, depositTag string, expiresAt int64) error
	GetCryptoPaymentStatus(ctx context.Context, orderID string) (json.RawMessage, error)
	UpdateCryptoPaymentStatus(ctx context.Context, orderID, status, txHash string, receivedAmount float64, confirmations int) error
	GetPendingCryptoPayments(ctx context.Context) (json.RawMessage, error)
	CheckExpiredCryptoPayments(ctx context.Context) error
	GetCryptoPaymentDepositAddress(ctx context.Context, orderID string) (string, error)
	GetCryptoPaymentCreatedAt(ctx context.Context, orderID string) (int64, error)
	CheckIfTxHashAlreadyUsed(ctx context.Context, txHash string) (string, error)
	FindActivePaymentByAddress(ctx context.Context, userID uint32, currency, network, address string) (string, error)
	GetActivePaymentsByUserAndCurrency(ctx context.Context, userID uint32, currency, network string) (json.RawMessage, error)
	UpdatePaymentExpiration(ctx context.Context, orderID string, newExpiresAt int64) error
}

type ExternalDBRepository interface {
	comdb.Exterior
}

type Repository struct {
	Internal InternalDBRepository
	External ExternalDBRepository
}
