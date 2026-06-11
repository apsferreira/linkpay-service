package domain

import (
	"testing"
)

func TestCreatorEarningsCents(t *testing.T) {
	tests := []struct {
		name        string
		commission  int
		priceCents  int
		wantEarning int
		wantFee     int
	}{
		{
			name:        "comissão padrão 30%",
			commission:  0, // usa DefaultPlatformCommissionPct
			priceCents:  10000,
			wantEarning: 7000,
			wantFee:     3000,
		},
		{
			name:        "comissão explícita 10%",
			commission:  10,
			priceCents:  10000,
			wantEarning: 9000,
			wantFee:     1000,
		},
		{
			name:        "comissão 30%",
			commission:  30,
			priceCents:  10000,
			wantEarning: 7000,
			wantFee:     3000,
		},
		{
			name:        "preço R$1,00 comissão 10%",
			commission:  10,
			priceCents:  100,
			wantEarning: 90,
			wantFee:     10,
		},
		{
			name:        "comissão negativa usa padrão 30%",
			commission:  -5,
			priceCents:  10000,
			wantEarning: 7000,
			wantFee:     3000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &DigitalProduct{
				PlatformCommissionPct: tc.commission,
			}
			gotEarning := p.CreatorEarningsCents(tc.priceCents)
			gotFee := p.PlatformFeeCents(tc.priceCents)
			if gotEarning != tc.wantEarning {
				t.Errorf("CreatorEarningsCents = %d, want %d", gotEarning, tc.wantEarning)
			}
			if gotFee != tc.wantFee {
				t.Errorf("PlatformFeeCents = %d, want %d", gotFee, tc.wantFee)
			}
			if gotEarning+gotFee != tc.priceCents {
				t.Errorf("earning(%d) + fee(%d) != price(%d): distribuição inconsistente",
					gotEarning, gotFee, tc.priceCents)
			}
		})
	}
}
