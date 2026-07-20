package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

type HTTPSourceClient struct {
	client *http.Client
	now    func() time.Time
}

func NewHTTPSourceClient() *HTTPSourceClient {
	return &HTTPSourceClient{
		client: http.DefaultClient,
		now:    time.Now,
	}
}

func (c *HTTPSourceClient) Fetch(
	ctx context.Context,
	source SourceConfig,
	task *oraclev1.OracleTask,
	timeout time.Duration,
) (*oraclev1.OracleSample, error) {
	if strings.TrimSpace(source.Timeout) != "" {
		sourceTimeout, err := parseDuration(source.Timeout)
		if err != nil {
			return nil, err
		}
		timeout = sourceTimeout
	}

	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, renderSourceURL(source.URL, task.GetSymbol()), nil)
	if err != nil {
		return nil, err
	}
	for key, value := range source.Headers {
		request.Header.Set(key, value)
	}

	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}

	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()

	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}

	rawValue, err := ExtractJSONPath(payload, source.ResponsePath)
	if err != nil {
		return nil, err
	}
	value, err := sampleValueString(rawValue)
	if err != nil {
		return nil, err
	}
	valueType, err := source.ProtoValueType()
	if err != nil {
		return nil, err
	}

	return &oraclev1.OracleSample{
		Source:         strings.TrimSpace(source.Name),
		ValueType:      valueType,
		Value:          value,
		SampleTimeUnix: c.now().Unix(),
	}, nil
}

func renderSourceURL(template string, symbol string) string {
	return strings.ReplaceAll(template, "{symbol}", url.QueryEscape(symbol))
}

func sampleValueString(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		result := strings.TrimSpace(typed)
		if result == "" {
			return "", fmt.Errorf("empty string value")
		}
		if _, err := sdkmath.LegacyNewDecFromStr(result); err != nil {
			return "", fmt.Errorf("invalid numeric value: %w", err)
		}
		return result, nil
	case json.Number:
		if _, err := sdkmath.LegacyNewDecFromStr(typed.String()); err != nil {
			return "", fmt.Errorf("invalid numeric value: %w", err)
		}
		return typed.String(), nil
	case float64:
		result := strconv.FormatFloat(typed, 'f', -1, 64)
		if _, err := sdkmath.LegacyNewDecFromStr(result); err != nil {
			return "", fmt.Errorf("invalid numeric value: %w", err)
		}
		return result, nil
	default:
		return "", fmt.Errorf("unsupported non-numeric JSON value type %T", value)
	}
}
