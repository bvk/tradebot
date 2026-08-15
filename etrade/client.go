// Copyright (c) 2026 Deepak Vankadaru

package etrade

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bvk/tradebot/etrade/autologin"
	"github.com/bvk/tradebot/etrade/internal"
	"github.com/bvk/tradebot/syncmap"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

type Credentials = autologin.Credentials

var ErrExtendedHoursClosed = errors.New("etrade: extended hours trading service is unavailable (7am-8pm ET)")

// Response envelope types — unexported, used only in this file for JSON unmarshaling.

type ordersResponseWrapper struct {
	OrdersResponse ordersResponse `json:"OrdersResponse"`
}

type ordersResponse struct {
	// Marker is set when the result was truncated to a single page -- pass
	// it back as the "marker" query param to fetch the next page. Empty
	// when this is the last (or only) page.
	Marker string               `json:"marker"`
	Order  []*internal.APIOrder `json:"Order"`
}

type quoteResponseWrapper struct {
	QuoteResponse quoteResponse `json:"QuoteResponse"`
}

type quoteResponse struct {
	QuoteData []*internal.APIQuoteData `json:"QuoteData"`
}

type balanceResponseWrapper struct {
	BalanceResponse *internal.APIBalanceResponse `json:"BalanceResponse"`
}

// cancelOrderRequestWrapper is the outer envelope for PUT /v1/accounts/{id}/orders/cancel.
type cancelOrderRequestWrapper struct {
	CancelOrderRequest cancelOrderRequest `json:"CancelOrderRequest"`
}

type cancelOrderRequest struct {
	OrderID int64 `json:"orderId"`
}

// previewOrderRequestWrapper is the outer envelope for POST /v1/accounts/{id}/orders/preview.
type previewOrderRequestWrapper struct {
	PreviewOrderRequest previewOrderRequest `json:"PreviewOrderRequest"`
}

type previewOrderRequest struct {
	ClientOrderID string             `json:"clientOrderId,omitempty"`
	OrderType     string             `json:"orderType"`
	Order         []placeOrderDetail `json:"Order"`
}

type previewOrderResponseWrapper struct {
	PreviewOrderResponse previewOrderResponse `json:"PreviewOrderResponse"`
}

type previewOrderResponse struct {
	PreviewIds []previewOrderID `json:"PreviewIds"`
}

type previewOrderID struct {
	PreviewID int64 `json:"previewId"`
}

// placeOrderRequestWrapper is the outer envelope for POST /v1/accounts/{id}/orders/place.
type placeOrderRequestWrapper struct {
	PlaceOrderRequest placeOrderRequest `json:"PlaceOrderRequest"`
}

type placeOrderRequest struct {
	ClientOrderID string             `json:"clientOrderId,omitempty"`
	OrderType     string             `json:"orderType"`
	PreviewIds    []previewOrderID   `json:"PreviewIds,omitempty"`
	Order         []placeOrderDetail `json:"Order"`
}

type placeOrderDetail struct {
	AllOrNone     bool                   `json:"allOrNone"`
	PriceType     string                 `json:"priceType"`
	OrderTerm     string                 `json:"orderTerm"`
	MarketSession string                 `json:"marketSession"`
	LimitPrice    decimal.Decimal        `json:"limitPrice"`
	Instrument    []placeOrderInstrument `json:"Instrument"`
}

type placeOrderInstrument struct {
	Product      placeOrderProduct `json:"Product"`
	OrderAction  string            `json:"orderAction"`
	QuantityType string            `json:"quantityType"`
	Quantity     decimal.Decimal   `json:"quantity"`
}

type placeOrderProduct struct {
	SecurityType string `json:"securityType"`
	Symbol       string `json:"symbol"`
}

// placeOrderResponseWrapper is the outer envelope for the place order response.
type placeOrderResponseWrapper struct {
	PlaceOrderResponse placeOrderResponse `json:"PlaceOrderResponse"`
}

type placeOrderResponse struct {
	OrderIds []placeOrderID `json:"OrderIds"`
}

type placeOrderID struct {
	OrderID       int64  `json:"orderId"`
	ClientOrderID string `json:"clientOrderId"`
}

// Client manages HTTP connectivity and background polling for the E*TRADE API.
type Client struct {
	lifeCtx    context.Context
	lifeCancel context.CancelCauseFunc

	wg sync.WaitGroup

	opts  Options
	creds Credentials

	httpClient http.Client

	// nonceCounter is incremented atomically for each OAuth request to ensure
	// each nonce is unique.
	nonceCounter atomic.Int64

	// symbolOrderTopicMap is a per-symbol topic for order updates. Products
	// subscribe to their symbol's topic to receive live order state changes.
	symbolOrderTopicMap syncmap.Map[string, *topic.Topic[*internal.Order]]

	// symbolPriceTopicMap is a per-symbol topic for quote updates. Any symbol
	// present in this map is included in each price poll cycle.
	symbolPriceTopicMap syncmap.Map[string, *topic.Topic[*internal.Quote]]

	// balancesTopicMap holds balancesTopic that publishes account balance
	// updates polled from the API. Each account has separate balance, so this is
	// a mapping from AccountID to it's balance.
	balancesTopicMap map[string]*topic.Topic[*internal.Balance]

	// refreshOrderTopicMap holds orderTopic that accepts order IDs that need to
	// be tracked until completion. goRefreshOrders reads from this topic, polls
	// GetOrder until the order is done, and publishes each update to the symbol
	// order topic. Each account bash separate order-id namespace, so this is a
	// mapping from AccountID to it's order updates topic.
	refreshOrderTopicMap map[string]*topic.Topic[int64]
}

// New creates a new E*TRADE client, verifies credentials, and starts background
// goroutines for token renewal and polling.
func New(ctx context.Context, creds *Credentials, opts *Options) (*Client, error) {
	if err := creds.Check(); err != nil {
		return nil, err
	}
	if opts == nil {
		opts = new(Options)
	}
	opts.setDefaults()
	if err := opts.Check(); err != nil {
		return nil, err
	}

	lifeCtx, lifeCancel := context.WithCancelCause(context.Background())
	c := &Client{
		lifeCtx:              lifeCtx,
		lifeCancel:           lifeCancel,
		opts:                 *opts,
		creds:                *creds,
		httpClient:           http.Client{Timeout: opts.HttpClientTimeout},
		balancesTopicMap:     make(map[string]*topic.Topic[*internal.Balance]),
		refreshOrderTopicMap: make(map[string]*topic.Topic[int64]),
	}
	c.nonceCounter.Store(time.Now().UnixNano())

	for _, a := range creds.Accounts {
		c.balancesTopicMap[a.AccountID] = topic.New[*internal.Balance]()
		c.refreshOrderTopicMap[a.AccountID] = topic.New[int64]()
	}

	// Verify credentials are valid before starting goroutines. Use GetBalance
	// rather than RenewAccessToken because E*TRADE rate-limits the renew
	// endpoint — calling it on every CLI invocation causes intermittent 401s.
	for _, a := range creds.Accounts {
		if _, err := c.GetBalance(ctx, a.AccountID); err != nil {
			lifeCancel(err)
			return nil, fmt.Errorf("etrade: credential verification failed: %w", err)
		}
	}

	c.wg.Add(2)
	go c.goRenewToken(c.lifeCtx)
	go c.goPollPrices(c.lifeCtx)

	for _, a := range creds.Accounts {
		c.wg.Add(2)
		go c.goPollBalances(c.lifeCtx, a.AccountID)
		go c.goRefreshOrders(c.lifeCtx, a.AccountID)
	}

	return c, nil
}

// Close shuts down all background goroutines and releases resources.
func (c *Client) Close() error {
	c.lifeCancel(os.ErrClosed)
	c.wg.Wait()
	return nil
}

// getSymbolOrdersTopic returns (creating if necessary) the per-symbol order update topic.
func (c *Client) getSymbolOrdersTopic(symbol string) *topic.Topic[*internal.Order] {
	tp, ok := c.symbolOrderTopicMap.Load(symbol)
	if !ok {
		tp, _ = c.symbolOrderTopicMap.LoadOrStore(symbol, topic.New[*internal.Order]())
	}
	return tp
}

// getSymbolPriceTopic returns (creating if necessary) the per-symbol price update topic.
// Calling this registers the symbol for inclusion in polling.
func (c *Client) getSymbolPriceTopic(symbol string) *topic.Topic[*internal.Quote] {
	tp, ok := c.symbolPriceTopicMap.Load(symbol)
	if !ok {
		return nil
	}
	return tp
}

func (c *Client) getSymbolInfo(symbol string) (*internal.Quote, bool) {
	tp, ok := c.symbolPriceTopicMap.Load(symbol)
	if !ok {
		return nil, false
	}
	sinfo, ok := tp.Last()
	if !ok {
		return nil, false
	}
	return sinfo, true
}

// WatchSymbol configures the client to periodically poll for symbols price and other information.
func (c *Client) WatchSymbol(ctx context.Context, symbol string) (*internal.Quote, error) {
	if sinfo, ok := c.getSymbolInfo(symbol); ok {
		return sinfo, nil
	}
	sinfos, err := c.GetQuotes(ctx, []string{symbol})
	if err != nil {
		return nil, err
	}
	tp, loaded := c.symbolPriceTopicMap.LoadOrStore(symbol, topic.New[*internal.Quote]())
	if !loaded {
		tp.Send(sinfos[0])
		return sinfos[0], nil
	}
	sinfo, _ := tp.Last()
	return sinfo, nil
}

// oauthSign computes an OAuth 1.0a Authorization header value for the given
// HTTP method, base URL, and query parameters. Query parameters are included
// in the signature computation but must also appear in the actual request URL.
func (c *Client) oauthSign(method, baseURL string, queryParams url.Values) string {
	nonce := strconv.FormatInt(c.nonceCounter.Add(1), 10)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	// Collect all parameters: OAuth protocol params + request query params.
	params := map[string]string{
		"oauth_consumer_key":     c.creds.ConsumerKey,
		"oauth_nonce":            nonce,
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        timestamp,
		"oauth_token":            c.creds.AccessToken,
		"oauth_version":          "1.0",
	}
	for k, vals := range queryParams {
		if len(vals) > 0 {
			params[k] = vals[0]
		}
	}

	// Sort and percent-encode into a normalized parameter string.
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, autologin.OauthEscape(k)+"="+autologin.OauthEscape(params[k]))
	}
	normalizedParams := strings.Join(parts, "&")

	// Signature base string: METHOD & base_url & normalized_params (all percent-encoded).
	sigBase := strings.ToUpper(method) + "&" + autologin.OauthEscape(baseURL) + "&" + autologin.OauthEscape(normalizedParams)

	// Signing key: percent-encoded consumer_secret & percent-encoded token_secret.
	signingKey := autologin.OauthEscape(c.creds.ConsumerSecret) + "&" + autologin.OauthEscape(c.creds.AccessTokenSecret)

	// HMAC-SHA1, base64-encoded.
	h := hmac.New(sha1.New, []byte(signingKey))
	h.Write([]byte(sigBase))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// Build the Authorization header. Signature is percent-encoded because
	// base64 output may contain '+', '/', and '=' which are reserved in OAuth.
	headerParts := []string{
		`oauth_consumer_key="` + autologin.OauthEscape(c.creds.ConsumerKey) + `"`,
		`oauth_nonce="` + autologin.OauthEscape(nonce) + `"`,
		`oauth_signature="` + autologin.OauthEscape(signature) + `"`,
		`oauth_signature_method="HMAC-SHA1"`,
		`oauth_timestamp="` + timestamp + `"`,
		`oauth_token="` + autologin.OauthEscape(c.creds.AccessToken) + `"`,
		`oauth_version="1.0"`,
	}
	return "OAuth " + strings.Join(headerParts, ", ")
}

// do executes a signed HTTP request. For GET requests, queryParams are added
// to the URL and included in the OAuth signature. For POST/PUT, body is the
// JSON payload and queryParams should be nil.
func (c *Client) do(ctx context.Context, method, apiPath string, queryParams url.Values, body string) (*http.Response, error) {
	baseURL := &url.URL{
		Scheme: "https",
		Host:   c.opts.restHostname(),
		Path:   apiPath,
	}
	authHeader := c.oauthSign(method, baseURL.String(), queryParams)
	if len(queryParams) > 0 {
		baseURL.RawQuery = queryParams.Encode()
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL.String(), bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// sleep waits for d or until ctx is canceled. Returns context.Cause(ctx) if
// the context finishes first.
func sleep(ctx context.Context, d time.Duration) error {
	sctx, scancel := context.WithTimeout(ctx, d)
	<-sctx.Done()
	scancel()
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return nil
}

// handleHTTPError maps E*TRADE HTTP status codes to errors. Returns true if
// the caller should retry (rate-limited), false otherwise.
func handleHTTPError(ctx context.Context, resp *http.Response, apiPath string) (retry bool, err error) {
	switch resp.StatusCode {
	case internal.StatusNotFound:
		body, _ := io.ReadAll(resp.Body)
		slog.Error("etrade: resource not found", "path", apiPath, "body", string(body))
		return false, os.ErrNotExist
	case internal.StatusUnauthorized:
		slog.Error("etrade: OAuth token expired or invalid; re-run 'setup etrade' to reauthorize", "path", apiPath)
		return false, fmt.Errorf("etrade: unauthorized: %w", os.ErrPermission)
	case internal.StatusTooManyRequests:
		timeout := time.Second
		if v := resp.Header.Get("Retry-After"); v != "" {
			if n, parseErr := strconv.Atoi(v); parseErr == nil {
				timeout = time.Duration(n) * time.Second
			}
		}
		slog.Warn("etrade: rate limited, will retry", "path", apiPath, "retry-after", timeout)
		if sleepErr := sleep(ctx, timeout); sleepErr != nil {
			return false, sleepErr
		}
		return true, nil
	default:
		body, _ := io.ReadAll(resp.Body)
		// Check for E*TRADE error codes in 4xx responses (delivered in XML body).
		if resp.StatusCode == http.StatusBadRequest {
			if strings.Contains(string(body), "<code>1513</code>") {
				slog.Warn("etrade: extended hours service unavailable", "path", apiPath)
				return false, ErrExtendedHoursClosed
			}
		}
		slog.Error("etrade: unexpected HTTP status", "path", apiPath, "status", resp.StatusCode, "body", string(body))
		return false, fmt.Errorf("etrade: %s returned HTTP %d", apiPath, resp.StatusCode)
	}
}

// doGetJSON performs a signed GET and unmarshals the 200 response into resp.
// Returns nil (with resp unchanged) on 204 No Content.
func doGetJSON[PT *T, T any](ctx context.Context, c *Client, apiPath string, queryParams url.Values, resp PT) error {
	for {
		httpResp, err := c.do(ctx, http.MethodGet, apiPath, queryParams, "")
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("etrade: GET failed", "path", apiPath, "err", err)
			}
			return err
		}

		if httpResp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			if err != nil {
				slog.Error("etrade: could not read GET response body", "path", apiPath, "err", err)
				return err
			}
			slog.Debug(string(data))
			if err := json.Unmarshal(data, resp); err != nil {
				slog.Error("etrade: could not unmarshal GET response", "path", apiPath, "body", string(data), "err", err)
				return err
			}
			return nil
		}

		if httpResp.StatusCode == http.StatusNoContent {
			// E*TRADE returns 204 when there are no results (e.g., no open orders).
			httpResp.Body.Close()
			return nil
		}

		retry, err := handleHTTPError(ctx, httpResp, apiPath)
		httpResp.Body.Close()
		if err != nil {
			return err
		}
		if retry {
			continue
		}
	}
}

// doPostJSON performs a signed POST with a JSON body and unmarshals the response.
func doPostJSON[PT *T, T any](ctx context.Context, c *Client, apiPath string, request any, resp PT) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	body := string(data)

	for {
		httpResp, err := c.do(ctx, http.MethodPost, apiPath, nil, body)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("etrade: POST failed", "path", apiPath, "err", err)
			}
			return err
		}

		if httpResp.StatusCode == http.StatusOK {
			respData, err := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			if err != nil {
				slog.Error("etrade: could not read POST response body", "path", apiPath, "err", err)
				return err
			}
			if err := json.Unmarshal(respData, resp); err != nil {
				slog.Error("etrade: could not unmarshal POST response", "path", apiPath, "body", string(respData), "err", err)
				return err
			}
			return nil
		}

		retry, err := handleHTTPError(ctx, httpResp, apiPath)
		httpResp.Body.Close()
		if err != nil {
			return err
		}
		if retry {
			continue
		}
	}
}

// doPutEmpty performs a signed PUT with no body and discards the response body.
// Used for cancel order, which only needs a 200 OK confirmation.
func (c *Client) doPutEmpty(ctx context.Context, apiPath string) error {
	for {
		httpResp, err := c.do(ctx, http.MethodPut, apiPath, nil, "")
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("etrade: PUT failed", "path", apiPath, "err", err)
			}
			return err
		}

		if httpResp.StatusCode == http.StatusOK {
			io.Copy(io.Discard, httpResp.Body)
			httpResp.Body.Close()
			return nil
		}

		retry, err := handleHTTPError(ctx, httpResp, apiPath)
		httpResp.Body.Close()
		if err != nil {
			return err
		}
		if retry {
			continue
		}
	}
}

// RenewAccessToken calls GET /oauth/renew_access_token to extend the OAuth
// session. E*TRADE tokens expire at midnight US Eastern time; this must be
// called periodically to keep them alive.
func (c *Client) RenewAccessToken(ctx context.Context) error {
	httpResp, err := c.do(ctx, http.MethodGet, "/oauth/renew_access_token", nil, "")
	if err != nil {
		return err
	}
	io.Copy(io.Discard, httpResp.Body)
	httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("etrade: renew_access_token returned HTTP %d", httpResp.StatusCode)
	}
	return nil
}

// GetQuotes fetches current market quotes for the given symbols from
// GET /v1/market/quote/{symbols}.
func (c *Client) GetQuotes(ctx context.Context, symbols []string) ([]*internal.Quote, error) {
	params := url.Values{
		"detailFlag":          {"ALL"},
		"requireEarningsDate": {"true"},
	}
	apiPath := "/v1/market/quote/" + url.PathEscape(strings.Join(symbols, ","))
	var wrapper quoteResponseWrapper
	if err := doGetJSON(ctx, c, apiPath, params, &wrapper); err != nil {
		return nil, err
	}
	quotes := make([]*internal.Quote, 0, len(wrapper.QuoteResponse.QuoteData))
	for _, qd := range wrapper.QuoteResponse.QuoteData {
		quotes = append(quotes, internal.NewQuoteFromAPI(qd))
	}
	return quotes, nil
}

// GetBalance fetches the current account balance from
// GET /v1/accounts/{accountIdKey}/balance.
func (c *Client) GetBalance(ctx context.Context, accountID string) (*internal.Balance, error) {
	accountIDKey, err := c.creds.GetAccountIDKey(accountID)
	if err != nil {
		return nil, err
	}
	apiPath := "/v1/accounts/" + url.PathEscape(accountIDKey) + "/balance"
	params := url.Values{
		"instType":    {"BROKERAGE"},
		"realTimeNAV": {"true"},
	}
	var wrapper balanceResponseWrapper
	if err := doGetJSON(ctx, c, apiPath, params, &wrapper); err != nil {
		return nil, err
	}
	if wrapper.BalanceResponse == nil {
		return nil, fmt.Errorf("etrade: empty BalanceResponse")
	}
	return internal.NewBalanceFromAPI(wrapper.BalanceResponse), nil
}

// maxOrderPages caps the number of pages ListOpenOrders will follow, as a
// safety valve against an API bug returning a marker that never clears.
// Real accounts don't have anywhere close to this many open orders.
const maxOrderPages = 1000

// ListOpenOrders fetches all currently open orders for the account from
// GET /v1/accounts/{accountIdKey}/orders?status=OPEN, following the API's
// marker-based pagination (ordersResponse.Marker) until every page has been
// fetched. Callers get the complete list either way -- without this, an
// account with more open orders than fit on one page would silently see
// only the first page.
func (c *Client) ListOpenOrders(ctx context.Context, accountID string) ([]*internal.Order, error) {
	accountIDKey, err := c.creds.GetAccountIDKey(accountID)
	if err != nil {
		return nil, err
	}
	apiPath := "/v1/accounts/" + url.PathEscape(accountIDKey) + "/orders"

	var orders []*internal.Order

	marker := ""
	for page := 0; ; page++ {
		if page >= maxOrderPages {
			return nil, fmt.Errorf("etrade: ListOpenOrders exceeded %d pages (marker %q); aborting", maxOrderPages, marker)
		}

		params := url.Values{"status": {"OPEN"}}
		if marker != "" {
			params.Set("marker", marker)
		}

		var wrapper ordersResponseWrapper
		if err := doGetJSON(ctx, c, apiPath, params, &wrapper); err != nil {
			return nil, err
		}
		for _, apiOrder := range wrapper.OrdersResponse.Order {
			if o := internal.NewOrderFromAPI(accountID, apiOrder); o != nil {
				orders = append(orders, o)
			}
		}

		marker = wrapper.OrdersResponse.Marker
		if marker == "" {
			break
		}
	}

	return orders, nil
}

// GetOrder fetches a single order by its E*TRADE order ID from
// GET /v1/accounts/{accountIdKey}/orders/{orderId}.
func (c *Client) GetOrder(ctx context.Context, accountID string, orderID int64) (*internal.Order, error) {
	accountIDKey, err := c.creds.GetAccountIDKey(accountID)
	if err != nil {
		return nil, err
	}
	apiPath := "/v1/accounts/" + url.PathEscape(accountIDKey) + "/orders/" + strconv.FormatInt(orderID, 10)
	var wrapper ordersResponseWrapper
	if err := doGetJSON(ctx, c, apiPath, nil, &wrapper); err != nil {
		return nil, err
	}
	if len(wrapper.OrdersResponse.Order) == 0 {
		return nil, os.ErrNotExist
	}
	if len(wrapper.OrdersResponse.Order) > 1 {
		return nil, fmt.Errorf("etrade: GetOrder %d: API returned %d top-level orders, expected 1", orderID, len(wrapper.OrdersResponse.Order))
	}
	order := internal.NewOrderFromAPI(accountID, wrapper.OrdersResponse.Order[0])
	if order == nil {
		return nil, os.ErrNotExist
	}
	return order, nil
}

// PlaceLimitOrder submits a limit order via the two-step preview+place flow required
// by E*TRADE. First calls POST /orders/preview to satisfy the session check,
// then calls POST /orders/place with the returned previewId. Returns the
// E*TRADE-assigned numeric order ID. clientOrderID must be a decimal integer
// string (E*TRADE only accepts digits in this field). marketSession is
// "REGULAR" or "EXTENDED"; orderTerm must be consistent with it
// ("GOOD_UNTIL_CANCEL" for REGULAR, "GOOD_FOR_DAY" for EXTENDED).
func (c *Client) PlaceLimitOrder(ctx context.Context, accountID, symbol, side string, qty, limitPrice decimal.Decimal, clientOrderID, orderTerm, marketSession string) (int64, error) {
	accountIDKey, err := c.creds.GetAccountIDKey(accountID)
	if err != nil {
		return -1, err
	}
	accountPath := "/v1/accounts/" + url.PathEscape(accountIDKey)
	orderDetail := placeOrderDetail{
		PriceType:     "LIMIT",
		OrderTerm:     orderTerm,
		MarketSession: marketSession,
		LimitPrice:    limitPrice,
		Instrument: []placeOrderInstrument{{
			Product:      placeOrderProduct{SecurityType: "EQ", Symbol: symbol},
			OrderAction:  strings.ToUpper(side),
			QuantityType: "QUANTITY",
			Quantity:     qty,
		}},
	}

	// Step 1: preview (required by E*TRADE — skipping causes error code 101).
	previewReq := previewOrderRequestWrapper{
		PreviewOrderRequest: previewOrderRequest{
			ClientOrderID: clientOrderID,
			OrderType:     "EQ",
			Order:         []placeOrderDetail{orderDetail},
		},
	}
	var previewResp previewOrderResponseWrapper
	if err := doPostJSON(ctx, c, accountPath+"/orders/preview", &previewReq, &previewResp); err != nil {
		return 0, fmt.Errorf("etrade: preview order failed: %w", err)
	}
	if len(previewResp.PreviewOrderResponse.PreviewIds) == 0 {
		return 0, fmt.Errorf("etrade: preview order response contained no PreviewIds")
	}

	// Step 2: place using the preview ID obtained above.
	placeReq := placeOrderRequestWrapper{
		PlaceOrderRequest: placeOrderRequest{
			ClientOrderID: clientOrderID,
			OrderType:     "EQ",
			PreviewIds:    previewResp.PreviewOrderResponse.PreviewIds,
			Order:         []placeOrderDetail{orderDetail},
		},
	}
	var resp placeOrderResponseWrapper
	if err := doPostJSON(ctx, c, accountPath+"/orders/place", &placeReq, &resp); err != nil {
		return 0, fmt.Errorf("etrade: place order failed: %w", err)
	}
	if len(resp.PlaceOrderResponse.OrderIds) == 0 {
		return 0, fmt.Errorf("etrade: place order response contained no OrderIds")
	}
	return resp.PlaceOrderResponse.OrderIds[0].OrderID, nil
}

// CancelOrder requests cancellation of an order via
// PUT /v1/accounts/{accountIdKey}/orders/cancel.
func (c *Client) CancelOrder(ctx context.Context, accountID string, orderID int64) error {
	accountIDKey, err := c.creds.GetAccountIDKey(accountID)
	if err != nil {
		return err
	}
	apiPath := "/v1/accounts/" + url.PathEscape(accountIDKey) + "/orders/cancel"
	data, _ := json.Marshal(cancelOrderRequestWrapper{CancelOrderRequest: cancelOrderRequest{OrderID: orderID}})
	for {
		httpResp, err := c.do(ctx, http.MethodPut, apiPath, nil, string(data))
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("etrade: PUT failed", "path", apiPath, "orderID", orderID, "err", err)
			}
			return err
		}
		if httpResp.StatusCode == http.StatusOK {
			io.Copy(io.Discard, httpResp.Body)
			httpResp.Body.Close()
			return nil
		}
		retry, err := handleHTTPError(ctx, httpResp, apiPath)
		httpResp.Body.Close()
		if err != nil {
			return err
		}
		if retry {
			continue
		}
	}
}

func (c *Client) goRenewToken(ctx context.Context) {
	defer c.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("etrade: CAUGHT PANIC in goRenewToken", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	for ctx.Err() == nil {
		if err := sleep(ctx, c.opts.TokenRenewalInterval); err != nil {
			return
		}
		if err := c.RenewAccessToken(ctx); err != nil {
			if errors.Is(err, context.Cause(ctx)) {
				return
			}
			slog.Error("etrade: token renewal failed; re-run 'setup etrade' to reauthorize", "err", err)
			c.lifeCancel(err)
			return
		}
		slog.Info("etrade: access token renewed")
	}
}

func (c *Client) goPollPrices(ctx context.Context) {
	defer c.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("etrade: CAUGHT PANIC in goPollPrices", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	for ; ctx.Err() == nil; sleep(ctx, c.opts.PollPricesInterval) {
		// Collect all symbols currently registered for price polling.
		var symbols []string
		c.symbolPriceTopicMap.Range(func(symbol string, _ *topic.Topic[*internal.Quote]) bool {
			symbols = append(symbols, symbol)
			return true
		})

		if len(symbols) > 0 {
			quotes, err := c.GetQuotes(ctx, symbols)
			if err != nil && !errors.Is(err, context.Cause(ctx)) {
				slog.Warn("etrade: could not poll prices (will retry)", "symbols", symbols, "err", err)
			}
			for _, quote := range quotes {
				c.getSymbolPriceTopic(quote.Symbol).Send(quote)
			}
		}
	}
}

func (c *Client) goPollBalances(ctx context.Context, accountID string) {
	defer c.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("etrade: CAUGHT PANIC in goPollBalances", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	balancesTopic, ok := c.balancesTopicMap[accountID]
	if !ok {
		slog.Error("etrade: account id key has no poll balances topic", "account-id", accountID)
		return
	}

	for ; ctx.Err() == nil; sleep(ctx, c.opts.PollBalancesInterval) {
		balance, err := c.GetBalance(ctx, accountID)
		if err != nil {
			if !errors.Is(err, context.Cause(ctx)) {
				slog.Warn("etrade: could not poll balance (will retry)", "err", err)
			}
			continue
		}
		balancesTopic.Send(balance)
	}
}

// TrackOrder queues an order ID for tracking by goRefreshOrders. Product.go
// calls this immediately after placing an order so that fills and cancellations
// are detected even after the order disappears from the open-orders poll.
func (c *Client) TrackOrder(accountID string, orderID int64) {
	if v, ok := c.refreshOrderTopicMap[accountID]; ok {
		v.Send(orderID)
	} else {
		slog.Error("etrade: account id key has no refresh order topic", "account-id", accountID, "order-id", orderID)
	}
}

func (c *Client) goRefreshOrders(ctx context.Context, accountID string) {
	defer c.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("etrade: CAUGHT PANIC in goRefreshOrders", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	refreshOrderTopic, ok := c.refreshOrderTopicMap[accountID]
	if !ok {
		slog.Error("etrade: account id key has no refresh order topic", "account-id", accountID)
		return
	}

	receiver, err := topic.Subscribe(refreshOrderTopic, 0, true)
	if err != nil {
		slog.Error("etrade: could not subscribe to refreshOrderTopic (unexpected)", "err", err)
		return
	}
	defer receiver.Close()

	receiveCh, err := topic.ReceiveCh(receiver)
	if err != nil {
		slog.Error("etrade: could not create receive channel for the refreshOrderTopic (unexpected)", "err", err)
		return
	}
	trackingOrderMap := make(map[int64]*internal.Order)

	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return

		case id := <-receiveCh:
			if _, ok := trackingOrderMap[id]; !ok {
				trackingOrderMap[id] = nil
			}

		case <-time.After(c.opts.PollOrdersInterval):
			orders, err := c.ListOpenOrders(ctx, accountID)
			if err != nil {
				if !errors.Is(err, context.Cause(ctx)) {
					slog.Warn("etrade: could not list open orders (will retry)", "err", err)
				}
				continue
			}

			newOrderMap := make(map[int64]*internal.Order)
			for _, v := range orders {
				c.getSymbolOrdersTopic(v.Symbol).Send(v)
				if _, ok := trackingOrderMap[v.OrderID]; ok {
					newOrderMap[v.OrderID] = v
				}
			}

			var missingOrderIDs []int64
			for k := range trackingOrderMap {
				if _, ok := newOrderMap[k]; !ok {
					missingOrderIDs = append(missingOrderIDs, k)
				}
			}

			for _, orderID := range missingOrderIDs {
				order, err := c.GetOrder(ctx, accountID, orderID)
				if err != nil {
					if !errors.Is(err, context.Cause(ctx)) {
						slog.Warn("etrade: could not refresh order (will retry)", "orderID", orderID, "err", err)
					}
					newOrderMap[orderID] = trackingOrderMap[orderID] // This order will be fetched again.
					continue
				}
				if !order.IsDone() {
					slog.Error("etrade: live order is found missing in list open orders (unexpected)", "orderID", orderID)
				}
				c.getSymbolOrdersTopic(order.Symbol).Send(order)
			}

			trackingOrderMap = newOrderMap
		}
	}
}
