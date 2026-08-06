package internal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	ledgerv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/ledger/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/domainerr"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type lineInput struct {
	accountType string
	ownerRef    string
	direction   string
	amountPence int64
	currency    string
}

func (s *Store) PostDoubleEntry(ctx context.Context, paymentID, description string, lines []lineInput) (*ledgerv1.JournalEntry, error) {
	if paymentID == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "payment_id is required")
	}
	if len(lines) < 2 {
		return nil, domainerr.New(domainerr.CodeValidation, "at least two journal lines are required")
	}

	var debit, credit int64
	currency := ""
	for i, l := range lines {
		if l.accountType == "" {
			return nil, domainerr.New(domainerr.CodeValidation, fmt.Sprintf("line %d: account_type is required", i))
		}
		if strings.TrimSpace(l.ownerRef) == "" {
			return nil, domainerr.New(domainerr.CodeValidation, fmt.Sprintf("line %d: owner_ref is required", i))
		}
		if l.direction != "DR" && l.direction != "CR" {
			return nil, domainerr.New(domainerr.CodeValidation, fmt.Sprintf("line %d: direction must be DR or CR", i))
		}
		if l.amountPence <= 0 {
			return nil, domainerr.New(domainerr.CodeValidation, fmt.Sprintf("line %d: amount must be positive", i))
		}
		cur := strings.ToUpper(strings.TrimSpace(l.currency))
		if len(cur) != 3 {
			return nil, domainerr.New(domainerr.CodeValidation, fmt.Sprintf("line %d: currency must be 3-letter ISO code", i))
		}
		if currency == "" {
			currency = cur
		} else if cur != currency {
			return nil, domainerr.New(domainerr.CodeValidation, "all lines must share the same currency")
		}
		lines[i].currency = cur
		switch l.direction {
		case "DR":
			debit += l.amountPence
		case "CR":
			credit += l.amountPence
		}
	}
	if debit != credit {
		return nil, domainerr.New(domainerr.CodeValidation, fmt.Sprintf("unbalanced entry: debit=%d credit=%d", debit, credit))
	}

	var entry *ledgerv1.JournalEntry
	err := withSerializable(ctx, s.pool, func(tx pgx.Tx) error {
		existing, err := s.loadEntryByPaymentID(ctx, tx, paymentID)
		if err == nil {
			entry = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		var entryID string
		var createdAt time.Time
		err = tx.QueryRow(ctx, `
			INSERT INTO journal_entry (payment_id, description)
			VALUES ($1, $2)
			RETURNING id::text, created_at
		`, paymentID, description).Scan(&entryID, &createdAt)
		if err != nil {
			if isUniqueViolation(err) {
				existing, loadErr := s.loadEntryByPaymentID(ctx, tx, paymentID)
				if loadErr != nil {
					return domainerr.Wrap(domainerr.CodeInternal, "reload after unique conflict", loadErr)
				}
				entry = existing
				return nil
			}
			return domainerr.Wrap(domainerr.CodeInternal, "insert journal_entry", err)
		}

		outLines := make([]*ledgerv1.JournalLine, 0, len(lines))
		for _, l := range lines {
			accountID, err := ensureAccount(ctx, tx, l.accountType, l.ownerRef, l.currency)
			if err != nil {
				return err
			}
			var lineID string
			err = tx.QueryRow(ctx, `
				INSERT INTO journal_line (journal_entry_id, account_id, direction, amount_pence)
				VALUES ($1::uuid, $2::uuid, $3, $4)
				RETURNING id::text
			`, entryID, accountID, l.direction, l.amountPence).Scan(&lineID)
			if err != nil {
				return domainerr.Wrap(domainerr.CodeInternal, "insert journal_line", err)
			}
			outLines = append(outLines, &ledgerv1.JournalLine{
				Id:          lineID,
				AccountType: accountTypeFromDB(l.accountType),
				OwnerRef:    l.ownerRef,
				Direction:   directionFromDB(l.direction),
				Amount: &commonv1.Money{
					AmountPence: l.amountPence,
					Currency:    l.currency,
				},
			})
		}

		entry = &ledgerv1.JournalEntry{
			Id:          entryID,
			PaymentId:   paymentID,
			Description: description,
			Lines:       outLines,
			CreatedAt:   timestamppb.New(createdAt),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *Store) ReverseEntry(ctx context.Context, paymentID, reason string) error {
	if paymentID == "" {
		return domainerr.New(domainerr.CodeValidation, "payment_id is required")
	}
	revPaymentID := paymentID + "-rev"

	return withSerializable(ctx, s.pool, func(tx pgx.Tx) error {
		var entryID string
		var reversed bool
		err := tx.QueryRow(ctx, `
			SELECT id::text, reversed
			FROM journal_entry
			WHERE payment_id = $1
			FOR UPDATE
		`, paymentID).Scan(&entryID, &reversed)
		if errors.Is(err, pgx.ErrNoRows) {
			return domainerr.New(domainerr.CodeNotFound, "journal entry not found")
		}
		if err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "load journal entry", err)
		}
		if reversed {
			return nil
		}

		// Already reversed via reverse payment_id (idempotent)
		var dummy string
		err = tx.QueryRow(ctx, `SELECT id::text FROM journal_entry WHERE payment_id = $1`, revPaymentID).Scan(&dummy)
		if err == nil {
			_, _ = tx.Exec(ctx, `UPDATE journal_entry SET reversed = TRUE WHERE id = $1::uuid`, entryID)
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domainerr.Wrap(domainerr.CodeInternal, "check reverse entry", err)
		}

		rows, err := tx.Query(ctx, `
			SELECT a.type, a.owner_ref, a.currency, jl.direction, jl.amount_pence
			FROM journal_line jl
			JOIN account a ON a.id = jl.account_id
			WHERE jl.journal_entry_id = $1::uuid
			ORDER BY jl.created_at, jl.id
		`, entryID)
		if err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "load journal lines", err)
		}
		defer rows.Close()

		type origLine struct {
			accountType string
			ownerRef    string
			currency    string
			direction   string
			amountPence int64
		}
		var orig []origLine
		for rows.Next() {
			var l origLine
			if err := rows.Scan(&l.accountType, &l.ownerRef, &l.currency, &l.direction, &l.amountPence); err != nil {
				return domainerr.Wrap(domainerr.CodeInternal, "scan journal line", err)
			}
			orig = append(orig, l)
		}
		if err := rows.Err(); err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "iterate journal lines", err)
		}
		if len(orig) == 0 {
			return domainerr.New(domainerr.CodeInternal, "journal entry has no lines")
		}

		desc := "REVERSAL"
		if reason != "" {
			desc = "REVERSAL: " + reason
		}
		var revEntryID string
		err = tx.QueryRow(ctx, `
			INSERT INTO journal_entry (payment_id, description)
			VALUES ($1, $2)
			RETURNING id::text
		`, revPaymentID, desc).Scan(&revEntryID)
		if err != nil {
			if isUniqueViolation(err) {
				_, uerr := tx.Exec(ctx, `UPDATE journal_entry SET reversed = TRUE WHERE id = $1::uuid`, entryID)
				return uerr
			}
			return domainerr.Wrap(domainerr.CodeInternal, "insert reverse journal_entry", err)
		}

		for _, l := range orig {
			opp := "CR"
			if l.direction == "CR" {
				opp = "DR"
			}
			accountID, err := ensureAccount(ctx, tx, l.accountType, l.ownerRef, l.currency)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO journal_line (journal_entry_id, account_id, direction, amount_pence)
				VALUES ($1::uuid, $2::uuid, $3, $4)
			`, revEntryID, accountID, opp, l.amountPence)
			if err != nil {
				return domainerr.Wrap(domainerr.CodeInternal, "insert reverse journal_line", err)
			}
		}

		_, err = tx.Exec(ctx, `UPDATE journal_entry SET reversed = TRUE WHERE id = $1::uuid`, entryID)
		if err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "mark entry reversed", err)
		}
		return nil
	})
}

func (s *Store) GetBalance(ctx context.Context, accountType, ownerRef, currency string) (*ledgerv1.BalanceResponse, error) {
	if accountType == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "account_type is required")
	}
	if strings.TrimSpace(ownerRef) == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "owner_ref is required")
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "GBP"
	}
	if len(currency) != 3 {
		return nil, domainerr.New(domainerr.CodeValidation, "currency must be 3-letter ISO code")
	}

	var balance int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(
			CASE WHEN jl.direction = 'DR' THEN -jl.amount_pence ELSE jl.amount_pence END
		), 0)
		FROM account a
		LEFT JOIN journal_line jl ON jl.account_id = a.id
		WHERE a.type = $1 AND a.owner_ref = $2 AND a.currency = $3
	`, accountType, ownerRef, currency).Scan(&balance)
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "get balance", err)
	}

	return &ledgerv1.BalanceResponse{
		Balance: &commonv1.Money{
			AmountPence: balance,
			Currency:    currency,
		},
		AsOf: timestamppb.Now(),
	}, nil
}

func (s *Store) GetJournalEntry(ctx context.Context, paymentID string) (*ledgerv1.JournalEntry, error) {
	if paymentID == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "payment_id is required")
	}
	entry, err := s.loadEntryByPaymentID(ctx, s.pool, paymentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.New(domainerr.CodeNotFound, "journal entry not found")
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (s *Store) loadEntryByPaymentID(ctx context.Context, q querier, paymentID string) (*ledgerv1.JournalEntry, error) {
	var (
		entryID     string
		desc        string
		createdAt   time.Time
	)
	err := q.QueryRow(ctx, `
		SELECT id::text, description, created_at
		FROM journal_entry
		WHERE payment_id = $1
	`, paymentID).Scan(&entryID, &desc, &createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, domainerr.Wrap(domainerr.CodeInternal, "load journal entry", err)
	}

	rows, err := q.Query(ctx, `
		SELECT jl.id::text, a.type, a.owner_ref, a.currency, jl.direction, jl.amount_pence
		FROM journal_line jl
		JOIN account a ON a.id = jl.account_id
		WHERE jl.journal_entry_id = $1::uuid
		ORDER BY jl.created_at, jl.id
	`, entryID)
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "load journal lines", err)
	}
	defer rows.Close()

	var lines []*ledgerv1.JournalLine
	for rows.Next() {
		var (
			lineID, accType, ownerRef, currency, direction string
			amount                                         int64
		)
		if err := rows.Scan(&lineID, &accType, &ownerRef, &currency, &direction, &amount); err != nil {
			return nil, domainerr.Wrap(domainerr.CodeInternal, "scan journal line", err)
		}
		lines = append(lines, &ledgerv1.JournalLine{
			Id:          lineID,
			AccountType: accountTypeFromDB(accType),
			OwnerRef:    ownerRef,
			Direction:   directionFromDB(direction),
			Amount: &commonv1.Money{
				AmountPence: amount,
				Currency:    currency,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "iterate journal lines", err)
	}

	return &ledgerv1.JournalEntry{
		Id:          entryID,
		PaymentId:   paymentID,
		Description: desc,
		Lines:       lines,
		CreatedAt:   timestamppb.New(createdAt),
	}, nil
}

func ensureAccount(ctx context.Context, tx pgx.Tx, accountType, ownerRef, currency string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO account (type, owner_ref, currency)
		VALUES ($1, $2, $3)
		ON CONFLICT (type, owner_ref, currency) DO UPDATE
		SET type = EXCLUDED.type
		RETURNING id::text
	`, accountType, ownerRef, currency).Scan(&id)
	if err != nil {
		return "", domainerr.Wrap(domainerr.CodeInternal, "ensure account", err)
	}
	return id, nil
}

func withSerializable(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	const maxAttempts = 5
	var last error
	for range maxAttempts {
		err := pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.Serializable}, fn)
		if err == nil {
			return nil
		}
		last = err
		if !isSerializationFailure(err) {
			return err
		}
	}
	return domainerr.Wrap(domainerr.CodeConflict, "serializable transaction retries exhausted", last)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	return false
}

func accountTypeToDB(t ledgerv1.AccountType) (string, error) {
	switch t {
	case ledgerv1.AccountType_CONSUMER_ESCROW:
		return "CONSUMER_ESCROW", nil
	case ledgerv1.AccountType_MERCHANT_ESCROW:
		return "MERCHANT_ESCROW", nil
	case ledgerv1.AccountType_PLATFORM_FEE:
		return "PLATFORM_FEE", nil
	default:
		return "", domainerr.New(domainerr.CodeValidation, "invalid account_type")
	}
}

func accountTypeFromDB(s string) ledgerv1.AccountType {
	switch s {
	case "CONSUMER_ESCROW":
		return ledgerv1.AccountType_CONSUMER_ESCROW
	case "MERCHANT_ESCROW":
		return ledgerv1.AccountType_MERCHANT_ESCROW
	case "PLATFORM_FEE":
		return ledgerv1.AccountType_PLATFORM_FEE
	default:
		return ledgerv1.AccountType_ACCOUNT_TYPE_UNSPECIFIED
	}
}

func directionToDB(d ledgerv1.Direction) (string, error) {
	switch d {
	case ledgerv1.Direction_DR:
		return "DR", nil
	case ledgerv1.Direction_CR:
		return "CR", nil
	default:
		return "", domainerr.New(domainerr.CodeValidation, "invalid direction")
	}
}

func directionFromDB(s string) ledgerv1.Direction {
	switch s {
	case "DR":
		return ledgerv1.Direction_DR
	case "CR":
		return ledgerv1.Direction_CR
	default:
		return ledgerv1.Direction_DIRECTION_UNSPECIFIED
	}
}
