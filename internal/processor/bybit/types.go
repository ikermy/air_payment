package bybit

import (
	"encoding/json"
	"strings"
	"time"
)

// MySQLDateTime кастомный тип для работы с MySQL DATETIME форматом
type MySQLDateTime struct {
	time.Time
}

// UnmarshalJSON кастомный unmarshaler для MySQL DATETIME формата
func (mt *MySQLDateTime) UnmarshalJSON(data []byte) error {
	// Убираем кавычки из JSON строки
	str := strings.Trim(string(data), `"`)

	// Если строка пустая или null, устанавливаем нулевое время
	if str == "" || str == "null" {
		mt.Time = time.Time{}
		return nil
	}

	// Пытаемся парсить в MySQL DATETIME формате
	parsedTime, err := time.Parse("2006-01-02 15:04:05", str)
	if err != nil {
		// Если не получилось, пытаемся в RFC3339 формате (для обратной совместимости)
		parsedTime, err = time.Parse(time.RFC3339, str)
		if err != nil {
			return err
		}
	}

	mt.Time = parsedTime
	return nil
}

// MarshalJSON кастомный marshaler для MySQL DATETIME формата
func (mt MySQLDateTime) MarshalJSON() ([]byte, error) {
	if mt.Time.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(mt.Time.Format("2006-01-02 15:04:05"))
}

// PaymentRequest запрос на оплату
type PaymentRequest struct {
	UserID uint64  `json:"userId"`
	Currency  string  `json:"currency"`
	Amount    float64 `json:"amount"`
	AmountUSD float64 `json:"amountUsd"`
	OrderID   string  `json:"orderId"`
	Network   string  `json:"network"`
	ExpiresAt int64   `json:"expiresAt"`
}

// DepositAddress адрес для депозита
type DepositAddress struct {
	Currency string `json:"currency"`
	Address  string `json:"address"`
	Tag      string `json:"tag,omitempty"` // Для некоторых валют (XRP, EOS)
	Network  string `json:"network"`
	QRCode   string `json:"qrCode,omitempty"`
}

// PaymentStatus статус платежа
type PaymentStatus struct {
	OrderID        string        `json:"orderId"`
	UserID uint64        `json:"userId"`
	Status         string        `json:"status"` // pending, confirmed, failed, expired
	Currency       string        `json:"currency"`
	Network        string        `json:"network"`
	Amount         float64       `json:"amount"`
	AmountUsd      float64       `json:"amountUsd"`
	ReceivedAmount float64       `json:"receivedAmount"`
	TxHash         string        `json:"txHash"`
	Confirmations  int           `json:"confirmations"`
	CreatedAt      MySQLDateTime `json:"createdAt"`
	UpdatedAt      MySQLDateTime `json:"updatedAt"`
	ExpiresAt      MySQLDateTime `json:"expiresAt"`
}

// SupportedCurrency поддерживаемые валюты
type SupportedCurrency struct {
	Symbol      string  `json:"symbol"`
	Name        string  `json:"name"`
	MinDeposit  float64 `json:"minDeposit"`
	Network     string  `json:"network"`
	IsAvailable bool    `json:"isAvailable"`
}

// PaymentCalculation предварительный расчет платежа
type PaymentCalculation struct {
	Currency         string  `json:"currency"`
	Network          string  `json:"network"`
	AmountUSD        float64 `json:"amountUsd"`
	CryptoAmount     float64 `json:"cryptoAmount"`
	CurrencyPrice    float64 `json:"currencyPrice"`
	ExpiresInMinutes int     `json:"expiresInMinutes"`
	CalculatedAt     int64   `json:"calculatedAt"`
}
