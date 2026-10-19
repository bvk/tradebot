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
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/etrade/autologin"
	"github.com/bvk/tradebot/etrade/internal"
	"github.com/bvk/tradebot/gobs"
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

// portfolioResponseWrapper is the outer envelope for
// GET /v1/accounts/{id}/portfolio?lotsRequired=true.
type portfolioResponseWrapper struct {
	PortfolioResponse portfolioResponse `json:"PortfolioResponse"`
}

type portfolioResponse struct {
	AccountPortfolio []accountPortfolio `json:"AccountPortfolio"`
}

type accountPortfolio struct {
	AccountID  string               `json:"accountID"`
	Next       string               `json:"next"`
	NumPages   int32                `json:"totalNoOfPages"`
	NextPageNo string               `json:"nextPageNo"`
	Position   []*portfolioPosition `json:"Position"`
}

type portfolioPosition struct {
	PositionID  int64  `json:"positionId"`
	LotsDetails string `json:"lotsDetails"`
	Product     struct {
		Symbol string `json:"symbol"`
	} `json:"Product"`
}

type positionLotsResponseWrapper struct {
	PositionLotsResponse positionLotsResponse `json:"PositionLotsResponse"`
}

type positionLotsResponse struct {
	PositionLots []*positionLot `json:"PositionLot"`
}

type positionLot struct {
	LotsDetails string // Not a JSON generated field.
	AccountID   string // Not a JSON generated field.
	Symbol      string // Not a JSON generated field.

	PositionLotID int64           `json:"positionLotId"`
	OrderNo       int64           `json:"orderNo"`
	OriginalQty   decimal.Decimal `json:"originalQty"`
	RemainingQty  decimal.Decimal `json:"remainingQty"`
	Price         decimal.Decimal `json:"price"`        // per-share cost basis
	AcquiredDate  int64           `json:"acquiredDate"` // Unix milliseconds
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
	Lots         *placeOrderLots   `json:"Lots,omitempty"`
}

type placeOrderLots struct {
	Lot []*placeOrderLot `json:"Lot"`
}

// placeOrderLot specifies how many shares to draw from a specific tax lot.
// ID is int64 (E*TRADE's positionLotId); Size is the shares to sell from it.
type placeOrderLot struct {
	ID   int64           `json:"id"`
	Size decimal.Decimal `json:"size"`
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

	reloginMutex sync.Mutex

	opts  Options
	creds atomic.Pointer[Credentials]

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
	// a mapping from AccountID to it's balance. Written at startup and whenever a
	// credential reload adds a new account, so it is a thread-safe map.
	balancesTopicMap syncmap.Map[string, *topic.Topic[*internal.Balance]]

	// refreshOrderTopicMap holds orderTopic that accepts order IDs that need to
	// be tracked until completion. goRefreshOrders reads from this topic, polls
	// GetOrder until the order is done, and publishes each update to the symbol
	// order topic. Each account bash separate order-id namespace, so this is a
	// mapping from AccountID to it's order updates topic.
	refreshOrderTopicMap syncmap.Map[string, *topic.Topic[int64]]

	// accountMu guards accountSet and serializes per-account initialization.
	accountMu sync.Mutex
	// accountSet holds the AccountIDs that have been initialized (topics created
	// and, in daemon mode, polling goroutines spawned). Used to make
	// ensureNewAccounts idempotent across credential reloads.
	accountSet map[string]bool
	// initialized becomes true once New has finished its initial setup, so that
	// relogin only rebuilds the delta (newly-added accounts) afterwards.
	initialized atomic.Bool

	// portfolioMap holds tax lots for current asset holdings. Each account has
	// separate portfolio, so this is a mapping from (AccountID, symbol, orderID)
	// to it's lot detail url.
	portfolioMap syncmap.Map[string, *syncmap.Map[string, *syncmap.Map[int64, string]]]

	// lotDetailMap is a mapping from position URL to it's last known lots metadata.
	lotDetailMap syncmap.Map[string, []*positionLot]

	// portfolioMu serializes the read-modify-write updates to portfolioMap and
	// lotDetailMap in updatePortfolio, which can run from both goTrackPortfolio
	// and ResolveOrderLots for the same account.
	portfolioMu sync.Mutex
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
		lifeCtx:    lifeCtx,
		lifeCancel: lifeCancel,
		opts:       *opts,
		httpClient: http.Client{Timeout: opts.HttpClientTimeout},
		accountSet: make(map[string]bool),
	}
	c.creds.Store(creds.Clone())
	c.nonceCounter.Store(time.Now().UnixNano())

	if !c.opts.cmdlineToolMode {
		if err := c.relogin(ctx); err != nil {
			slog.Error("etrade: could not perform unattended relogin", "err", err)
			return nil, err
		}
	}

	for _, a := range creds.ActiveAccounts() {
		c.balancesTopicMap.Store(a.AccountID, topic.New[*internal.Balance]())
		c.refreshOrderTopicMap.Store(a.AccountID, topic.New[int64]())
		c.accountSet[a.AccountID] = true
	}

	if !opts.cmdlineToolMode {
		// Verify credentials are valid before starting goroutines. Use GetBalance
		// rather than RenewAccessToken because E*TRADE rate-limits the renew
		// endpoint — calling it on every CLI invocation causes intermittent 401s.
		for _, a := range creds.ActiveAccounts() {
			if _, err := c.GetBalance(ctx, a.AccountID); err != nil {
				lifeCancel(err)
				return nil, fmt.Errorf("etrade: credential verification failed: %w", err)
			}
		}

		c.wg.Add(2)
		go c.goRenewToken(c.lifeCtx)
		go c.goPollPrices(c.lifeCtx)

		for _, a := range creds.ActiveAccounts() {
			c.wg.Add(3)
			go c.goPollBalances(c.lifeCtx, a.AccountID)
			go c.goRefreshOrders(c.lifeCtx, a.AccountID)
			go c.goTrackPortfolio(c.lifeCtx, a.AccountID)
		}

		if c.opts.DisplayRunner != nil {
			c.wg.Add(1)
			go c.goLogin(c.lifeCtx)
		}
	}

	c.initialized.Store(true)
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
	creds := c.creds.Load()
	params := map[string]string{
		"oauth_consumer_key":     creds.ConsumerKey,
		"oauth_nonce":            nonce,
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        timestamp,
		"oauth_token":            creds.AccessToken,
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
	signingKey := autologin.OauthEscape(creds.ConsumerSecret) + "&" + autologin.OauthEscape(creds.AccessTokenSecret)

	// HMAC-SHA1, base64-encoded.
	h := hmac.New(sha1.New, []byte(signingKey))
	h.Write([]byte(sigBase))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// Build the Authorization header. Signature is percent-encoded because
	// base64 output may contain '+', '/', and '=' which are reserved in OAuth.
	headerParts := []string{
		`oauth_consumer_key="` + autologin.OauthEscape(creds.ConsumerKey) + `"`,
		`oauth_nonce="` + autologin.OauthEscape(nonce) + `"`,
		`oauth_signature="` + autologin.OauthEscape(signature) + `"`,
		`oauth_signature_method="HMAC-SHA1"`,
		`oauth_timestamp="` + timestamp + `"`,
		`oauth_token="` + autologin.OauthEscape(creds.AccessToken) + `"`,
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
func (c *Client) handleHTTPError(ctx context.Context, resp *http.Response, apiPath string) (retry bool, err error) {
	switch resp.StatusCode {
	case internal.StatusNotFound:
		body, _ := io.ReadAll(resp.Body)
		slog.Error("etrade: resource not found", "path", apiPath, "body", string(body))
		return false, os.ErrNotExist
	case internal.StatusUnauthorized:
		if err := c.relogin(ctx); err != nil {
			slog.Error("etrade: received unauthorized response and relogin has failed as well", "err", err)
		} else {
			return true, nil
		}
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
		// Check for E*TRADE error codes in any responses (delivered in XML body).
		if strings.Contains(string(body), "<code>1513</code>") {
			slog.Warn("etrade: extended hours service unavailable", "path", apiPath)
			return false, ErrExtendedHoursClosed
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

		retry, err := c.handleHTTPError(ctx, httpResp, apiPath)
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

		retry, err := c.handleHTTPError(ctx, httpResp, apiPath)
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

		retry, err := c.handleHTTPError(ctx, httpResp, apiPath)
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
	creds := c.creds.Load()
	accountIDKey, err := creds.GetAccountIDKey(accountID)
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
	creds := c.creds.Load()
	accountIDKey, err := creds.GetAccountIDKey(accountID)
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
	creds := c.creds.Load()
	accountIDKey, err := creds.GetAccountIDKey(accountID)
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
func (c *Client) PlaceLimitOrder(ctx context.Context, accountID, symbol, side string, qty, limitPrice decimal.Decimal, clientOrderID, orderTerm, marketSession string, plots []*placeOrderLot) (int64, error) {
	creds := c.creds.Load()
	accountIDKey, err := creds.GetAccountIDKey(accountID)
	if err != nil {
		return -1, err
	}

	var plotsWrapper *placeOrderLots
	if len(plots) != 0 {
		if !strings.EqualFold(side, "sell") {
			return -1, fmt.Errorf("etrade: tax lots are supported only on sell side")
		}
		plotsWrapper = &placeOrderLots{
			Lot: plots,
		}
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
			Lots:         plotsWrapper,
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
	creds := c.creds.Load()
	accountIDKey, err := creds.GetAccountIDKey(accountID)
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
		retry, err := c.handleHTTPError(ctx, httpResp, apiPath)
		httpResp.Body.Close()
		if err != nil {
			return err
		}
		if retry {
			continue
		}
	}
}

func (c *Client) makeTaxLotID(accountID, symbol, lotDetailURL string, orderNo int64) (gobs.TaxLotID, error) {
	if orderNo <= 0 {
		return "", fmt.Errorf("etrade: server order no must not be zero or -ve")
	}
	if !strings.HasPrefix(lotDetailURL, gobs.ETradeTaxLotIDPrefix) {
		return "", fmt.Errorf("etrade: lot detail url %q has invalid/unexpected prefix", lotDetailURL)
	}
	taxLotURL, err := url.Parse(lotDetailURL)
	if err != nil {
		return "", err
	}
	creds := c.creds.Load()
	if _, err := creds.GetAccountIDKey(accountID); err != nil {
		return "", err
	}
	if _, ok := c.symbolPriceTopicMap.Load(symbol); !ok {
		return "", fmt.Errorf("etrade: tax lot refers to invalid/unwatched symbol %q", symbol)
	}
	values := make(url.Values)
	values.Set("AccountID", accountID)
	values.Set("Symbol", symbol)
	values.Set("OrderNo", strconv.FormatInt(orderNo, 10))
	taxLotURL.RawQuery = values.Encode()
	return gobs.TaxLotID(taxLotURL.String()), nil
}

func (c *Client) parseTaxLotID(id gobs.TaxLotID) (accountID, symbol, lotDetailURL string, orderNo int64, err error) {
	if !strings.HasPrefix(string(id), gobs.ETradeTaxLotIDPrefix) {
		return "", "", "", -1, fmt.Errorf("etrade: tax lot id %q has invalid/unexpected prefix", id)
	}
	taxLotURL, err := url.Parse(string(id))
	if err != nil {
		return "", "", "", -1, err
	}
	values := taxLotURL.Query()
	accountID = values.Get("AccountID")
	symbol = values.Get("Symbol")
	orderNo, err = strconv.ParseInt(values.Get("OrderNo"), 10, 64)
	if err != nil {
		return "", "", "", -1, err
	}
	if orderNo <= 0 {
		return "", "", "", -1, fmt.Errorf("etrade: server order no %d is invalid", orderNo)
	}
	creds := c.creds.Load()
	if _, err := creds.GetAccountIDKey(accountID); err != nil {
		return "", "", "", -1, err
	}
	if _, ok := c.symbolPriceTopicMap.Load(symbol); !ok {
		return "", "", "", -1, fmt.Errorf("etrade: tax lot refers to invalid/unwatched symbol %q", symbol)
	}
	taxLotURL.RawQuery = ""
	return accountID, symbol, taxLotURL.String(), orderNo, nil
}

func (c *Client) GetTaxLotDetail(ctx context.Context, taxLotID gobs.TaxLotID) ([]*positionLot, error) {
	var lots []*positionLot
	accountID, symbol, lotDetailURL, orderNo, err := c.parseTaxLotID(taxLotID)
	if err != nil {
		return nil, err
	}
	detailURL, err := url.Parse(lotDetailURL)
	if err != nil {
		return nil, err
	}
	var posWrapper positionLotsResponseWrapper
	if err := doGetJSON(ctx, c, detailURL.Path, nil, &posWrapper); err != nil {
		return nil, fmt.Errorf("etrade: could not fetch lots from %q: %w", taxLotID, err)
	}
	for _, v := range posWrapper.PositionLotsResponse.PositionLots {
		if v.OrderNo != orderNo {
			continue
		}
		// Attach account id and symbol to each position lot.
		v.LotsDetails = lotDetailURL
		v.AccountID = accountID
		v.Symbol = symbol
		lots = append(lots, v)
	}
	return lots, nil
}

// GetPortfolio returns the asset balances along with the tax lots for the buy
// orders. It is fetched with GET
// /v1/accounts/{accountIdKey}/portfolio?lotsRequired=true.
func (c *Client) GetPortfolio(ctx context.Context, accountID string) ([]*positionLot, error) {
	creds := c.creds.Load()
	accountIDKey, err := creds.GetAccountIDKey(accountID)
	if err != nil {
		return nil, err
	}
	apiPath := "/v1/accounts/" + url.PathEscape(accountIDKey) + "/portfolio"
	params := url.Values{
		"lotsRequired": {"true"},
		"count":        {"5"},
	}
	nextPageNo := ""
	var positions []*portfolioPosition
	for i := 0; i == 0 || nextPageNo != ""; i++ {
		wrapper := new(portfolioResponseWrapper)
		if err := doGetJSON(ctx, c, apiPath, params, wrapper); err != nil {
			return nil, err
		}
		for _, ap := range wrapper.PortfolioResponse.AccountPortfolio {
			positions = append(positions, ap.Position...)
			nextPageNo = ap.NextPageNo
		}
		params["pageNumber"] = []string{nextPageNo}
	}

	var lots []*positionLot
	for _, pos := range positions {
		detailURL, err := url.Parse(pos.LotsDetails)
		if err != nil {
			return nil, err
		}
		var posWrapper positionLotsResponseWrapper
		if err := doGetJSON(ctx, c, detailURL.Path, nil, &posWrapper); err != nil {
			return nil, fmt.Errorf("etrade: could not fetch lots from %q: %w", pos.LotsDetails, err)
		}
		for _, v := range posWrapper.PositionLotsResponse.PositionLots {
			v.LotsDetails = pos.LotsDetails
			v.AccountID = accountID
			v.Symbol = pos.Product.Symbol
			lots = append(lots, v)
		}
	}
	return lots, nil
}

// GetURL performs an OAuth-signed GET against an E*TRADE API URL and returns
// the raw response body. rawURL may be a full https URL (api.etrade.com or
// apisb.etrade.com) or a path beginning with /; query parameters in the URL
// are included in the OAuth signature. The request is always sent to the
// host selected by Options.Sandbox, regardless of any host in rawURL.
func (c *Client) GetURL(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("invalid url %q: %w", rawURL, err)
	}
	if u.Host != "" && u.Host != c.opts.restHostname() {
		return nil, fmt.Errorf("url host %q does not match the configured E*TRADE host %q", u.Host, c.opts.restHostname())
	}
	if !strings.HasPrefix(u.Path, "/") {
		return nil, fmt.Errorf("url must include a path beginning with /")
	}
	var raw json.RawMessage
	if err := doGetJSON(ctx, c, u.Path, u.Query(), &raw); err != nil {
		return nil, err
	}
	return raw, nil
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

	balancesTopic, ok := c.balancesTopicMap.Load(accountID)
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
	if v, ok := c.refreshOrderTopicMap.Load(accountID); ok {
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

	refreshOrderTopic, ok := c.refreshOrderTopicMap.Load(accountID)
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

func (c *Client) goTrackPortfolio(ctx context.Context, accountID string) {
	defer c.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("etrade: CAUGHT PANIC in goTrackPortfolio", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	for ; ctx.Err() == nil; ctxutil.Sleep(ctx, time.Minute) {
		lots, err := c.GetPortfolio(ctx, accountID)
		if err != nil {
			slog.Error("etrade: could not fetch portfolio for an account (will retry)", "account-id", accountID, "err", err)
			continue
		}
		c.updatePortfolio(lots)
	}
}

func (c *Client) updatePortfolio(lots []*positionLot) {
	c.portfolioMu.Lock()
	defer c.portfolioMu.Unlock()

	// Group the incoming lots by their position's lot-detail URL. All lots of a
	// position share one URL (set in GetPortfolio), and lotDetailMap holds the
	// whole set per URL, so we must store them together rather than one-at-a-time
	// (which would wipe siblings). GetPortfolio returns the account's complete
	// portfolio, so replacing each URL's set wholesale also drops lots no longer
	// held.
	byURL := make(map[string][]*positionLot)
	for _, v := range lots {
		if v.OrderNo == 0 {
			continue
		}
		symbolMap, ok := c.portfolioMap.Load(v.AccountID)
		if !ok {
			symbolMap, _ = c.portfolioMap.LoadOrStore(v.AccountID, new(syncmap.Map[string, *syncmap.Map[int64, string]]))
		}
		orderDetailURLMap, ok := symbolMap.Load(v.Symbol)
		if !ok {
			orderDetailURLMap, _ = symbolMap.LoadOrStore(v.Symbol, new(syncmap.Map[int64, string]))
		}
		if lotDetailURL, ok := orderDetailURLMap.Load(v.OrderNo); ok && lotDetailURL != v.LotsDetails {
			slog.Error("etrade: position lot id for old order in portfolio has changed", "account-id", v.AccountID, "symbol", v.Symbol, "order-id", v.OrderNo, "old-lot-details", lotDetailURL, "new-lot-details", v.LotsDetails)
			// FIXME: Should we return ErrNeedsAck ???
		}
		orderDetailURLMap.Store(v.OrderNo, v.LotsDetails)
		byURL[v.LotsDetails] = append(byURL[v.LotsDetails], v)
	}

	// Replace each URL's cached lot set with the freshly-fetched one.
	for lotDetailURL, group := range byURL {
		c.lotDetailMap.Store(lotDetailURL, group)
	}
}

func (c *Client) ResolveTaxLotIDs(ctx context.Context, taxLotIDs []gobs.TaxLotID) ([]*positionLot, error) {
	var missing []int
	// Check for the position lot ids in the cache.
	var lots []*positionLot
	for i, taxLotID := range taxLotIDs {
		_, _, lotDetailURL, orderNo, err := c.parseTaxLotID(taxLotID)
		if err != nil {
			return nil, err
		}
		ps, ok := c.lotDetailMap.Load(lotDetailURL)
		if !ok {
			missing = append(missing, i)
			continue
		}
		for _, p := range ps {
			if p.OrderNo == orderNo {
				lots = append(lots, p)
			}
		}
	}
	// Fetch missing lot ids with an explicit request.
	for _, index := range missing {
		_, _, _, orderNo, err := c.parseTaxLotID(taxLotIDs[index])
		if err != nil {
			return nil, err
		}
		ps, err := c.GetTaxLotDetail(ctx, taxLotIDs[index])
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			if p.OrderNo == orderNo {
				lots = append(lots, p)
			}
		}
	}
	return lots, nil
}

const (
	// resolveLotsMaxAttempts and resolveLotsRetryInterval bound the portfolio
	// refresh retries in ResolveOrderLots. A just-executed order's tax lots may
	// not be posted by E*TRADE immediately, so we refresh and retry for a while
	// before giving up.
	resolveLotsMaxAttempts   = 12
	resolveLotsRetryInterval = 5 * time.Second
)

func (c *Client) ResolveOrderLots(ctx context.Context, accountID, symbol string, serverOrderIDs []int64) ([]*positionLot, error) {
	slog.Info("resolving tax lots for account symbol orders", "account-id", accountID, "symbol", symbol, "orders", serverOrderIDs)
	if len(serverOrderIDs) == 0 {
		return nil, nil
	}

	// Try the in-memory portfolio cache first.
	if lots, err := c.resolveLotsFromCache(accountID, symbol, serverOrderIDs); err == nil {
		slog.Info("etrade: all orders are resolved to tax lots from the cache", "account-id", accountID, "symbol", symbol, "orders", serverOrderIDs, "num-lots", len(lots))
		return lots, nil
	}

	// The tax lots for a just-executed order may not be posted by E*TRADE
	// immediately, so refresh the full portfolio and retry a bounded number of
	// times until *every* requested order resolves to at least one lot.
	var lastErr error
	for attempt := 1; attempt <= resolveLotsMaxAttempts; attempt++ {
		full, err := c.GetPortfolio(ctx, accountID)
		if err != nil {
			return nil, err
		}
		slog.Info("fetched full portfolio for account to resolve orders", "account-id", accountID, "symbol", symbol, "orders", serverOrderIDs, "attempt", attempt)
		c.updatePortfolio(full)

		lots, err := c.resolveLotsFromCache(accountID, symbol, serverOrderIDs)
		if err == nil {
			slog.Info("etrade: all orders are resolved to tax lots after a full portfolio refresh", "account-id", accountID, "symbol", symbol, "orders", serverOrderIDs, "num-lots", len(lots), "attempt", attempt)
			return lots, nil
		}
		lastErr = err

		if attempt < resolveLotsMaxAttempts {
			slog.Warn("orders not yet resolved to tax lots; will refresh and retry", "account-id", accountID, "symbol", symbol, "orders", serverOrderIDs, "attempt", attempt, "retry-after", resolveLotsRetryInterval, "err", lastErr)
			if err := sleep(ctx, resolveLotsRetryInterval); err != nil {
				return nil, err
			}
		}
	}

	slog.Error("could not resolve tax lots for orders after portfolio refreshes", "account-id", accountID, "symbol", symbol, "orders", serverOrderIDs, "attempts", resolveLotsMaxAttempts, "err", lastErr)
	return nil, fmt.Errorf("could not resolve tax lots for orders %v after %d portfolio refreshes: %w", serverOrderIDs, resolveLotsMaxAttempts, lastErr)
}

// resolveLotsFromCache returns the tax lots for every requested order id from
// the in-memory portfolio cache. It returns an error (naming the offending
// order) if any requested order has no lot id, no lot detail, or no matching
// lot entry — so a nil error guarantees that *all* serverOrderIDs resolved to
// at least one lot.
func (c *Client) resolveLotsFromCache(accountID, symbol string, serverOrderIDs []int64) ([]*positionLot, error) {
	symbolMap, ok := c.portfolioMap.Load(accountID)
	if !ok {
		return nil, fmt.Errorf("portfolio has no holdings for account %s", accountID)
	}
	orderDetailURLMap, ok := symbolMap.Load(symbol)
	if !ok {
		return nil, fmt.Errorf("portfolio has no lots for symbol %s", symbol)
	}
	lots := make([]*positionLot, 0, len(serverOrderIDs))
	for _, oid := range serverOrderIDs {
		lotDetailURL, ok := orderDetailURLMap.Load(oid)
		if !ok {
			return nil, fmt.Errorf("portfolio has no tax lot id for order %d", oid)
		}
		details, ok := c.lotDetailMap.Load(lotDetailURL)
		if !ok {
			return nil, fmt.Errorf("portfolio has no tax lot detail for order %d", oid)
		}
		found := false
		for _, v := range details {
			if v.OrderNo == oid {
				lots = append(lots, v)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("tax lot detail has no entry for order %d", oid)
		}
	}
	return lots, nil
}

func (c *Client) goLogin(ctx context.Context) {
	defer c.wg.Done()

	failed := false
	for ctxutil.Sleep(ctx, time.Hour); ctx.Err() == nil; ctxutil.Sleep(ctx, time.Hour) {
		if err := c.relogin(ctx); err != nil {
			slog.Error("etrade: could not complete periodic login (will retry)", "err", err)
			if c.opts.Messenger != nil {
				failed = true
				c.opts.Messenger.SendMessage(ctx, time.Now(), "E*TRADE client has failed to authenticate automatically: %v", err)
			}
			continue
		}
		if failed {
			failed = false
			c.opts.Messenger.SendMessage(ctx, time.Now(), "E*TRADE client has authenticated successfully")
		}
	}
}

func (c *Client) relogin(ctx context.Context) error {
	c.reloginMutex.Lock()
	defer c.reloginMutex.Unlock()

	if c.opts.DisplayRunner == nil {
		return fmt.Errorf("etrade: a X11 display server is required for automated logins")
	}

	var environ []string
	environ = append(environ, "HOME="+os.Getenv("HOME"))
	environ = append(environ, "PATH="+os.Getenv("PATH"))

	credsPath := filepath.Join(c.opts.DataDir, "etrade", "creds.json")
	binPath := filepath.Join(c.opts.DataDir, "tradebot")
	cmdline := []string{
		binPath, "setup", "etrade",
		fmt.Sprintf("--data-dir=%s", c.opts.DataDir),
		fmt.Sprintf("--extra-output-path=%s", credsPath),
		"--unattended=true",
	}

	slog.Info("starting etrade autologin", "environ", environ)
	cmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...)
	cmd.Env = environ
	if err := c.opts.DisplayRunner.RunWithDisplay(cmd); err != nil {
		slog.Error("etrade: could not perform unattended login", "err", err)
		return err
	}
	data, err := os.ReadFile(credsPath)
	if err != nil {
		slog.Error("etrade: could not read updated credentials", "err", err)
		return err
	}
	creds := new(Credentials)
	if err := json.Unmarshal(data, creds); err != nil {
		slog.Error("etrade: could not unmarshal updated credentials file", "file", credsPath, "err", err)
		return err
	}
	// Validate before swapping so a malformed/partial creds file from the login
	// subprocess leaves the last-good credentials in place rather than being
	// accepted wholesale. Store a clone so the swapped-in value is isolated.
	if err := creds.Check(); err != nil {
		slog.Error("etrade: reloaded credentials failed validation", "file", credsPath, "err", err)
		return err
	}
	c.creds.Store(creds.Clone())
	slog.Info("unattended login to E*TRADE is successful")

	// Pick up any account added since startup (topics + polling goroutines) so
	// it becomes usable without a daemon restart. Only after New's initial setup
	// has completed; New handles the initial account set itself.
	if c.initialized.Load() {
		c.ensureNewAccounts()
	}
	return nil
}

// ensureNewAccounts creates the per-account topics and (in daemon mode) spawns
// the per-account polling goroutines for any account in the current credentials
// that has not been initialized yet. Called after a credential reload so that
// accounts added since startup become usable without a restart. Idempotent and
// safe to call concurrently.
func (c *Client) ensureNewAccounts() {
	c.accountMu.Lock()
	defer c.accountMu.Unlock()
	for _, a := range c.creds.Load().ActiveAccounts() {
		if c.accountSet[a.AccountID] {
			continue
		}
		c.accountSet[a.AccountID] = true
		c.balancesTopicMap.Store(a.AccountID, topic.New[*internal.Balance]())
		c.refreshOrderTopicMap.Store(a.AccountID, topic.New[int64]())
		slog.Info("etrade: initializing account added since startup", "account-id", a.AccountID)
		if !c.opts.cmdlineToolMode {
			c.wg.Add(3)
			go c.goPollBalances(c.lifeCtx, a.AccountID)
			go c.goRefreshOrders(c.lifeCtx, a.AccountID)
			go c.goTrackPortfolio(c.lifeCtx, a.AccountID)
		}
	}
}
