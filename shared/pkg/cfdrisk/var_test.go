package cfdrisk

import (
	"math"
	"testing"

	"bitso-trading-platform/shared/pkg/varmodel"
)

// Two books with perfectly correlated returns add up (the Bitso shortcut),
// perfectly anti-correlated ones net, and the general case is
// z x sqrt(e1^2 v1^2 + e2^2 v2^2 + 2 rho e1 e2 v1 v2) by hand.
func TestTwoAssetParametricVaR(t *testing.T) {
	start := ymd("2023-01-03")
	a := alternating(600, 0.01)
	b := make([]float64, len(a))
	for i := range b {
		b[i] = 2 * a[i] // same sign, twice the vol
	}
	neg := make([]float64, len(a))
	for i := range neg {
		neg[i] = -a[i]
	}
	// Period-4 pattern against the period-2 one: EWMA correlation ~0.
	ind := make([]float64, len(a))
	for i := range ind {
		ind[i] = 0.01
		if i%4 >= 2 {
			ind[i] = -0.01
		}
	}
	z := DefaultParams().Z()
	cases := []struct {
		name   string
		rb     []float64
		ea, eb float64
		rho    float64
	}{
		{"rho +1", b, 1000, 500, 1},
		{"rho -1", neg, 1000, 400, -1},
		{"rho ~0", ind, 1000, 1000, 0},
	}
	for _, c := range cases {
		p := VaR([]Book{
			{Name: "a", Closes: tradingCloses(start, a), Units: c.ea / 100},
			{Name: "b", Closes: tradingCloses(start, c.rb), Units: c.eb / 100},
		})
		if len(p.Books) != 2 || p.Books[0].Err != nil || p.Books[1].Err != nil || !p.CorrelationOK {
			t.Fatalf("%s: %+v", c.name, p)
		}
		rho := p.Correlation[0][1]
		if !near(rho, c.rho, 0.01) || !near(p.LongCorrelation[0][1], c.rho, 0.01) {
			t.Fatalf("%s: correlation %.4f / %.4f, want %.0f", c.name, rho, p.LongCorrelation[0][1], c.rho)
		}
		ba, bb := p.Books[0], p.Books[1]
		if !near(ba.VaR, z*ba.Vol*math.Abs(ba.Exposure), 1e-9) {
			t.Fatalf("%s: book VaR %v", c.name, ba.VaR)
		}
		hand := z * math.Sqrt(ba.Exposure*ba.Exposure*ba.Vol*ba.Vol+bb.Exposure*bb.Exposure*bb.Vol*bb.Vol+2*rho*ba.Exposure*bb.Exposure*ba.Vol*bb.Vol)
		if !near(p.VaR, hand, 1e-9) {
			t.Fatalf("%s: portfolio VaR %v, by hand %v", c.name, p.VaR, hand)
		}
		switch c.rho {
		case 1:
			if !near(p.VaR, ba.VaR+bb.VaR, 1e-9) || !near(p.VaR, p.VaRUndiversified, 1e-9) {
				t.Fatalf("rho 1: %v vs %v + %v", p.VaR, ba.VaR, bb.VaR)
			}
		case -1:
			if !near(p.VaR, math.Abs(ba.VaR-bb.VaR), 1e-9) {
				t.Fatalf("rho -1: %v vs |%v - %v|", p.VaR, ba.VaR, bb.VaR)
			}
		}
		if p.ESNormal <= p.VaR || !near(p.ESNormal/p.VaR, varmodel.ZES/z, 1e-9) {
			t.Fatalf("%s: normal ES %v vs VaR %v", c.name, p.ESNormal, p.VaR)
		}
		if p.PairCorrelation("a", "b") != rho || p.PairCorrelation("a", "zz") != 1 {
			t.Fatalf("%s: pair correlation", c.name)
		}
	}
}

// Historical VaR and ES against the sorted scenarios: the last 252 returns
// are -1..-252 bps in a shuffled order, so the k-th worst are known.
func TestHistoricalVaRAndESAgainstSortedScenarios(t *testing.T) {
	rs := alternating(100, 0.001)
	tail := make([]float64, 252)
	for i := range tail {
		tail[(i*97)%252] = -float64(i+1) * 1e-4
	}
	rs = append(rs, tail...)
	const e = 10000.0
	p := VaR([]Book{{Name: "a", Closes: tradingCloses(ymd("2023-01-03"), rs), Units: e / 100}})
	a := p.Books[0]
	if a.Err != nil || a.Scenarios != 252 || !a.HistOK {
		t.Fatalf("%+v", a)
	}
	// Exposure = units x last close; losses scale with it.
	k := int(math.Ceil(252 * 0.01)) // 3: the 3rd worst is -250 bps
	wantVaR := -a.Exposure * math.Expm1(-250e-4)
	if !near(a.VaRHistorical, wantVaR, 1e-9) {
		t.Fatalf("hist VaR %v, want %v (k=%d)", a.VaRHistorical, wantVaR, k)
	}
	kes := int(math.Ceil(252 * 0.025)) // 7: mean of -252..-246 bps
	sum := 0.0
	for i := 252; i > 252-kes; i-- {
		sum += -a.Exposure * math.Expm1(-float64(i)*1e-4)
	}
	if !near(a.ESHistorical, sum/float64(kes), 1e-9) {
		t.Fatalf("hist ES %v, want %v", a.ESHistorical, sum/float64(kes))
	}
	// One book: the portfolio figures are the book's.
	if !near(p.VaRHistorical, a.VaRHistorical, 1e-9) || !near(p.ESHistorical, a.ESHistorical, 1e-9) || p.Scenarios != 252 || !near(p.VaR, a.VaR, 1e-9) {
		t.Fatalf("portfolio %+v", p)
	}
	if !near(p.WorstLoss, -a.Exposure*math.Expm1(-252e-4), 1e-9) || p.WorstDate.IsZero() {
		t.Fatalf("worst day %v %v", p.WorstDate, p.WorstLoss)
	}

	// Two identical books: aligned scenarios double, the quantiles double.
	p2 := VaR([]Book{
		{Name: "a", Closes: tradingCloses(ymd("2023-01-03"), rs), Units: e / 100},
		{Name: "b", Closes: tradingCloses(ymd("2023-01-03"), rs), Units: e / 100},
	})
	if !near(p2.VaRHistorical, 2*a.VaRHistorical, 1e-6) || !near(p2.ESHistorical, 2*a.ESHistorical, 1e-6) || p2.AlignedReturns != 352 {
		t.Fatalf("two books: %v %v aligned %d", p2.VaRHistorical, p2.ESHistorical, p2.AlignedReturns)
	}
}

// A book with too little history is reported with Err and left out; a
// flat book contributes no exposure; with no positions the portfolio is
// zero.
func TestVaRIncompleteBooks(t *testing.T) {
	p := VaR([]Book{
		{Name: "short", Closes: tradingCloses(ymd("2026-01-05"), alternating(30, 0.01)), Units: 1},
		{Name: "flat", Closes: tradingCloses(ymd("2023-01-03"), alternating(600, 0.01)), Units: 0},
		{Name: "long", Closes: tradingCloses(ymd("2023-01-03"), alternating(600, 0.02)), Units: 10},
	})
	if p.Books[0].Err == nil || p.Books[1].Err != nil || p.Books[1].VaR != 0 || p.Books[2].Err != nil {
		t.Fatalf("%+v", p.Books)
	}
	if !near(p.VaR, p.Books[2].VaR, 1e-9) || p.Gross != p.Books[2].Exposure+p.Books[0].Exposure || p.Books[0].Exposure == 0 {
		t.Fatalf("portfolio %v vs %v, gross %v", p.VaR, p.Books[2].VaR, p.Gross)
	}
	if none := VaR(nil); none.VaR != 0 || none.HistOK || !none.CorrelationOK {
		t.Fatalf("empty %+v", none)
	}
}
