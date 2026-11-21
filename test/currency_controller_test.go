package test

import (
	"fmt"
	"net/http"
	"testing"
)

func addTransactionPath(userID int) string {
	return fmt.Sprintf("/currency/user/%d/transactions/add", userID)
}

func TestAddUserCurrencyTransaction_Access(t *testing.T) {
	tests := []struct {
		name   string
		caller func(f *fixture) int
		want   int
	}{
		{"unauthenticated", func(f *fixture) int { return 0 }, http.StatusUnauthorized},
		{"user role", func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"admin", func(f *fixture) int { return f.admin }, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			body := map[string]any{"type": "income", "amount": 5}
			expectStatus(t, f.do(t, "POST", addTransactionPath(f.member), tt.caller(f), body), tt.want)
		})
	}
}

func TestAddUserCurrencyTransaction_StoresKey(t *testing.T) {
	tests := []struct {
		name    string
		body    map[string]any
		wantKey string
		wantSum int
	}{
		{"chosen income type", map[string]any{"type": "income", "amount": 5, "income_type_key": "currency_income_100_game_posts"}, "currency_income_100_game_posts", 5},
		{"chosen spend type", map[string]any{"type": "spend", "amount": 3, "income_type_key": "currency_spend_auto_archiving_immunity"}, "currency_spend_auto_archiving_immunity", -3},
		{"no income type", map[string]any{"type": "income", "amount": 5, "income_type_key": nil}, "currency_income_manual_transaction", 5},
		{"custom spend with note", map[string]any{"type": "spend", "amount": 2, "metadata": map[string]any{"note": "shop"}}, "currency_spend_manual_transaction", -2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)

			expectStatus(t, f.do(t, "POST", addTransactionPath(f.member), f.admin, tt.body), http.StatusOK)

			if n := f.count(t, "SELECT COUNT(*) FROM currency_user_transactions WHERE user_id = ? AND income_type_key = ?", f.member, tt.wantKey); n != 1 {
				t.Errorf("expected 1 transaction with key %s, got %d", tt.wantKey, n)
			}
			if n := f.count(t, "SELECT amount FROM currency_user_account WHERE user_id = ?", f.member); n != tt.wantSum {
				t.Errorf("expected account amount %d, got %d", tt.wantSum, n)
			}
		})
	}
}

func TestAddUserCurrencyTransaction_StoresNote(t *testing.T) {
	f := newFixture(t)
	body := map[string]any{"type": "income", "amount": 5, "metadata": map[string]any{"note": "event prize"}}

	expectStatus(t, f.do(t, "POST", addTransactionPath(f.member), f.admin, body), http.StatusOK)

	if n := f.count(t, "SELECT COUNT(*) FROM currency_user_transactions WHERE user_id = ? AND JSON_UNQUOTE(JSON_EXTRACT(metadata, '$.note')) = 'event prize'", f.member); n != 1 {
		t.Errorf("expected the note to be stored in metadata, got %d matching rows", n)
	}
}

func TestAddUserCurrencyTransaction_InvalidInput(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
	}{
		{"unknown key", map[string]any{"type": "income", "amount": 5, "income_type_key": "currency_income_nonexistent"}},
		{"spend key on income", map[string]any{"type": "income", "amount": 5, "income_type_key": "currency_spend_auto_archiving_immunity"}},
		{"income key on spend", map[string]any{"type": "spend", "amount": 5, "income_type_key": "currency_income_game_post"}},
		{"invalid numeric type", map[string]any{"type": 5, "amount": 5}},
		{"negative amount", map[string]any{"type": "income", "amount": -5}},
		{"malformed metadata", map[string]any{"type": "income", "amount": 5, "metadata": map[string]any{"note": 12}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)

			expectStatus(t, f.do(t, "POST", addTransactionPath(f.member), f.admin, tt.body), http.StatusBadRequest)

			if n := f.count(t, "SELECT COUNT(*) FROM currency_user_transactions"); n != 0 {
				t.Errorf("expected no transaction rows, got %d", n)
			}
			if n := f.count(t, "SELECT COUNT(*) FROM currency_user_account"); n != 0 {
				t.Errorf("expected no account rows, got %d", n)
			}
		})
	}
}
