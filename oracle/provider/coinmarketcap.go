package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type CMCProvider struct {
    client  *http.Client
    baseURL string
    apiKey  string
}

func NewCMCProvider(client *http.Client) *CMCProvider {
    return &CMCProvider{
        client:  client,
        baseURL: "https://pro-api.coinmarketcap.com/v2/tools/price-conversion",
        apiKey:  "05ef7493af324f38bfdd8c94bda1b889",
    }
}

func (p *CMCProvider) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	p.client = client
}

func (p *CMCProvider) ID() string {
    return "coinmarketcap"
}

func (p *CMCProvider) Categories() []int32 {
	// c := oracletypes.Category_value[oracletypes.Category_CATEGORY_OPERATION.String()]

	return []int32{2,3}
}

func (p *CMCProvider) Fetch(ctx context.Context, symbol string) (string, error) {
    // 예: "USD/KRW"
    parts := strings.Split(symbol, "/")
    if len(parts) != 2 {
        return "", fmt.Errorf("invalid symbol: %s", symbol)
    }

    url := fmt.Sprintf("%s?amount=1&symbol=%s&convert=%s", p.baseURL, parts[0], parts[1])
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return "", err
    }

    req.Header.Set("X-CMC_PRO_API_KEY", p.apiKey)
    req.Header.Set("Accept", "application/json")

    resp, err := p.client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var payload struct {
        Data []struct {
            Quote map[string]struct {
                Price float64 `json:"price"`
            } `json:"quote"`
        } `json:"data"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
        return "", err
    }

    if len(payload.Data) == 0 {
        return "", fmt.Errorf("no data returned from CMC")
    }

    price := payload.Data[0].Quote[parts[1]].Price
    amountStr := fmt.Sprintf("%f", price)

    return amountStr, nil
}
