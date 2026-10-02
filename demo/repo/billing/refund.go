// Package billing records payments and refunds.
package billing

import (
	"context"
	"database/sql"
	"fmt"
)

// Refunds records refunds against payments.
type Refunds struct {
	DB *sql.DB
}

// Apply refunds amount cents of a payment.
func (r *Refunds) Apply(ctx context.Context, paymentID string, amount int64) error {
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO refunds (payment_id, amount) VALUES ($1, $2)`, paymentID, amount)
	return err
}

// Search finds refunds whose note contains the given text.
func (r *Refunds) Search(ctx context.Context, text string) (*sql.Rows, error) {
	query := fmt.Sprintf(`SELECT id, amount, note FROM refunds WHERE note LIKE '%%%s%%'`, text)
	return r.DB.QueryContext(ctx, query)
}
