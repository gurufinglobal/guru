package worker

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/gurufinglobal/guru/v2/oralce/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ClientTestSuite struct {
	suite.Suite

	client *httpClient
}

func (c *ClientTestSuite) SetupSuite() {
	c.T().Log("setting up client test suite")

	// Initialize test configuration
	config.TestConfig()

	c.client = newHTTPClient(log.NewTestLogger(c.T()))
}

func (c *ClientTestSuite) TearDownSuite() {
	c.T().Log("tearing down client test suite")
}

func TestClientTestSuite(t *testing.T) {
	suite.Run(t, new(ClientTestSuite))
}

func (c *ClientTestSuite) TestNewHTTPClient() {
	c.T().Log("testing new http client")

	// Valid logger -> should create client
	{
		logger := log.NewTestLogger(c.T())
		client := newHTTPClient(logger)
		assert.NotNil(c.T(), client)
		assert.NotNil(c.T(), client.logger)
		assert.NotNil(c.T(), client.client)
		assert.Equal(c.T(), 30*time.Second, client.client.Timeout)
	}
}

func (c *ClientTestSuite) TestFetchRawData_Success() {
	c.T().Log("testing fetch raw data - success")

	// 1) Successful response with valid JSON
	{
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(c.T(), "Guru-V2-Oracle/1.0", r.Header.Get("User-Agent"))
			assert.Equal(c.T(), "application/json", r.Header.Get("Accept"))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"rates":{"KRW":1388.95,"USD":1}}`))
		}))
		defer server.Close()

		data, err := c.client.fetchRawData(server.URL)
		assert.NoError(c.T(), err)
		assert.NotNil(c.T(), data)
		assert.Contains(c.T(), string(data), "KRW")
		assert.Contains(c.T(), string(data), "1388.95")
	}

	// 2) Successful response with real exchange rate API format
	{
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"provider":"https://www.exchangerate-api.com","base":"USD","date":"2025-01-01","rates":{"USD":1,"KRW":1388.95,"EUR":0.856}}`))
		}))
		defer server.Close()

		data, err := c.client.fetchRawData(server.URL)
		assert.NoError(c.T(), err)
		assert.NotNil(c.T(), data)
		assert.Contains(c.T(), string(data), "rates")
		assert.Contains(c.T(), string(data), "KRW")
	}
}

func (c *ClientTestSuite) TestFetchRawData_HTTPErrors() {
	c.T().Log("testing fetch raw data - http errors")

	// 1) 404 Not Found -> should return error immediately
	{
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Not Found"))
		}))
		defer server.Close()

		data, err := c.client.fetchRawData(server.URL)
		assert.Error(c.T(), err)
		assert.Nil(c.T(), data)
		assert.Contains(c.T(), err.Error(), "HTTP 404")
	}

	// 2) 400 Bad Request -> should return error immediately
	{
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Bad Request"))
		}))
		defer server.Close()

		data, err := c.client.fetchRawData(server.URL)
		assert.Error(c.T(), err)
		assert.Nil(c.T(), data)
		assert.Contains(c.T(), err.Error(), "HTTP 400")
	}

	// 3) 500 Internal Server Error -> should retry and then fail
	{
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal Server Error"))
		}))
		defer server.Close()

		data, err := c.client.fetchRawData(server.URL)
		assert.Error(c.T(), err)
		assert.Nil(c.T(), data)
		assert.Contains(c.T(), err.Error(), "failed to fetch raw data")
	}

	// 4) 429 Too Many Requests -> should retry
	{
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("Too Many Requests"))
		}))
		defer server.Close()

		data, err := c.client.fetchRawData(server.URL)
		assert.Error(c.T(), err)
		assert.Nil(c.T(), data)
		assert.Contains(c.T(), err.Error(), "failed to fetch raw data")
	}
}

func (c *ClientTestSuite) TestFetchRawData_InvalidURL() {
	c.T().Log("testing fetch raw data - invalid url")

	// 1) Invalid URL -> should return error
	{
		data, err := c.client.fetchRawData("invalid-url")
		assert.Error(c.T(), err)
		assert.Nil(c.T(), data)
	}

	// 2) Empty URL -> should return error
	{
		data, err := c.client.fetchRawData("")
		assert.Error(c.T(), err)
		assert.Nil(c.T(), data)
	}
}

func (c *ClientTestSuite) TestParseRawData_ValidJSON() {
	c.T().Log("testing parse raw data - valid json")

	// 1) Valid JSON object -> should return map
	{
		jsonData := []byte(`{"rates":{"KRW":1388.95,"USD":1},"base":"USD"}`)
		result, err := c.client.parseRawData(jsonData)
		assert.NoError(c.T(), err)
		assert.NotNil(c.T(), result)
		assert.Equal(c.T(), "USD", result["base"])
		rates, ok := result["rates"].(map[string]any)
		assert.True(c.T(), ok)
		assert.Equal(c.T(), 1388.95, rates["KRW"])
	}

	// 2) Valid JSON array with object -> should return first element
	{
		jsonData := []byte(`[{"id":1,"name":"test"},{"id":2,"name":"test2"}]`)
		result, err := c.client.parseRawData(jsonData)
		assert.NoError(c.T(), err)
		assert.NotNil(c.T(), result)
		assert.Equal(c.T(), float64(1), result["id"])
		assert.Equal(c.T(), "test", result["name"])
	}

	// 3) Nested JSON object -> should return map
	{
		jsonData := []byte(`{"data":{"currency":{"from":"USD","to":"KRW","rate":1388.95}}}`)
		result, err := c.client.parseRawData(jsonData)
		assert.NoError(c.T(), err)
		assert.NotNil(c.T(), result)
		data, ok := result["data"].(map[string]any)
		assert.True(c.T(), ok)
		currency, ok := data["currency"].(map[string]any)
		assert.True(c.T(), ok)
		assert.Equal(c.T(), "USD", currency["from"])
		assert.Equal(c.T(), 1388.95, currency["rate"])
	}
}

func (c *ClientTestSuite) TestParseRawData_InvalidJSON() {
	c.T().Log("testing parse raw data - invalid json")

	// 1) Invalid JSON syntax -> should return error
	{
		jsonData := []byte(`{"rates":{"KRW":1388.95,"USD":1}`) // missing closing brace
		result, err := c.client.parseRawData(jsonData)
		assert.Error(c.T(), err)
		assert.Nil(c.T(), result)
		assert.Contains(c.T(), err.Error(), "failed to parse JSON")
	}

	// 2) Empty JSON array -> should return error
	{
		jsonData := []byte(`[]`)
		result, err := c.client.parseRawData(jsonData)
		assert.Error(c.T(), err)
		assert.Nil(c.T(), result)
		assert.Contains(c.T(), err.Error(), "empty JSON array")
	}

	// 3) JSON array with non-object first element -> should return error
	{
		jsonData := []byte(`["string", "another"]`)
		result, err := c.client.parseRawData(jsonData)
		assert.Error(c.T(), err)
		assert.Nil(c.T(), result)
		assert.Contains(c.T(), err.Error(), "first array element is not a JSON object")
	}

	// 4) JSON primitive (string) -> should return error
	{
		jsonData := []byte(`"just a string"`)
		result, err := c.client.parseRawData(jsonData)
		assert.Error(c.T(), err)
		assert.Nil(c.T(), result)
		assert.Contains(c.T(), err.Error(), "JSON must be object or array")
	}

	// 5) JSON primitive (number) -> should return error
	{
		jsonData := []byte(`42`)
		result, err := c.client.parseRawData(jsonData)
		assert.Error(c.T(), err)
		assert.Nil(c.T(), result)
		assert.Contains(c.T(), err.Error(), "JSON must be object or array")
	}

	// 6) Empty input -> should return error
	{
		jsonData := []byte(``)
		result, err := c.client.parseRawData(jsonData)
		assert.Error(c.T(), err)
		assert.Nil(c.T(), result)
	}
}

func (c *ClientTestSuite) TestExtractDataByPath_ValidPaths() {
	c.T().Log("testing extract data by path - valid paths")

	data := map[string]any{
		"rates": map[string]any{
			"KRW": 1388.95,
			"USD": 1.0,
			"EUR": 0.856,
		},
		"base": "USD",
		"array": []any{
			map[string]any{"value": "first"},
			map[string]any{"value": "second"},
			"string_element",
		},
		"nested": map[string]any{
			"deep": map[string]any{
				"level": "test",
			},
		},
	}

	// 1) Simple key path -> should return value
	{
		result, err := c.client.extractDataByPath(data, "base")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), "USD", result)
	}

	// 2) Nested object path (rates.KRW) -> should return value
	{
		result, err := c.client.extractDataByPath(data, "rates.KRW")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), "1388.95", result)
	}

	// 3) Nested object path (rates.USD) -> should return value
	{
		result, err := c.client.extractDataByPath(data, "rates.USD")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), "1", result)
	}

	// 4) Deep nested path -> should return value
	{
		result, err := c.client.extractDataByPath(data, "nested.deep.level")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), "test", result)
	}

	// 5) Array index access -> should return value
	{
		result, err := c.client.extractDataByPath(data, "array.0.value")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), "first", result)
	}

	// 6) Array index access for string element -> should return value
	{
		result, err := c.client.extractDataByPath(data, "array.2")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), "string_element", result)
	}
}

func (c *ClientTestSuite) TestExtractDataByPath_InvalidPaths() {
	c.T().Log("testing extract data by path - invalid paths")

	data := map[string]any{
		"rates": map[string]any{
			"KRW": 1388.95,
			"USD": 1.0,
		},
		"array": []any{
			map[string]any{"value": "first"},
		},
	}

	// 1) Empty path -> should return error
	{
		result, err := c.client.extractDataByPath(data, "")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), "", result)
		assert.Contains(c.T(), err.Error(), "path cannot be empty")
	}

	// 2) Non-existent key -> should return error
	{
		result, err := c.client.extractDataByPath(data, "nonexistent")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), "", result)
		assert.Contains(c.T(), err.Error(), "key 'nonexistent' not found")
	}

	// 3) Non-existent nested key -> should return error
	{
		result, err := c.client.extractDataByPath(data, "rates.JPY")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), "", result)
		assert.Contains(c.T(), err.Error(), "key 'JPY' not found")
	}

	// 4) Array index out of bounds -> should return error
	{
		result, err := c.client.extractDataByPath(data, "array.5")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), "", result)
		assert.Contains(c.T(), err.Error(), "array index 5 out of bounds")
	}

	// 5) Invalid array index (negative) -> should return error
	{
		result, err := c.client.extractDataByPath(data, "array.-1")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), "", result)
		assert.Contains(c.T(), err.Error(), "invalid array index")
	}

	// 6) Invalid array index (non-numeric) -> should return error
	{
		result, err := c.client.extractDataByPath(data, "array.abc")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), "", result)
		assert.Contains(c.T(), err.Error(), "invalid array index")
	}

	// 7) Trying to traverse non-map/non-array -> should return error
	{
		result, err := c.client.extractDataByPath(data, "rates.KRW.something")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), "", result)
		assert.Contains(c.T(), err.Error(), "cannot traverse")
	}
}

func (c *ClientTestSuite) TestParseArrayIndex_ValidIndices() {
	c.T().Log("testing parse array index - valid indices")

	// 1) Zero index -> should return 0
	{
		index, err := parseArrayIndex("0")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), 0, index)
	}

	// 2) Positive index -> should return value
	{
		index, err := parseArrayIndex("5")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), 5, index)
	}

	// 3) Large index -> should return value
	{
		index, err := parseArrayIndex("999")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), 999, index)
	}
}

func (c *ClientTestSuite) TestParseArrayIndex_InvalidIndices() {
	c.T().Log("testing parse array index - invalid indices")

	// 1) Empty string -> should return error
	{
		index, err := parseArrayIndex("")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), -1, index)
		assert.Contains(c.T(), err.Error(), "array index cannot be empty")
	}

	// 2) Negative index -> should return error
	{
		index, err := parseArrayIndex("-1")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), -1, index)
		assert.Contains(c.T(), err.Error(), "negative array index not allowed")
	}

	// 3) Non-numeric string -> should return error
	{
		index, err := parseArrayIndex("abc")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), -1, index)
		assert.Contains(c.T(), err.Error(), "invalid array index")
	}

	// 4) Mixed alphanumeric -> should return error
	{
		index, err := parseArrayIndex("1abc")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), -1, index)
		assert.Contains(c.T(), err.Error(), "invalid array index")
	}

	// 5) Float number -> should return error
	{
		index, err := parseArrayIndex("1.5")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), -1, index)
		assert.Contains(c.T(), err.Error(), "invalid array index")
	}

	// 6) Spaces around number -> should return error
	{
		index, err := parseArrayIndex(" 1 ")
		assert.Error(c.T(), err)
		assert.Equal(c.T(), -1, index)
		assert.Contains(c.T(), err.Error(), "invalid array index")
	}
}

func (c *ClientTestSuite) TestRealWorldScenario_ExchangeRateAPI() {
	c.T().Log("testing real world scenario - exchange rate api")

	// Mock the exchange rate API response format
	exchangeRateJSON := `{
		"provider": "https://www.exchangerate-api.com",
		"base": "USD",
		"date": "2025-01-01",
		"rates": {
			"USD": 1,
			"KRW": 1388.95,
			"EUR": 0.856,
			"JPY": 147.15
		}
	}`

	// 1) Parse the JSON -> should succeed
	{
		result, err := c.client.parseRawData([]byte(exchangeRateJSON))
		assert.NoError(c.T(), err)
		assert.NotNil(c.T(), result)
		assert.Equal(c.T(), "USD", result["base"])
	}

	// 2) Extract KRW rate using rates.KRW path -> should succeed
	{
		result, err := c.client.parseRawData([]byte(exchangeRateJSON))
		assert.NoError(c.T(), err)

		krwRate, err := c.client.extractDataByPath(result, "rates.KRW")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), "1388.95", krwRate)
	}

	// 3) Extract base currency -> should succeed
	{
		result, err := c.client.parseRawData([]byte(exchangeRateJSON))
		assert.NoError(c.T(), err)

		baseCurrency, err := c.client.extractDataByPath(result, "base")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), "USD", baseCurrency)
	}

	// 4) Extract non-existent currency -> should fail
	{
		result, err := c.client.parseRawData([]byte(exchangeRateJSON))
		assert.NoError(c.T(), err)

		_, err = c.client.extractDataByPath(result, "rates.NON_EXISTENT")
		assert.Error(c.T(), err)
	}
}

func (c *ClientTestSuite) TestIntegration_FetchParseExtract() {
	c.T().Log("testing integration - fetch parse extract")

	// Mock server that returns exchange rate data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"provider": "https://www.exchangerate-api.com",
			"base": "USD",
			"date": "2025-01-01",
			"rates": {
				"USD": 1,
				"KRW": 1388.95,
				"EUR": 0.856
			}
		}`))
	}))
	defer server.Close()

	// Full integration test: fetch -> parse -> extract
	{
		// 1) Fetch data
		rawData, err := c.client.fetchRawData(server.URL)
		assert.NoError(c.T(), err)
		assert.NotNil(c.T(), rawData)

		// 2) Parse data
		parsedData, err := c.client.parseRawData(rawData)
		assert.NoError(c.T(), err)
		assert.NotNil(c.T(), parsedData)

		// 3) Extract KRW rate
		krwRate, err := c.client.extractDataByPath(parsedData, "rates.KRW")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), "1388.95", krwRate)

		// 4) Extract USD rate
		usdRate, err := c.client.extractDataByPath(parsedData, "rates.USD")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), "1", usdRate)

		// 5) Extract base currency
		base, err := c.client.extractDataByPath(parsedData, "base")
		assert.NoError(c.T(), err)
		assert.Equal(c.T(), "USD", base)
	}
}
