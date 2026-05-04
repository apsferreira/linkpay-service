package domain

import (
	"testing"
)

// Valida as regras de holdback e reserva anti-chargeback (domain constants).

func TestHoldbackConstants(t *testing.T) {
	if HoldbackPIXDays != 2 {
		t.Errorf("HoldbackPIXDays = %d, want 2", HoldbackPIXDays)
	}
	if HoldbackBoletoDays != 2 {
		t.Errorf("HoldbackBoletoDays = %d, want 2", HoldbackBoletoDays)
	}
	if HoldbackCardDays != 15 {
		t.Errorf("HoldbackCardDays = %d, want 15", HoldbackCardDays)
	}
	if HoldbackReservePct != 10 {
		t.Errorf("HoldbackReservePct = %d, want 10", HoldbackReservePct)
	}
}

func TestReservaAntiChargeback(t *testing.T) {
	tests := []struct {
		name          string
		grossCents    int
		wantReserve   int
		wantAvailable int
	}{
		{"R$100 bruto", 10000, 1000, 9000},
		{"R$10 bruto", 1000, 100, 900},
		{"R$0 bruto", 0, 0, 0},
		{"R$9,99 bruto", 999, 99, 900},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reserve := tc.grossCents * HoldbackReservePct / 100
			available := tc.grossCents - reserve
			if reserve != tc.wantReserve {
				t.Errorf("reserve = %d, want %d", reserve, tc.wantReserve)
			}
			if available != tc.wantAvailable {
				t.Errorf("available = %d, want %d", available, tc.wantAvailable)
			}
		})
	}
}

func TestAnticipationFee(t *testing.T) {
	// Antecipação: 2% sobre valor líquido elegível
	netCents := 10000 // R$100
	fee := netCents * AnticipationFeePercent / 100
	wantFee := 200 // R$2
	if fee != wantFee {
		t.Errorf("anticipation fee = %d, want %d", fee, wantFee)
	}

	anticipated := netCents - fee
	wantAnticipated := 9800
	if anticipated != wantAnticipated {
		t.Errorf("após antecipação = %d, want %d", anticipated, wantAnticipated)
	}
}
