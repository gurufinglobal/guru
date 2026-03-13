package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

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
	return []int32{1,2,3}
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
		Data map[string][]struct {
			Quote map[string]struct {
				Price json.Number `json:"price"`
			} `json:"quote"`
		} `json:"data"`
	}

	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err = dec.Decode(&payload); err != nil {
		return "", err
	}

	if payload.Status.ErrorCode != 0 {
		return "", fmt.Errorf("cmc api error: code=%d message=%q", payload.Status.ErrorCode, payload.Status.ErrorMessage)
	}

	conversions, ok := payload.Data[baseSymbol]
	if !ok || len(conversions) == 0 {
		return "", fmt.Errorf("conversion data not found for symbol %s", baseSymbol)
	}

	quote, ok := conversions[0].Quote[convertSymbol]
	if !ok {
		return "", fmt.Errorf("quote not found for convert symbol %s", convertSymbol)
	}

	price := quote.Price.String()
	if price == "" {
		return "", fmt.Errorf("price not found")
	}

	normalized, err := normalizeChainDecimal(price)
	if err != nil {
		return "", fmt.Errorf("invalid price %q: %w", price, err)
	}

	return normalized, nil
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
