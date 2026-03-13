package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

type CoinMarketCapProvider struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

func NewCoinMarketCapProvider(client *http.Client, apiKey string) *CoinMarketCapProvider {
	return &CoinMarketCapProvider{
		client:  client,
		baseURL: "https://pro-api.coinmarketcap.com/v2/tools/price-conversion",
		apiKey:  apiKey,
	}
}

func (p *CoinMarketCapProvider) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	p.client = client
}

func (p *CoinMarketCapProvider) ID() string {
	return "cmc"
}

func (p *CoinMarketCapProvider) Categories() []int32 {
	return []int32{1, 2, 3}
}

func (p *CoinMarketCapProvider) Fetch(ctx context.Context, symbol string) (string, error) {
	baseSymbol, convertSymbol, err := splitProviderSymbol(symbol)
	if err != nil {
		return "", err
	}

	params := url.Values{}
	params.Set("amount", "1")
	params.Set("symbol", baseSymbol)
	params.Set("convert", convertSymbol)

	reqURL := p.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-CMC_PRO_API_KEY", p.apiKey)

	resp, err := p.client.Do(req) //nolint:gosec // request target is fixed CMC API baseURL
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := readBodySnippet(resp.Body, 2048)
		return "", fmt.Errorf("cmc unexpected status: %d, symbol=%s, convert=%s, body=%q", resp.StatusCode, baseSymbol, convertSymbol, snippet)
	}

	var payload struct {
		Status struct {
			ErrorCode    int    `json:"error_code"`
			ErrorMessage string `json:"error_message"`
		} `json:"status"`
		Data json.RawMessage `json:"data"`
	}

	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err = dec.Decode(&payload); err != nil {
		return "", err
	}

	if payload.Status.ErrorCode != 0 {
		return "", fmt.Errorf("cmc api error: code=%d message=%q", payload.Status.ErrorCode, payload.Status.ErrorMessage)
	}

	normalized, err := extractCMCPrice(payload.Data, baseSymbol, convertSymbol)
	if err != nil {
		return "", err
	}

	return normalized, nil
}

type cmcConversion struct {
	Symbol      string              `json:"symbol"`
	LastUpdated string              `json:"last_updated"`
	Quote       map[string]cmcQuote `json:"quote"`
}

type cmcQuote struct {
	Price       json.RawMessage `json:"price"`
	LastUpdated string          `json:"last_updated"`
}

func extractCMCPrice(dataRaw json.RawMessage, baseSymbol, convertSymbol string) (string, error) {
	conversions, err := parseCMCConversions(dataRaw, baseSymbol)
	if err != nil {
		return "", err
	}

	return pickCMCConversionPrice(conversions, baseSymbol, convertSymbol)
}

func parseCMCConversions(dataRaw json.RawMessage, baseSymbol string) ([]cmcConversion, error) {
	// Newer CMC responses encode data as an array.
	var asArray []cmcConversion
	if err := json.Unmarshal(dataRaw, &asArray); err == nil {
		if len(asArray) == 0 {
			return nil, fmt.Errorf("conversion data not found for symbol %s", baseSymbol)
		}
		return asArray, nil
	}

	// Backward-compatible path for map-based responses in docs/examples.
	var asMap map[string][]cmcConversion
	if err := json.Unmarshal(dataRaw, &asMap); err == nil {
		conversions := asMap[baseSymbol]
		if len(conversions) == 0 {
			return nil, fmt.Errorf("conversion data not found for symbol %s", baseSymbol)
		}
		return conversions, nil
	}

	return nil, fmt.Errorf("unsupported cmc data format")
}

func pickCMCConversionPrice(conversions []cmcConversion, baseSymbol, convertSymbol string) (string, error) {
	bestPrice := ""
	var bestUpdatedAt time.Time
	found := false

	for _, conversion := range conversions {
		if conversion.Symbol != "" && !strings.EqualFold(conversion.Symbol, baseSymbol) {
			continue
		}

		quote, ok := conversion.Quote[convertSymbol]
		if !ok {
			continue
		}

		normalized, err := parseJSONDecimal(quote.Price)
		if err != nil {
			continue
		}

		updatedAt := cmcCandidateUpdatedAt(quote.LastUpdated, conversion.LastUpdated)
		if !found || updatedAt.After(bestUpdatedAt) {
			bestPrice = normalized
			bestUpdatedAt = updatedAt
			found = true
		}
	}

	if found {
		return bestPrice, nil
	}

	return "", fmt.Errorf("quote not found for convert symbol %s", convertSymbol)
}

func cmcCandidateUpdatedAt(quoteUpdatedAtRaw, conversionUpdatedAtRaw string) time.Time {
	if t, ok := parseCMCTimestamp(quoteUpdatedAtRaw); ok {
		return t
	}
	if t, ok := parseCMCTimestamp(conversionUpdatedAtRaw); ok {
		return t
	}
	return time.Time{}
}

func parseCMCTimestamp(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}

	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return ts, true
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts, true
	}

	return time.Time{}, false
}

func splitProviderSymbol(symbol string) (string, string, error) {
	raw := strings.TrimSpace(symbol)
	if raw == "" {
		return "", "", fmt.Errorf("symbol is empty")
	}

	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("symbol must be in BASE/QUOTE format")
	}

	base := strings.ToUpper(strings.TrimSpace(parts[0]))
	quote := strings.ToUpper(strings.TrimSpace(parts[1]))
	if base == "" || quote == "" {
		return "", "", fmt.Errorf("symbol must be in BASE/QUOTE format")
	}

	return base, quote, nil
}

func normalizeChainDecimal(raw string) (string, error) {
	dec, err := oracletypes.ParseOracleDecimal(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	return oracletypes.FormatOracleDecimal(dec), nil
}
