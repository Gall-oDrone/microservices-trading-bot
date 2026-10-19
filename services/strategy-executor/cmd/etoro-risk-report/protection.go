package main

import (
	"fmt"
	"math"
	"sort"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/cfdrisk"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/stops"
)

// protectionSection (M15) reads the ledger only: the held positions' stops
// against the mark and the ATR, the stop edits, the venue closes. marks are
// the last closes on or before the as-of date per book; atrs the ATR(14)
// of the saved bars per book (missing: no ATR columns).
func protectionSection(recs []etoroledger.Record, marks map[string]cfdrisk.Mark, atrs map[string]float64) ProtectionSection {
	sec := ProtectionSection{Positions: []ProtectedPosition{}, Edits: []ProtectionEdit{}, VenueCloses: []VenueCloseRow{}}
	source := func(r etoroledger.Record) string {
		if r.IsUI() {
			return "manual"
		}
		return "rule"
	}
	// Who opened each position (for the source of a held position).
	openedBy := map[string]string{}
	for _, r := range recs {
		if d := r.Demo; d != nil && d.Action == etoroledger.ActionOpen && d.PositionAfter.PositionID != "" {
			openedBy[d.PositionAfter.PositionID] = source(r)
		}
	}

	// Held positions, per book, from the newest demo record.
	books := map[string]bool{}
	for _, r := range recs {
		if r.Demo != nil {
			books[r.Book] = true
		}
	}
	names := make([]string, 0, len(books))
	for b := range books {
		names = append(names, b)
	}
	sort.Strings(names)
	for _, b := range names {
		p := etoroledger.LastDemoPosition(recs, b)
		if p.State != "long" {
			continue
		}
		pp := ProtectedPosition{Book: b, PositionID: p.PositionID, Source: openedBy[p.PositionID], OpenRate: p.OpenRate, Units: p.Units,
			StopLossRate: p.StopLossRate, TakeProfitRate: p.TakeProfitRate, StopType: p.StopType, Protected: p.Protected(), Mark: marks[b].Close, ATR: atrs[b]}
		if pp.Source == "" {
			pp.Source = "unknown"
		}
		if pp.Protected {
			if p.OpenRate > 0 {
				pp.StopPctOfOpen = (p.OpenRate - p.StopLossRate) / p.OpenRate
				pp.MaxLossUSD = p.Units * (p.OpenRate - p.StopLossRate)
			}
			if pp.Mark > 0 {
				pp.StopPctOfMark = (pp.Mark - p.StopLossRate) / pp.Mark
			}
			if pp.ATR > 0 && pp.Mark > 0 {
				pp.StopATRs = (pp.Mark - p.StopLossRate) / pp.ATR
			}
			sec.Summary.HeldProtected++
		}
		sec.Summary.Held++
		sec.Positions = append(sec.Positions, pp)
	}

	// Edits and venue closes, in ledger order (bar date, then recorded time).
	sorted := append([]etoroledger.Record(nil), recs...)
	sort.SliceStable(sorted, func(i, j int) bool { return etoroledger.Newer(sorted[j], sorted[i]) })
	for _, r := range sorted {
		d := r.Demo
		if d == nil {
			continue
		}
		switch d.Action {
		case etoroledger.ActionProtect:
			e := ProtectionEdit{FillDate: r.Decision.FillDate, Book: r.Book, PositionID: d.PositionAfter.PositionID, Source: source(r),
				StopBefore: d.PositionBefore.StopLossRate, StopAfter: d.PositionAfter.StopLossRate,
				TPBefore: d.PositionBefore.TakeProfitRate, TPAfter: d.PositionAfter.TakeProfitRate, Status: "recorded"}
			if o := d.Order; o != nil {
				e.Status, e.OperationID = o.Status, o.OrderID
			}
			if p := d.Protection; p != nil {
				e.Profile, e.ATR, e.MaxLossUSD, e.Reason = p.Profile, p.ATR, p.MaxLossUSD, p.Reason
				if e.StopBefore == 0 {
					e.StopBefore = p.StopLossBefore
				}
				if e.OperationID == "" {
					e.OperationID = p.OperationID
				}
			}
			sec.Edits = append(sec.Edits, e)
			sec.Summary.Edits++
			if !r.IsUI() {
				sec.Summary.Ratchets++
			}
		case etoroledger.ActionVenueClose:
			pb := d.PositionBefore
			row := VenueCloseRow{FillDate: r.Decision.FillDate, Book: r.Book, PositionID: pb.PositionID, Kind: etoroledger.VenueCloseOther,
				OpenRate: pb.OpenRate, Units: pb.Units, StopLossRate: pb.StopLossRate, TakeProfitRate: pb.TakeProfitRate}
			if o := d.Order; o != nil {
				row.CloseRate, row.Fees = o.AvgPrice, o.Fees
				if row.Units == 0 {
					row.Units = o.Units
				}
			}
			if vc := d.VenueClose; vc != nil {
				row.Kind, row.CloseRate, row.GapBps, row.ClosedAt, row.NetProfit, row.Fees = vc.Kind, vc.CloseRate, vc.GapBps, vc.ClosedAt, vc.NetProfit, vc.Fees
				if vc.StopLossRate > 0 {
					row.StopLossRate = vc.StopLossRate
				}
				if vc.TakeProfitRate > 0 {
					row.TakeProfitRate = vc.TakeProfitRate
				}
			}
			row.Realized = row.NetProfit - row.Fees
			switch row.Kind {
			case etoroledger.VenueCloseStoppedOut:
				sec.Summary.StopOuts++
				if row.StopLossRate > 0 && row.OpenRate > 0 {
					row.ModelledAtStop = -row.Units * (row.OpenRate - row.StopLossRate)
					row.VsModelled = row.Realized - row.ModelledAtStop
					sec.Summary.ModelledUSD += row.ModelledAtStop
				}
				sec.Summary.WorstGapBps = math.Max(sec.Summary.WorstGapBps, row.GapBps)
			case etoroledger.VenueCloseTookProfit:
				sec.Summary.TakeProfits++
			default:
				sec.Summary.OtherCloses++
			}
			sec.Summary.RealizedUSD += row.Realized
			sec.VenueCloses = append(sec.VenueCloses, row)
		}
	}
	return sec
}

// stopTypeLabel names a stop type for the markdown ("" = fixed).
func stopTypeLabel(t string) string {
	if t == "" {
		return broker.StopFixed
	}
	return t
}

// noStop is true for eToro's placeholder and zero.
func noStop(rate float64) bool { return !stops.HasStop(rate) }

// rateOrNone prints a rate, or "none" for 0 / the venue's placeholder.
func rateOrNone(rate float64) string {
	if noStop(rate) {
		return "none"
	}
	return fmt.Sprintf("%.2f", rate)
}

// optF prints a positive figure or "".
func optF(x float64) string {
	if x == 0 {
		return ""
	}
	return fmt.Sprintf("%.2f", x)
}
