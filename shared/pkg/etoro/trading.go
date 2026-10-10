package etoro

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Transaction is the side of an open order.
type Transaction string

const (
	TxBuy       Transaction = "buy"
	TxSellShort Transaction = "sellShort"
)

// OrderType is the execution type of an open order.
type OrderType string

const (
	OrderMarket   OrderType = "mkt"
	OrderMIT      OrderType = "mit"      // market-if-touched, needs TriggerRate
	OrderLimitIOC OrderType = "limitIOC" // needs LimitRate within 10% of market
)

// Settlement types (eligibility leverageConfigs[].settlementType).
const (
	SettlementCFD  = "cfd"
	SettlementReal = "real"
)

// OrderRequest opens a position through POST /api/v2/trading/execution[/demo]/orders.
// Exactly one of Amount / Units sizes the order. A stop-loss is required by
// eToro for leverage > 1 and for sellShort.
type OrderRequest struct {
	Action         string      `json:"action"` // always "open" (close is not supported on v2 yet)
	Transaction    Transaction `json:"transaction"`
	InstrumentID   int64       `json:"instrumentId"`
	OrderType      OrderType   `json:"orderType"`
	Leverage       int         `json:"leverage"`
	Amount         *float64    `json:"amount,omitempty"`
	Units          *float64    `json:"units,omitempty"`
	OrderCurrency  string      `json:"orderCurrency,omitempty"`
	SettlementType string      `json:"settlementType,omitempty"`
	StopLossRate   *float64    `json:"stopLossRate,omitempty"`
	TakeProfitRate *float64    `json:"takeProfitRate,omitempty"`
	TriggerRate    *float64    `json:"triggerRate,omitempty"`
	LimitRate      *float64    `json:"limitRate,omitempty"`
}

// MarketBuyByAmount is a market buy of amount (account currency, USD) at
// the given leverage with no SL/TP.
func MarketBuyByAmount(instrumentID int64, amount float64, leverage int) OrderRequest {
	return OrderRequest{
		Action: "open", Transaction: TxBuy, InstrumentID: instrumentID, OrderType: OrderMarket,
		Leverage: leverage, Amount: &amount, OrderCurrency: "usd",
	}
}

// Validate applies eToro's documented request rules locally so a bad order
// fails before it spends write budget.
func (r OrderRequest) Validate() error {
	if r.Action != "open" {
		return fmt.Errorf("order action must be \"open\" (got %q): close positions with ClosePosition", r.Action)
	}
	if r.InstrumentID <= 0 {
		return fmt.Errorf("instrumentId is required")
	}
	if r.Transaction != TxBuy && r.Transaction != TxSellShort {
		return fmt.Errorf("transaction must be buy or sellShort, got %q", r.Transaction)
	}
	if r.Leverage < 1 {
		return fmt.Errorf("leverage must be >= 1, got %d", r.Leverage)
	}
	sizes := 0
	for _, p := range []*float64{r.Amount, r.Units} {
		if p != nil {
			if !(*p > 0) || math.IsInf(*p, 0) {
				return fmt.Errorf("order size must be a positive number")
			}
			sizes++
		}
	}
	if sizes != 1 {
		return fmt.Errorf("exactly one of amount or units must be set")
	}
	switch r.OrderType {
	case OrderMarket, "":
	case OrderMIT:
		if r.TriggerRate == nil || *r.TriggerRate <= 0 {
			return fmt.Errorf("mit orders need triggerRate")
		}
	case OrderLimitIOC:
		if r.LimitRate == nil || *r.LimitRate <= 0 {
			return fmt.Errorf("limitIOC orders need limitRate")
		}
	default:
		return fmt.Errorf("unknown orderType %q", r.OrderType)
	}
	if (r.Leverage > 1 || r.Transaction == TxSellShort) && (r.StopLossRate == nil || *r.StopLossRate <= 0) {
		return fmt.Errorf("stopLossRate is required for leverage > 1 or sellShort")
	}
	return nil
}

// OrderAccepted is the response to an accepted open order. Acceptance is
// not execution: poll LookupOrder for the outcome.
type OrderAccepted struct {
	OrderID     int64  `json:"orderId"`
	ReferenceID string `json:"referenceId"` // echoes the x-request-id
	Token       string `json:"token"`
}

// OpenOrder submits an open order with requestID as x-request-id. Pass
// RequestIDFor(clientRef) to make the order resumable; an empty requestID
// gets a random one. The call is never retried here: on an error that may
// have reached eToro (TransportError, 5xx, 429) resolve it with
// LookupOrderByReference before trying again.
func (c *Client) OpenOrder(ctx context.Context, req OrderRequest, requestID string) (OrderAccepted, error) {
	if req.OrderType == "" {
		req.OrderType = OrderMarket
	}
	if err := req.Validate(); err != nil {
		return OrderAccepted{}, err
	}
	if requestID == "" {
		requestID = NewRequestID()
	}
	var resp OrderAccepted
	err := c.do(ctx, call{method: "POST", path: c.ordersPath(), body: req, requestID: requestID, write: true}, &resp)
	if err != nil {
		return OrderAccepted{}, err
	}
	if resp.ReferenceID == "" {
		resp.ReferenceID = requestID
	}
	return resp, nil
}

// OrderStatus is the status block of orders:lookup.
type OrderStatus struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	ErrorCode    int    `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
}

// OrderAsset is the asset block of orders:lookup.
type OrderAsset struct {
	Symbol         string `json:"symbol"`
	InstrumentID   int64  `json:"instrumentId"`
	Currency       string `json:"currency"`
	SettlementType string `json:"settlementType"`
	Leverage       int    `json:"leverage"`
	Side           string `json:"side"` // long | short
}

// OpeningData describes the fill that opened a position.
type OpeningData struct {
	OpenTime          string  `json:"openTime"`
	OrderID           int64   `json:"orderId"`
	ExecutionTime     string  `json:"executionTime"`
	Units             float64 `json:"units"`
	Contracts         float64 `json:"contracts"`
	AvgPrice          float64 `json:"avgPrice"`
	AvgConversionRate float64 `json:"avgConversionRate"`
	MarketSpread      float64 `json:"marketSpread"`
	Markup            float64 `json:"markup"`
	PriceID           int64   `json:"priceId"`
	Fees              float64 `json:"fees"`
	Taxes             float64 `json:"taxes"`
}

// PositionExecution is a position created (or changed) by an order.
type PositionExecution struct {
	PositionID                     int64        `json:"positionId"`
	State                          string       `json:"state"` // open | closed
	InvestedAmountCurrency         float64      `json:"investedAmountCurrency"`
	InitialExposureAccountCurrency float64      `json:"initialExposureAccountCurrency"`
	InitialExposureAssetCurrency   float64      `json:"initialExposureAssetCurrency"`
	MarginAccountCurrency          float64      `json:"marginAccountCurrency"`
	RemainingUnits                 float64      `json:"remainingUnits"`
	StopLossRate                   float64      `json:"stopLossRate"`
	TakeProfitRate                 float64      `json:"takeProfitRate"`
	OpeningData                    *OpeningData `json:"openingData"`
}

// OrderInfo is GET /api/v2/trading/info[/demo]/orders:lookup.
type OrderInfo struct {
	OrderID            int64               `json:"orderId"`
	Action             string              `json:"action"`
	Transaction        string              `json:"transaction"`
	Type               string              `json:"type"`
	Status             OrderStatus         `json:"status"`
	Asset              OrderAsset          `json:"asset"`
	OrderCurrency      string              `json:"orderCurrency"`
	RequestedAmount    float64             `json:"requestedAmount"`
	RequestedUnits     float64             `json:"requestedUnits"`
	FrozenAmount       float64             `json:"frozenAmount"`
	TotalCosts         float64             `json:"totalCosts"`
	PositionsToClose   []int64             `json:"positionsToClose"`
	PositionExecutions []PositionExecution `json:"positionExecutions"`
	RequestTime        string              `json:"requestTime"`
	LastUpdate         string              `json:"lastUpdate"`
	OpenActionType     string              `json:"openActionType"`
	RequestType        string              `json:"requestType"`
}

// Outcome is a coarse, broker-neutral reading of an order's state.
type Outcome string

const (
	OutcomePending   Outcome = "pending"
	OutcomeExecuted  Outcome = "executed"
	OutcomeRejected  Outcome = "rejected"
	OutcomeCancelled Outcome = "cancelled"
	OutcomeUnknown   Outcome = "unknown"
)

// Outcome classifies the order. eToro's status names are not enumerated
// in the docs, so this reads the name and error code conservatively: an
// executed position, or a status name saying so, is executed; an error code
// or a reject/fail name is rejected; cancel/expire is cancelled; anything
// else stays pending (never re-send a pending order).
func (o OrderInfo) Outcome() Outcome {
	name := strings.ToLower(o.Status.Name)
	switch {
	case o.Status.ErrorCode != 0 || strings.Contains(name, "reject") || strings.Contains(name, "fail") || strings.Contains(name, "error"):
		return OutcomeRejected
	case strings.Contains(name, "cancel") || strings.Contains(name, "expire"):
		return OutcomeCancelled
	case strings.Contains(name, "execut") || strings.Contains(name, "fill") || len(o.PositionExecutions) > 0:
		return OutcomeExecuted
	case name == "":
		return OutcomeUnknown
	default:
		return OutcomePending
	}
}

// PositionIDs lists the positions the order produced.
func (o OrderInfo) PositionIDs() []int64 {
	var ids []int64
	for _, p := range o.PositionExecutions {
		if p.PositionID > 0 {
			ids = append(ids, p.PositionID)
		}
	}
	return ids
}

// LookupOrder fetches an order by its numeric id.
func (c *Client) LookupOrder(ctx context.Context, orderID int64) (OrderInfo, error) {
	var info OrderInfo
	q := url.Values{"orderId": {strconv.FormatInt(orderID, 10)}}
	err := c.do(ctx, call{method: "GET", path: c.orderLookupPath(), query: q}, &info)
	return info, err
}

// LookupOrderByReference finds an order by reference id. CAUTION (demo API,
// 2026-10-10): for orders placed through this API it answers 404 "No
// external operation was found for referenceId" even though the order
// exists; the reference index only covers external operations. found=false
// is therefore NOT proof that an order is absent. Idempotency comes from
// eToro rejecting a reused x-request-id instead (IsDuplicateReference).
func (c *Client) LookupOrderByReference(ctx context.Context, referenceID string) (info OrderInfo, found bool, err error) {
	q := url.Values{"referenceId": {referenceID}}
	err = c.do(ctx, call{method: "GET", path: c.orderLookupPath(), query: q}, &info)
	if IsNotFound(err) {
		return OrderInfo{}, false, nil
	}
	if err != nil {
		return OrderInfo{}, false, err
	}
	return info, true, nil
}

// WaitOrder polls LookupOrder until the outcome is final or ctx ends,
// returning the last state seen.
func (c *Client) WaitOrder(ctx context.Context, orderID int64, every time.Duration) (OrderInfo, error) {
	if every <= 0 {
		every = 2 * time.Second
	}
	var last OrderInfo
	for {
		info, err := c.LookupOrder(ctx, orderID)
		if err != nil && !IsNotFound(err) {
			return last, err
		}
		if err == nil {
			last = info
			if oc := info.Outcome(); oc != OutcomePending && oc != OutcomeUnknown {
				return info, nil
			}
		}
		if serr := c.sleep(ctx, every); serr != nil {
			return last, serr
		}
	}
}

// CloseAccepted is the response of the v1 market-close route.
type CloseAccepted struct {
	OrderForClose struct {
		PositionID    int64   `json:"positionID"`
		InstrumentID  int64   `json:"instrumentID"`
		UnitsToDeduct float64 `json:"unitsToDeduct"`
		OrderID       int64   `json:"orderID"`
		OrderType     int     `json:"orderType"`
		StatusID      int     `json:"statusID"`
		OpenDateTime  string  `json:"openDateTime"`
		LastUpdate    string  `json:"lastUpdate"`
	} `json:"orderForClose"`
	Token string `json:"token"`
}

type closeRequest struct {
	InstrumentID  int64    `json:"InstrumentID"`
	UnitsToDeduct *float64 `json:"UnitsToDeduct"`
}

// ClosePosition closes a position fully (unitsToDeduct nil) or partly. It is
// naturally idempotent on eToro's side: closing a closed or closing position
// is rejected (codes 631/632), so a resumed run checks the position first.
func (c *Client) ClosePosition(ctx context.Context, positionID, instrumentID int64, unitsToDeduct *float64, requestID string) (CloseAccepted, error) {
	if positionID <= 0 || instrumentID <= 0 {
		return CloseAccepted{}, fmt.Errorf("positionID and instrumentID are required")
	}
	if unitsToDeduct != nil && !(*unitsToDeduct > 0) {
		return CloseAccepted{}, fmt.Errorf("unitsToDeduct must be positive or nil (full close)")
	}
	var resp CloseAccepted
	body := closeRequest{InstrumentID: instrumentID, UnitsToDeduct: unitsToDeduct}
	err := c.do(ctx, call{method: "POST", path: c.closePositionPath(positionID), body: body, requestID: requestID, write: true}, &resp)
	return resp, err
}

// ClosedFill is one position fill of a close order.
type ClosedFill struct {
	PositionID     int64   `json:"positionID"`
	Occurred       string  `json:"occurred"`
	Rate           float64 `json:"rate"`
	Units          float64 `json:"units"`
	ConversionRate float64 `json:"conversionRate"`
	Amount         float64 `json:"amount"`
}

// CloseOrderInfo is GET /api/v1/trading/info/{demo|real}/close-orders/{orderId}.
type CloseOrderInfo struct {
	OrderID         int64        `json:"orderID"`
	StatusID        int          `json:"statusID"`
	ReferenceID     string       `json:"referenceID"`
	OrderType       int          `json:"orderType"`
	ErrorCode       int          `json:"errorCode"`
	ErrorMessage    string       `json:"errorMessage"`
	InstrumentID    int64        `json:"instrumentID"`
	RequestOccurred string       `json:"requestOccurred"`
	Proceeds        float64      `json:"proceeds"`
	Positions       []ClosedFill `json:"positions"`
}

// GetCloseOrder fetches a close order and its fills.
func (c *Client) GetCloseOrder(ctx context.Context, orderID int64) (CloseOrderInfo, error) {
	var info CloseOrderInfo
	err := c.do(ctx, call{method: "GET", path: c.closeOrderInfoPath(orderID)}, &info)
	return info, err
}

// Cost is one component of a what-if cost preview, in the order currency.
type Cost struct {
	Type     string  `json:"costType"` // markup | marketSpread | transactionFee | overnightFee | overWeekendFee | sdrt
	Currency string  `json:"currency"`
	Value    float64 `json:"value"`
}

// Cost types.
const (
	CostMarkup         = "markup"
	CostMarketSpread   = "marketSpread"
	CostTransactionFee = "transactionFee"
	CostOvernightFee   = "overnightFee"
	CostOverWeekendFee = "overWeekendFee"
	CostSDRT           = "sdrt"
)

// CostPreview is POST /api/v2/trading/info[/demo]/costs.
type CostPreview struct {
	InstrumentID int64  `json:"instrumentId"`
	Symbol       string `json:"symbol"`
	Costs        []Cost `json:"costs"`
	LastUpdated  string `json:"lastUpdated"`
}

// Get returns the value of one cost type (0 when absent).
func (p CostPreview) Get(costType string) float64 {
	for _, c := range p.Costs {
		if c.Type == costType {
			return c.Value
		}
	}
	return 0
}

// Costs previews what an open order would cost now. Read-only (a POST that
// changes nothing), so it uses the read budget and is retried like a read.
func (c *Client) Costs(ctx context.Context, req OrderRequest) (CostPreview, error) {
	if req.OrderType == "" {
		req.OrderType = OrderMarket
	}
	if err := req.Validate(); err != nil {
		return CostPreview{}, err
	}
	var resp CostPreview
	err := c.do(ctx, call{method: "POST", path: c.costsPath(), body: req}, &resp)
	return resp, err
}

// LeverageConfig is one allowed (settlement, direction) combination.
type LeverageConfig struct {
	SettlementType            string  `json:"settlementType"`
	Direction                 string  `json:"direction"` // long | short
	LeverageValues            []int   `json:"leverageValues"`
	IsPotential               bool    `json:"isPotential"`
	MinPositionAmount         float64 `json:"minPositionAmount"`
	AllowEditStopLoss         bool    `json:"allowEditStopLoss"`
	MinStopLossPercentage     float64 `json:"minStopLossPercentage"`
	MaxStopLossPercentage     float64 `json:"maxStopLossPercentage"`
	DefaultStopLossPercentage float64 `json:"defaultStopLossPercentage"`
	MaxTakeProfitPercentage   float64 `json:"maxTakeProfitPercentage"`
	AllowStopLossTakeProfit   bool    `json:"allowStopLossTakeProfit"`
}

// Eligibility is the connected account's trading rules for one instrument.
type Eligibility struct {
	InstrumentID              int64            `json:"instrumentId"`
	Symbol                    string           `json:"symbol"`
	MinPositionExposure       float64          `json:"minPositionExposure"`
	MaxUnitsPerOrder          float64          `json:"maxUnitsPerOrder"`
	AllowOpenPosition         bool             `json:"allowOpenPosition"`
	AllowClosePosition        bool             `json:"allowClosePosition"`
	AllowPartialClosePosition bool             `json:"allowPartialClosePosition"`
	AllowMitOrders            bool             `json:"allowMitOrders"`
	AllowTrailingStopLoss     bool             `json:"allowTrailingStopLoss"`
	RequiresW8Ben             bool             `json:"requiresW8Ben"`
	UnitsQuantityType         string           `json:"unitsQuantityType"`
	TradeUnitType             string           `json:"tradeUnitType"`
	LeverageConfigs           []LeverageConfig `json:"leverageConfigs"`
}

// Config returns the leverage config that allows (direction, leverage),
// or false.
func (e Eligibility) Config(direction string, leverage int) (LeverageConfig, bool) {
	for _, lc := range e.LeverageConfigs {
		if !strings.EqualFold(lc.Direction, direction) || lc.IsPotential {
			continue
		}
		for _, v := range lc.LeverageValues {
			if v == leverage {
				return lc, true
			}
		}
	}
	return LeverageConfig{}, false
}

type eligibilityRequest struct {
	InstrumentIDs []int64  `json:"instrumentIds,omitempty"`
	Symbols       []string `json:"symbols,omitempty"`
	Currency      string   `json:"currency,omitempty"`
}

type eligibilityResponse struct {
	Currency      string        `json:"currency"`
	Eligibilities []Eligibility `json:"eligibilities"`
}

// Eligibility fetches the account's rules for the instruments. Read-only.
func (c *Client) Eligibility(ctx context.Context, instrumentIDs []int64) ([]Eligibility, error) {
	if len(instrumentIDs) == 0 {
		return nil, fmt.Errorf("at least one instrument ID is required")
	}
	var resp eligibilityResponse
	body := eligibilityRequest{InstrumentIDs: instrumentIDs, Currency: "USD"}
	err := c.do(ctx, call{method: "POST", path: c.eligibilityPath(), body: body}, &resp)
	return resp.Eligibilities, err
}
