package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type CDNProvider struct {
	client  *http.Client
	baseURL string
}

func NewCDNProvider(client *http.Client) *CDNProvider {
	return &CDNProvider{
		client:  client,
		baseURL: "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies",
	}
}

func (p *CDNProvider) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	p.client = client
}

func (p *CDNProvider) ID() string {
	return "cdn"
}

func (p *CDNProvider) Categories() []int32 {
	return []int32{3}
}

func (p *CDNProvider) Fetch(ctx context.Context, symbol string) (string, error) {
	baseSymbol, convertSymbol, err := splitProviderSymbol(symbol)
	if err != nil {
		return "", err
	}

	baseSymbol = strings.ToLower(baseSymbol)
	convertSymbol = strings.ToLower(convertSymbol)

	reqURL := fmt.Sprintf("%s/%s.json", p.baseURL, baseSymbol)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req) //nolint:gosec // request target is fixed jsDelivr CDN baseURL
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := readBodySnippet(resp.Body, 2048)
		return "", fmt.Errorf("cdn unexpected status: %d, base=%s, body=%q", resp.StatusCode, baseSymbol, snippet)
	}

	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()

	payload := make(map[string]json.RawMessage)
	if err = dec.Decode(&payload); err != nil {
		return "", err
	}

	ratesRaw, ok := payload[baseSymbol]
	if !ok {
		return "", fmt.Errorf("rates not found for base %s", baseSymbol)
	}

	rates := make(map[string]json.RawMessage)
	if err = json.Unmarshal(ratesRaw, &rates); err != nil {
		return "", fmt.Errorf("invalid rates payload for base %s: %w", baseSymbol, err)
	}

	valueRaw, ok := rates[convertSymbol]
	if !ok {
		return "", fmt.Errorf("rate not found for pair %s/%s", strings.ToUpper(baseSymbol), strings.ToUpper(convertSymbol))
	}

	normalized, err := parseJSONDecimal(valueRaw)
	if err != nil {
		return "", fmt.Errorf("invalid rate for pair %s/%s: %w", strings.ToUpper(baseSymbol), strings.ToUpper(convertSymbol), err)
	}

	return normalized, nil
}

func parseJSONDecimal(raw json.RawMessage) (string, error) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return normalizeChainDecimal(asString)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var asNumber json.Number
	if err := dec.Decode(&asNumber); err != nil {
		return "", fmt.Errorf("value is neither number nor string")
	}

	return normalizeChainDecimal(asNumber.String())
}
