package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
)

type CoinbaseProvider struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

func NewCoinbaseProvider(client *http.Client) *CoinbaseProvider {
	return &CoinbaseProvider{client: client, baseURL: "https://api.coinbase.com/v2/prices/", apiKey: ""}
}

func (p *CoinbaseProvider) ID() string {
	return "coinbase"
}

func (p *CoinbaseProvider) Categories() []int32 {
	// 2: currency
	return []int32{2}
}

func (p *CoinbaseProvider) Fetch(ctx context.Context, symbol string) (*big.Float, error) {
	pair := strings.ReplaceAll(symbol, "/", "-")
	url := fmt.Sprintf("%s%s/spot", p.baseURL, pair)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	if p.apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Data struct {
			Amount string `json:"amount"`
		} `json:"data"`
	}
	if err = json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	if payload.Data.Amount == "" {
		return nil, fmt.Errorf("amount not found")
	}

	val, ok := new(big.Float).SetString(payload.Data.Amount)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", payload.Data.Amount)
	}

	return val, nil
}
