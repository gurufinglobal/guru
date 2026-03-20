package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

type CoinbaseProvider struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

func NewCoinbaseProvider(client *http.Client) *CoinbaseProvider {
	return &CoinbaseProvider{client: client, baseURL: "https://api.coinbase.com/v2/prices/", apiKey: ""}
}

// SetHTTPClient replaces the underlying HTTP client. Intended for daemon restarts.
func (p *CoinbaseProvider) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	p.client = client
}

func (p *CoinbaseProvider) ID() string {
	return "coinbase"
}

func (p *CoinbaseProvider) Categories() []int32 {
	// c := oracletypes.Category_value[oracletypes.Category_CATEGORY_OPERATION.String()]

	return []int32{1, 2, 3}
}

func (p *CoinbaseProvider) Fetch(ctx context.Context, symbol string) (string, error) {
	if symbol == "" {
		return "", fmt.Errorf("symbol is empty")
	}

	pair := strings.ReplaceAll(symbol, "/", "-")
	url := fmt.Sprintf("%s%s/spot", p.baseURL, pair)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	if p.apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req) //nolint:gosec // request target is fixed Coinbase API baseURL
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := readBodySnippet(resp.Body, 2048)
		return "", fmt.Errorf("coinbase unexpected status: %d, pair=%s, body=%q", resp.StatusCode, pair, snippet)
	}

	var payload struct {
		Data struct {
			Amount string `json:"amount"`
		} `json:"data"`
	}
	dec := json.NewDecoder(resp.Body)
	if err = dec.Decode(&payload); err != nil {
		return "", err
	}

	if payload.Data.Amount == "" {
		return "", fmt.Errorf("amount not found")
	}

	// Must be accepted by oracletypes.ParseOracleDecimal (matches chain validation).
	if !isChainDecimal(payload.Data.Amount) {
		return "", fmt.Errorf("invalid amount: %s", payload.Data.Amount)
	}

	return payload.Data.Amount, nil
}

func isChainDecimal(s string) bool {
	_, err := oracletypes.ParseOracleDecimal(s)
	return err == nil
}

func readBodySnippet(r io.Reader, limit int64) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return "", err
	}
	// ensure printable-ish output; avoid broken utf-8 logs
	if !utf8.Valid(b) {
		return string([]rune(string(b))), nil
	}
	return string(b), nil
}
