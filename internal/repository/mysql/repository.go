package mysql

import (
	"air_payment/internal/metrics"
	"air_payment/internal/repository"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ikermy/air_common/pkg/comdb"
	"github.com/ikermy/air_common/pkg/mode"
	"github.com/ikermy/air_logger/v2/pkg/logger"
)

type Implementation struct {
	db *comdb.DB
}

func New(db *comdb.DB) (repository.Repository, error) {
	if db == nil {
		return repository.Repository{}, fmt.Errorf("database connection is nil")
	}

	userRepo := &Implementation{db: db}

	return repository.Repository{
		Internal: userRepo,
		External: db,
	}, nil
}

// NullBytes Промежуточный тип для загрузки массива байт из базы
type NullBytes struct {
	Bytes []byte
	Valid bool // Valid = true, если Bytes не NULL
}

// Scan Реализация интерфейса sql.Scanner
func (nb *NullBytes) Scan(value any) error {
	if value == nil {
		nb.Bytes = nil
		nb.Valid = false
		return nil
	}

	switch v := value.(type) {
	case []byte:
		nb.Bytes = v
		nb.Valid = true
	default:
		return fmt.Errorf("невозможно преобразовать тип %T в NullBytes", value)
	}
	return nil
}

func (r *Implementation) GetUserTariff(ctx context.Context, userID uint32) (json.RawMessage, error) {
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	// Выполняем вызов хранимой функции
	//var result string
	var result []byte
	err := r.db.Conn().QueryRowContext(ctx, "SELECT GetUserTariff(?);", userID).Scan(&result)
	if err != nil {
		return nil, fmt.Errorf("ошибка вызова хранимой функции GetUserTariff: %w", err)
	}

	// Проверяем корректность результата
	if len(result) == 0 {
		return nil, fmt.Errorf("пустой результат от GetUserTariff")
	}

	// Преобразуем результат в json.RawMessage
	rawMessage := json.RawMessage(result)

	return rawMessage, nil
}

func (r *Implementation) SetBillingData(ctx context.Context, userId uint32, payment float64, months uint8, discount float64) error {
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	// Начинаем транзакцию
	tx, err := r.db.Conn().Begin()
	if err != nil {
		return fmt.Errorf("не удалось начать транзакцию SetBillingData: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// 1) Получаем currency из users
	var userCurrency uint8
	if err = tx.QueryRowContext(ctx, "SELECT currency FROM users WHERE Id = ?", userId).Scan(&userCurrency); err != nil {
		return fmt.Errorf("ошибка получения currency пользователя: %w", err)
	}

	// 2) Вставляем данные в billing
	_, err = tx.Exec(`INSERT INTO billing (UserId, Payment, Months, Discont, Currency, Date)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		userId, payment, months, discount, userCurrency)
	if err != nil {
		return fmt.Errorf("ошибка вставки записи в billing: %w", err)
	}

	// 3) Получаем текущую конечную дату подписки
	var endDate sql.NullTime
	if err = tx.QueryRowContext(ctx, "SELECT EndDate FROM subscriptions WHERE UserId = ?", userId).Scan(&endDate); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("ошибка получения EndDate из subscriptions: %w", err)
		}
	}

	// 4) Рассчитываем новую дату окончания
	var newEndDate time.Time
	if endDate.Valid {
		newEndDate = endDate.Time.AddDate(0, int(months), 0)
	} else {
		newEndDate = time.Now().AddDate(0, int(months), 0)
	}

	// 5) Обновляем данные в subscriptions
	_, err = tx.ExecContext(ctx, `UPDATE subscriptions
		SET EndDate = ?,
			Discount = ?,
			MonthsPaid = ?,
			TotalCost = ?,
			Notified = FALSE
		WHERE UserId = ?`,
		newEndDate.Format("2006-01-02"), discount, months, payment, userId)
	if err != nil {
		return fmt.Errorf("ошибка обновления subscriptions: %w", err)
	}

	// 6) Проверяем RoleId пользователя
	var roleId int
	if err = tx.QueryRowContext(ctx, "SELECT RoleId FROM users WHERE Id = ?", userId).Scan(&roleId); err != nil {
		return fmt.Errorf("ошибка получения RoleId пользователя: %w", err)
	}
	if roleId == 1 { // Demo
		_, err = tx.ExecContext(ctx, "UPDATE users SET RoleId = 2 WHERE Id = ?", userId)
		if err != nil {
			return fmt.Errorf("ошибка обновления RoleId пользователя: %w", err)
		}
	}

	// Коммит
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("ошибка коммита транзакции SetBillingData: %w", err)
	}

	return nil
}

// SaveCryptoPaymentRequest сохраняет запрос на криптоплатёж через хранимую процедуру
func (r *Implementation) SaveCryptoPaymentRequest(ctx context.Context, orderID string, userID uint32, currency, network string, amount, amountUSD float64, depositAddress, depositTag string, expiresAt int64) error {
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	// Логируем параметры для отладки
	logger.Debug("SaveCryptoPaymentRequest params: orderID=%s, userID=%d, currency=%s, network=%s, amount=%f, amountUSD=%f, depositAddress=%s, depositTag=%s, expiresAt=%d",
		orderID, userID, currency, network, amount, amountUSD, depositAddress, depositTag, expiresAt)

	// Конвертируем Unix timestamp в MySQL DATETIME
	// MySQL DATETIME is stored without timezone information. Always write UTC,
	// because Unix timestamps represent an absolute moment in time and the DB
	// expiration check compares this value with UTC NOW().
	expiresAtTime := time.Unix(expiresAt, 0).UTC().Format("2006-01-02 15:04:05")

	// Прямой INSERT вместо хранимой процедуры
	_, err := r.db.Conn().ExecContext(ctx, `INSERT INTO crypto_payments (
		order_id, user_id, currency, network, amount, amount_usd,
		deposit_address, deposit_tag, expires_at, status
	) VALUES (
		?, ?, ?, ?, ?, ?,
		?, ?, ?, 'pending'
	)`,
		orderID, userID, currency, network, amount, amountUSD,
		depositAddress, depositTag, expiresAtTime,
	)
	if err != nil {
		return fmt.Errorf("ошибка выполнения INSERT SaveCryptoPaymentRequest: %w", err)
	}
	return nil
}

// GetCryptoPaymentStatus получает статус криптоплатежа через хранимую функцию
func (r *Implementation) GetCryptoPaymentStatus(ctx context.Context, orderID string) (json.RawMessage, error) {
	startedAt := time.Now()
	defer func() { metrics.ObserveDatabase("get_payment_status", "success", time.Since(startedAt).Seconds()) }()
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	// Прямой запрос вместо хранимой функции
	row := r.db.Conn().QueryRowContext(ctx, `SELECT 
		order_id,
		user_id,
		status,
		currency,
		network,
		amount,
		amount_usd,
		received_amount,
		tx_hash,
		confirmations,
		created_at,
		updated_at,
		expires_at,
		deposit_address,
		deposit_tag
	FROM crypto_payments
	WHERE order_id = ?`, orderID)

	var (
		orderId        string
		userId         uint32
		status         string
		currency       string
		network        string
		amount         float64
		amountUsd      float64
		receivedAmount sql.NullFloat64
		txHash         sql.NullString
		confirmations  sql.NullInt64
		createdAt      time.Time
		updatedAt      time.Time
		expiresAt      time.Time
		depositAddress sql.NullString
		depositTag     sql.NullString
	)

	if err := row.Scan(
		&orderId,
		&userId,
		&status,
		&currency,
		&network,
		&amount,
		&amountUsd,
		&receivedAmount,
		&txHash,
		&confirmations,
		&createdAt,
		&updatedAt,
		&expiresAt,
		&depositAddress,
		&depositTag,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("платёж не найден")
		}
		return nil, fmt.Errorf("ошибка выполнения запроса GetCryptoPaymentStatus: %w", err)
	}

	// Готовим объект для JSON, приводим NULL к значениям по умолчанию
	resp := struct {
		OrderID        string    `json:"orderId"`
		UserID         uint32    `json:"userId"`
		Status         string    `json:"status"`
		Currency       string    `json:"currency"`
		Network        string    `json:"network"`
		Amount         float64   `json:"amount"`
		AmountUSD      float64   `json:"amountUsd"`
		ReceivedAmount float64   `json:"receivedAmount"`
		TxHash         string    `json:"txHash"`
		Confirmations  int64     `json:"confirmations"`
		CreatedAt      time.Time `json:"createdAt"`
		UpdatedAt      time.Time `json:"updatedAt"`
		ExpiresAt      time.Time `json:"expiresAt"`
		DepositAddress string    `json:"depositAddress"`
		DepositTag     string    `json:"depositTag"`
	}{
		OrderID:   orderId,
		UserID:    userId,
		Status:    status,
		Currency:  currency,
		Network:   network,
		Amount:    amount,
		AmountUSD: amountUsd,
		ReceivedAmount: func() float64 {
			if receivedAmount.Valid {
				return receivedAmount.Float64
			}
			return 0
		}(),
		TxHash: func() string {
			if txHash.Valid {
				return txHash.String
			}
			return ""
		}(),
		Confirmations: func() int64 {
			if confirmations.Valid {
				return confirmations.Int64
			}
			return 0
		}(),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		ExpiresAt: expiresAt,
		DepositAddress: func() string {
			if depositAddress.Valid {
				return depositAddress.String
			}
			return ""
		}(),
		DepositTag: func() string {
			if depositTag.Valid {
				return depositTag.String
			}
			return ""
		}(),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации статуса платежа в JSON: %w", err)
	}
	return data, nil
}

// UpdateCryptoPaymentStatus обновляет статус криптоплатежа через хранимую процедуру
func (r *Implementation) UpdateCryptoPaymentStatus(ctx context.Context, orderID, status, txHash string, receivedAmount float64, confirmations int) error {
	startedAt := time.Now()
	result := "success"
	defer func() { metrics.ObserveDatabase("update_payment_status", result, time.Since(startedAt).Seconds()) }()
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	// Начинаем транзакцию
	tx, err := r.db.Conn().Begin()
	if err != nil {
		result = "error"
		return fmt.Errorf("не удалось начать транзакцию UpdateCryptoPaymentStatus: %w", err)
	}

	// Обеспечиваем откат при ошибке
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Выполняем обновление. Если txHash пустой (""), оставляем прежнее значение
	res, execErr := tx.ExecContext(ctx, `UPDATE crypto_payments 
		SET 
			status = ?,
			tx_hash = IFNULL(?, tx_hash),
			received_amount = ?,
			confirmations = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE order_id = ?`,
		status, sql.NullString{String: txHash, Valid: txHash != ""}, receivedAmount, confirmations, orderID)
	if execErr != nil {
		result = "error"
		err = fmt.Errorf("ошибка выполнения UPDATE UpdateCryptoPaymentStatus: %w", execErr)
		return err
	}

	affected, affErr := res.RowsAffected()
	if affErr != nil {
		result = "error"
		err = fmt.Errorf("ошибка получения количества затронутых строк: %w", affErr)
		return err
	}
	if affected == 0 {
		result = "error"
		err = fmt.Errorf("payment not found for update")
		return err
	}

	// Коммитим транзакцию
	if commitErr := tx.Commit(); commitErr != nil {
		result = "error"
		return fmt.Errorf("ошибка коммита транзакции UpdateCryptoPaymentStatus: %w", commitErr)
	}

	return nil
}

// GetPendingCryptoPayments получает список ожидающих криптоплатежей через хранимую функцию
func (r *Implementation) GetPendingCryptoPayments(ctx context.Context) (json.RawMessage, error) {
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	// Прямой запрос вместо хранимой функции
	rows, err := r.db.Conn().QueryContext(ctx, `SELECT 
		order_id,
		user_id,
		status,
		currency,
		network,
		amount,
		amount_usd,
		received_amount,
		tx_hash,
		confirmations,
		deposit_address,
		deposit_tag,
		created_at,
		updated_at,
		expires_at
	FROM crypto_payments
	WHERE status IN ('pending', 'partial')
	  AND expires_at > NOW()
	ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса ожидающих платежей: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type payment struct {
		OrderID        string    `json:"orderId"`
		UserID         uint32    `json:"userId"`
		Status         string    `json:"status"`
		Currency       string    `json:"currency"`
		Network        string    `json:"network"`
		Amount         float64   `json:"amount"`
		AmountUSD      float64   `json:"amountUsd"`
		ReceivedAmount float64   `json:"receivedAmount"`
		TxHash         string    `json:"txHash"`
		Confirmations  int       `json:"confirmations"`
		DepositAddress string    `json:"depositAddress"`
		DepositTag     string    `json:"depositTag"`
		CreatedAt      time.Time `json:"createdAt"`
		UpdatedAt      time.Time `json:"updatedAt"`
		ExpiresAt      time.Time `json:"expiresAt"`
	}

	var payments []payment
	for rows.Next() {
		var p payment
		var txHash sql.NullString
		if err := rows.Scan(
			&p.OrderID,
			&p.UserID,
			&p.Status,
			&p.Currency,
			&p.Network,
			&p.Amount,
			&p.AmountUSD,
			&p.ReceivedAmount,
			&txHash,
			&p.Confirmations,
			&p.DepositAddress,
			&p.DepositTag,
			&p.CreatedAt,
			&p.UpdatedAt,
			&p.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("ошибка чтения строки ожидающих платежей: %w", err)
		}
		if txHash.Valid {
			p.TxHash = txHash.String
		}
		payments = append(payments, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка итерации по ожидающим платежам: %w", err)
	}

	if len(payments) == 0 {
		return json.RawMessage("[]"), nil
	}

	data, err := json.Marshal(payments)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации ожидающих платежей в JSON: %w", err)
	}
	return data, nil
}

// CheckExpiredCryptoPayments проверяет и помечает истекшие криптоплатежи через хранимую процедуру
func (r *Implementation) CheckExpiredCryptoPayments(ctx context.Context) error {
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	// Прямой UPDATE вместо хранимой процедуры
	_, err := r.db.Conn().ExecContext(ctx, `UPDATE crypto_payments
		SET status = 'expired', updated_at = NOW()
		WHERE status IN ('pending', 'partial')
			AND expires_at < NOW()
			AND (tx_hash IS NULL OR tx_hash = '')`)
	if err != nil {
		return fmt.Errorf("ошибка выполнения обновления CheckExpiredCryptoPayments: %w", err)
	}
	return nil
}

// GetCryptoPaymentDepositAddress получает адрес депозита по orderID через хранимую функцию
func (r *Implementation) GetCryptoPaymentDepositAddress(ctx context.Context, orderID string) (string, error) {
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	var address sql.NullString
	// Прямой запрос вместо хранимой функции
	err := r.db.Conn().QueryRowContext(ctx, `SELECT deposit_address FROM crypto_payments WHERE order_id = ? LIMIT 1;`, orderID).Scan(&address)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("ошибка выполнения запроса GetCryptoPaymentDepositAddress: %w", err)
	}
	if !address.Valid {
		return "", nil
	}
	return address.String, nil
}

// GetCryptoPaymentCreatedAt получает время создания платежа по orderID через хранимую функцию
func (r *Implementation) GetCryptoPaymentCreatedAt(ctx context.Context, orderID string) (int64, error) {
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	var createdAt sql.NullInt64
	// Прямой запрос вместо хранимой функции
	err := r.db.Conn().QueryRowContext(ctx, `SELECT UNIX_TIMESTAMP(created_at) FROM crypto_payments WHERE order_id = ? LIMIT 1;`, orderID).Scan(&createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil // нет записи — как в функции БД, возвращаем 0
		}
		return 0, fmt.Errorf("ошибка выполнения запроса GetCryptoPaymentCreatedAt: %w", err)
	}
	if !createdAt.Valid {
		return 0, nil
	}
	return createdAt.Int64, nil
}

// CheckIfTxHashAlreadyUsed проверяет, не был ли txHash уже использован другим платежом
func (r *Implementation) CheckIfTxHashAlreadyUsed(ctx context.Context, txHash string) (string, error) {
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	var orderID sql.NullString
	// Прямой запрос вместо хранимой функции
	err := r.db.Conn().QueryRowContext(ctx, `SELECT order_id FROM crypto_payments
		WHERE tx_hash = ?
			AND status IN ('confirmed', 'partial')
		LIMIT 1;`, txHash).Scan(&orderID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil // txHash не использован
		}
		return "", fmt.Errorf("ошибка выполнения запроса CheckIfTxHashAlreadyUsed: %w", err)
	}
	if !orderID.Valid {
		return "", nil
	}
	return orderID.String, nil
}

// FindActivePaymentByAddress ищет активный платеж по адресу депозита с дополнительной диагностикой
func (r *Implementation) FindActivePaymentByAddress(ctx context.Context, userID uint32, currency, network, address string) (string, error) {
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	var orderID sql.NullString
	// Прямой запрос вместо хранимой функции
	err := r.db.Conn().QueryRowContext(ctx, `SELECT order_id FROM crypto_payments
		WHERE user_id = ?
			AND currency = ?
			AND network = ?
			AND deposit_address = ?
			AND status IN ('pending', 'partial')
			AND expires_at > NOW()
		ORDER BY created_at DESC
		LIMIT 1;`, userID, currency, network, address).Scan(&orderID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil // Активный платеж не найден
		}
		return "", fmt.Errorf("ошибка выполнения запроса FindActivePaymentByAddress: %w", err)
	}

	if !orderID.Valid {
		return "", nil // Активный платеж не найден
	}

	return orderID.String, nil
}

// GetActivePaymentsByUserAndCurrency получает все активные платежи пользователя по валюте и сети
func (r *Implementation) GetActivePaymentsByUserAndCurrency(ctx context.Context, userID uint32, currency, network string) (json.RawMessage, error) {
	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	// Прямой запрос вместо хранимой функции
	rows, err := r.db.Conn().QueryContext(ctx, `SELECT 
		order_id,
		user_id,
		status,
		currency,
		network,
		amount,
		amount_usd,
		received_amount,
		deposit_address,
		deposit_tag,
		tx_hash,
		confirmations,
		created_at,
		updated_at,
		expires_at
	FROM crypto_payments
	WHERE user_id = ?
		AND currency = ?
		AND network = ?
		AND status IN ('pending', 'partial')
		AND expires_at > NOW()
	ORDER BY created_at DESC`, userID, currency, network)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса активных платежей: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type payment struct {
		OrderID        string    `json:"orderId"`
		UserID         uint32    `json:"userId"`
		Status         string    `json:"status"`
		Currency       string    `json:"currency"`
		Network        string    `json:"network"`
		Amount         float64   `json:"amount"`
		AmountUSD      float64   `json:"amountUsd"`
		ReceivedAmount float64   `json:"receivedAmount"`
		DepositAddress string    `json:"depositAddress"`
		DepositTag     string    `json:"depositTag"`
		TxHash         string    `json:"txHash"`
		Confirmations  int       `json:"confirmations"`
		CreatedAt      time.Time `json:"createdAt"`
		UpdatedAt      time.Time `json:"updatedAt"`
		ExpiresAt      time.Time `json:"expiresAt"`
	}

	var payments []payment
	for rows.Next() {
		var p payment
		// Scanning all selected columns in order
		if err := rows.Scan(
			&p.OrderID,
			&p.UserID,
			&p.Status,
			&p.Currency,
			&p.Network,
			&p.Amount,
			&p.AmountUSD,
			&p.ReceivedAmount,
			&p.DepositAddress,
			&p.DepositTag,
			&p.TxHash,
			&p.Confirmations,
			&p.CreatedAt,
			&p.UpdatedAt,
			&p.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("ошибка чтения строки активных платежей: %w", err)
		}
		payments = append(payments, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка итерации по строкам активных платежей: %w", err)
	}

	if len(payments) == 0 {
		return json.RawMessage("[]"), nil
	}

	data, err := json.Marshal(payments)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации активных платежей в JSON: %w", err)
	}
	return data, nil
}

// UpdatePaymentExpiration продлевает срок действия платежа
func (r *Implementation) UpdatePaymentExpiration(ctx context.Context, orderID string, newExpiresAt int64) error {
	// Конвертируем Unix timestamp в MySQL DATETIME
	// Keep the stored DATETIME in UTC to match MySQL NOW() and avoid expiring
	// payments immediately when the application runs in another timezone.
	expiresAtTime := time.Unix(newExpiresAt, 0).UTC().Format("2006-01-02 15:04:05")

	// Дочерний контекст с тайм-аутом на операцию
	ctx, cancel := context.WithTimeout(ctx, mode.GetSQLTimeToCancel())
	defer cancel()

	_, err := r.db.Conn().ExecContext(ctx, `UPDATE crypto_payments
		SET expires_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE order_id = ? AND status IN ('pending', 'partial')`,
		expiresAtTime, orderID)
	if err != nil {
		return fmt.Errorf("ошибка обновления срока действия платежа: %w", err)
	}

	return nil
}
