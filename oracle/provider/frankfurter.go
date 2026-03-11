package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type FrankfurterProvider struct {
    client  *http.Client
    baseURL string
}

func NewFrankfurterProvider(client *http.Client) *FrankfurterProvider {
    return &FrankfurterProvider{
        client:  client,
        baseURL: "https://api.frankfurter.app/latest",
    }
}

func (p *FrankfurterProvider) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	p.client = client
}

func (p *FrankfurterProvider) Categories() []int32 {
	// c := oracletypes.Category_value[oracletypes.Category_CATEGORY_OPERATION.String()]

	return []int32{3}
}

func (p *FrankfurterProvider) ID() string {
    return "frankfurter"
}

func (p *FrankfurterProvider) Fetch(ctx context.Context, symbol string) (string, error) {
    if symbol == "" {
        return "", fmt.Errorf("symbol is empty")
    }

    // 예: "USD/KRW" -> from=USD, to=KRW
    parts := strings.Split(symbol, "/")
    if len(parts) != 2 {
        return "", fmt.Errorf("invalid symbol format: %s", symbol)
    }
    from, to := strings.ToUpper(parts[0]), strings.ToUpper(parts[1])

    url := fmt.Sprintf("%s?from=%s&to=%s", p.baseURL, from, to)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return "", err
    }

    resp, err := p.client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("frankfurter error status: %d", resp.StatusCode)
    }

    var payload struct {
        Rates map[string]float64 `json:"rates"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
        return "", err
    }

    val, ok := payload.Rates[to]
    if !ok {
        return "", fmt.Errorf("rate for %s not found", to)
    }

    amountStr := fmt.Sprintf("%f", val)
    if !isChainDecimal(amountStr) {
        return "", fmt.Errorf("invalid decimal: %s", amountStr)
    }

    return amountStr, nil
}