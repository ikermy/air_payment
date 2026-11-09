//go:build integration

package mysql

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/ikermy/air-common/v2/pkg/comdb"
)

func TestGetUserTariffV2Local(t *testing.T) {
	ctx := context.Background()
	base, err := comdb.New(ctx)
	if err != nil {
		t.Skipf("database is not configured: %v", err)
	}
	defer base.Close()

	repo, err := New(base)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, userID := range []uint64{1, 40} {
		var legacy []byte
		if err := base.Conn().QueryRowContext(ctx, "SELECT GetUserTariff(?)", userID).Scan(&legacy); err != nil {
			t.Fatalf("user %d: legacy GetUserTariff: %v", userID, err)
		}
		got, err := repo.Internal.GetUserTariff(ctx, userID)
		if err != nil {
			t.Fatalf("user %d: GetUserTariff: %v", userID, err)
		}

		var legacyObj, gotObj any
		if err := json.Unmarshal(legacy, &legacyObj); err != nil {
			t.Fatalf("user %d: unmarshal legacy: %v", userID, err)
		}
		if err := json.Unmarshal(got, &gotObj); err != nil {
			t.Fatalf("user %d: unmarshal v2: %v", userID, err)
		}
		if !reflect.DeepEqual(legacyObj, gotObj) {
			t.Fatalf("user %d tariff differs:\nlegacy=%s\nv2=%s", userID, legacy, got)
		}
	}
}
