package internal

import (
	"context"
	"log/slog"
	"time"

	bankv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/bank/v1"
)

// ReconciliationWorker periodically checks for in-flight payments that have stalled due to crashes
// or network partitions and either completes them (if settled at the bank) or executes compensation rollbacks.
type ReconciliationWorker struct {
	repo         *Repo
	orchestrator *Orchestrator
	bank         bankv1.BankAdapterClient
	interval     time.Duration
	staleAfter   time.Duration
}

func NewReconciliationWorker(repo *Repo, orch *Orchestrator, bank bankv1.BankAdapterClient, interval, staleAfter time.Duration) *ReconciliationWorker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if staleAfter <= 0 {
		staleAfter = 1 * time.Minute
	}
	return &ReconciliationWorker{
		repo:         repo,
		orchestrator: orch,
		bank:         bank,
		interval:     interval,
		staleAfter:   staleAfter,
	}
}

// Start runs the periodic reconciliation loop until ctx is cancelled.
func (w *ReconciliationWorker) Start(ctx context.Context) {
	slog.Info("reconciliation worker started", "interval", w.interval.String(), "stale_after", w.staleAfter.String())
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("reconciliation worker stopping")
			return
		case <-ticker.C:
			w.reconcileStalePayments(ctx)
		}
	}
}

func (w *ReconciliationWorker) reconcileStalePayments(ctx context.Context) {
	stale, err := w.repo.ListStaleInFlightPayments(ctx, w.staleAfter)
	if err != nil {
		slog.ErrorContext(ctx, "reconciliation list stale payments failed", "err", err)
		return
	}

	for _, p := range stale {
		slog.WarnContext(ctx, "reconciling stale in-flight payment",
			"payment_id", p.ID,
			"status", p.Status,
			"bank_ref", p.BankPaymentRef,
		)

		// 1. If payment reached the bank, query bank settlement status
		if p.BankPaymentRef != "" {
			resp, err := w.bank.GetPaymentStatus(ctx, &bankv1.StatusRequest{
				BankPaymentRef: p.BankPaymentRef,
			})
			if err != nil {
				slog.ErrorContext(ctx, "reconciliation query bank status failed", "payment_id", p.ID, "err", err)
				continue
			}

			if resp.GetStatus() == bankv1.BankPaymentStatus_SETTLED {
				slog.InfoContext(ctx, "reconciliation: bank payment confirmed SETTLED, resuming saga", "payment_id", p.ID)
				if err := w.orchestrator.ResumeFromLedger(ctx, p); err != nil {
					slog.ErrorContext(ctx, "reconciliation resume failed", "payment_id", p.ID, "err", err)
				}
				continue
			}
		}

		// 2. Otherwise (bank rejected, or payment stalled before bank), trigger compensation rollbacks
		slog.InfoContext(ctx, "reconciliation: compensating unconfirmed stale payment", "payment_id", p.ID)
		if err := w.orchestrator.CompensateStale(ctx, p, "RECONCILIATION_TIMEOUT"); err != nil {
			slog.ErrorContext(ctx, "reconciliation compensate failed", "payment_id", p.ID, "err", err)
		}
	}
}
