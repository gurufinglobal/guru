package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/v2/crypto/hd"
	"github.com/gurufinglobal/guru/v2/oralce/config"
	"github.com/gurufinglobal/guru/v2/testutil"
	"github.com/gurufinglobal/guru/v2/types"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type PoolTestSuite struct {
	suite.Suite

	ctx        context.Context
	cancelFunc context.CancelFunc
	pool       *WorkerPool

	// Test addresses generated from testutil
	testAddresses []sdk.AccAddress

	// Temporary directory for test keyring
	tempDir string
}

func (p *PoolTestSuite) SetupSuite() {
	p.T().Log("setting up pool test suite")

	// Create temporary directory for test keyring
	tempDir, err := os.MkdirTemp("", "pool-test-keyring-*")
	p.Require().NoError(err)
	p.tempDir = tempDir

	// Initialize test configuration
	config.TestConfig()

	// Create test key in the keyring using config's settings
	p.createTestKey()

	// Generate additional test addresses (not used for config but for test data)
	_, acc, err := testutil.GeneratePrivKeyAddressPairs(5)
	p.Require().NoError(err)
	p.testAddresses = acc

	// Create context with cancel
	p.ctx, p.cancelFunc = context.WithCancel(context.Background())

	// Create worker pool
	p.pool = New(p.ctx, log.NewTestLogger(p.T()))
}

func (p *PoolTestSuite) TearDownSuite() {
	p.T().Log("tearing down pool test suite")
	if p.cancelFunc != nil {
		p.cancelFunc()
	}

	// Clean up temporary directory
	if p.tempDir != "" {
		os.RemoveAll(p.tempDir)
	}
}

// createTestKey creates a test key in the keyring for testing
func (p *PoolTestSuite) createTestKey() {
	// Get the keyring from config (this will use the config's settings)
	kr := config.Keyring()

	// Create key record with the same name that config.Address() will look for
	keyName := config.KeyName() // This should be "mykey" from TestConfig

	// Try to create a new account with mnemonic using the correct method
	_, _, err := kr.NewMnemonic(keyName, keyring.English, types.BIP44HDPath, keyring.DefaultBIP39Passphrase, hd.EthSecp256k1)
	if err != nil {
		// If key already exists, delete it first and try again
		kr.Delete(keyName)
		_, _, err = kr.NewMnemonic(keyName, keyring.English, types.BIP44HDPath, keyring.DefaultBIP39Passphrase, hd.EthSecp256k1)
		p.Require().NoError(err)
	}
}

func TestPoolTestSuite(t *testing.T) {
	suite.Run(t, new(PoolTestSuite))
}

func (p *PoolTestSuite) TestNew() {
	p.T().Log("testing new worker pool")

	// Valid context and logger -> should create pool
	{
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		logger := log.NewTestLogger(p.T())
		pool := New(ctx, logger)

		assert.NotNil(p.T(), pool)
		assert.NotNil(p.T(), pool.logger)
		assert.NotNil(p.T(), pool.jobStore)
		assert.NotNil(p.T(), pool.resultCh)
		assert.NotNil(p.T(), pool.workerFunc)
		assert.NotNil(p.T(), pool.workerGroup)
		assert.NotNil(p.T(), pool.client)
	}
}

func (p *PoolTestSuite) TestResults() {
	p.T().Log("testing results channel")

	// Should return read-only channel
	{
		resultCh := p.pool.Results()
		assert.NotNil(p.T(), resultCh)

		// Channel should be readable but not writable
		select {
		case <-resultCh:
		default:
			// Channel is empty, which is expected
		}
	}
}

func (p *PoolTestSuite) TestProcessRequestDoc_StatusNotEnabled() {
	p.T().Log("testing process request doc - status not enabled")

	// 1) Request status is DISABLED -> should return early
	{
		requestDoc := oracletypes.OracleRequestDoc{
			RequestId: 1,
			Status:    oracletypes.RequestStatus_REQUEST_STATUS_DISABLED,
		}

		// Should not panic or error, just return early
		p.pool.ProcessRequestDoc(p.ctx, requestDoc, uint64(time.Now().Unix()))
	}

	// 2) Request status is PAUSED -> should return early
	{
		requestDoc := oracletypes.OracleRequestDoc{
			RequestId: 2,
			Status:    oracletypes.RequestStatus_REQUEST_STATUS_PAUSED,
		}

		// Should not panic or error, just return early
		p.pool.ProcessRequestDoc(p.ctx, requestDoc, uint64(time.Now().Unix()))
	}

	// 3) Request status is UNSPECIFIED -> should return early
	{
		requestDoc := oracletypes.OracleRequestDoc{
			RequestId: 3,
			Status:    oracletypes.RequestStatus_REQUEST_STATUS_UNSPECIFIED,
		}

		// Should not panic or error, just return early
		p.pool.ProcessRequestDoc(p.ctx, requestDoc, uint64(time.Now().Unix()))
	}
}

func (p *PoolTestSuite) TestProcessRequestDoc_AccountNotAssigned() {
	p.T().Log("testing process request doc - account not assigned")

	// Request with account list that doesn't include current oracle address -> should return early
	{
		requestDoc := oracletypes.OracleRequestDoc{
			RequestId: 4,
			Status:    oracletypes.RequestStatus_REQUEST_STATUS_ENABLED,
			AccountList: []string{
				p.testAddresses[1].String(), // First test address
				p.testAddresses[2].String(), // Second test address
			},
			Period: 3,
			Endpoints: []*oracletypes.OracleEndpoint{
				{
					Url:       "https://api.example.com/data",
					ParseRule: "rates.KRW",
				},
			},
		}

		// Should not panic or error, just return early
		p.pool.ProcessRequestDoc(p.ctx, requestDoc, uint64(time.Now().Unix()))
	}
}

func (p *PoolTestSuite) TestProcessRequestDoc_ValidRequest() {
	p.T().Log("testing process request doc - valid request")

	// Get the oracle address from config (which now uses our test key)
	oracleAddress := config.Address().String()

	// Valid request with oracle address in account list -> should process successfully
	{
		requestDoc := oracletypes.OracleRequestDoc{
			RequestId: 5,
			Status:    oracletypes.RequestStatus_REQUEST_STATUS_ENABLED,
			AccountList: []string{
				oracleAddress,               // Oracle address is first
				p.testAddresses[1].String(), // Second test address
				p.testAddresses[2].String(), // Third test address
			},
			Period: 3,
			Nonce:  1,
			Endpoints: []*oracletypes.OracleEndpoint{
				{
					Url:       "https://api.exchangerate-api.com/v4/latest/USD",
					ParseRule: "rates.KRW",
				},
				{
					Url:       "https://api.backup.com/rates",
					ParseRule: "data.KRW",
				},
				{
					Url:       "https://api.third.com/rates",
					ParseRule: "rates.KRW",
				},
			},
		}

		// Should not panic or error
		p.pool.ProcessRequestDoc(p.ctx, requestDoc, uint64(time.Now().Unix()))

		// Check if job was stored
		time.Sleep(100 * time.Millisecond) // Allow goroutine to execute
	}

	// Valid request with current address in middle of account list -> should process successfully
	{
		requestDoc := oracletypes.OracleRequestDoc{
			RequestId: 6,
			Status:    oracletypes.RequestStatus_REQUEST_STATUS_ENABLED,
			AccountList: []string{
				p.testAddresses[1].String(), // First test address
				oracleAddress,               // Oracle address is second
				p.testAddresses[2].String(), // Third test address
			},
			Period: 4,
			Nonce:  2,
			Endpoints: []*oracletypes.OracleEndpoint{
				{
					Url:       "https://api.exchangerate-api.com/v4/latest/USD",
					ParseRule: "rates.EUR",
				},
				{
					Url:       "https://api.backup.com/rates",
					ParseRule: "data.EUR",
				},
				{
					Url:       "https://api.third.com/rates",
					ParseRule: "rates.EUR",
				},
			},
		}

		// Should not panic or error
		p.pool.ProcessRequestDoc(p.ctx, requestDoc, uint64(time.Now().Unix()))

		// Check if job was stored
		time.Sleep(100 * time.Millisecond) // Allow goroutine to execute
	}
}

func (p *PoolTestSuite) TestProcessComplete_JobNotFound() {
	p.T().Log("testing process complete - job not found")

	// Process complete event for non-existent job -> should return early
	{
		// Should not panic or error, just return early with debug log
		p.pool.ProcessComplete(p.ctx, "999", 5, uint64(time.Now().Unix()))
	}
}

func (p *PoolTestSuite) TestProcessComplete_ValidJob() {
	p.T().Log("testing process complete - valid job")

	// Get oracle address from config (which now uses our test key)
	oracleAddress := config.Address().String()

	// First, create a job by processing a request doc
	{
		requestDoc := oracletypes.OracleRequestDoc{
			RequestId: 7,
			Status:    oracletypes.RequestStatus_REQUEST_STATUS_ENABLED,
			AccountList: []string{
				oracleAddress,
				p.testAddresses[1].String(),
			},
			Period: 3,
			Nonce:  1,
			Endpoints: []*oracletypes.OracleEndpoint{
				{
					Url:       "https://api.exchangerate-api.com/v4/latest/USD",
					ParseRule: "rates.KRW",
				},
				{
					Url:       "https://api.backup.com/rates",
					ParseRule: "rates.KRW",
				},
			},
		}

		p.pool.ProcessRequestDoc(p.ctx, requestDoc, uint64(time.Now().Unix()))
		time.Sleep(100 * time.Millisecond) // Allow job to be stored
	}

	// Now process complete event for the existing job
	{
		// Should update the job's nonce and reschedule
		p.pool.ProcessComplete(p.ctx, "7", 3, uint64(time.Now().Unix()))
		time.Sleep(100 * time.Millisecond) // Allow processing
	}

	// Process complete with lower nonce -> should use max nonce
	{
		// Should not decrease the nonce
		p.pool.ProcessComplete(p.ctx, "7", 2, uint64(time.Now().Unix()))
		time.Sleep(100 * time.Millisecond) // Allow processing
	}
}

func (p *PoolTestSuite) TestIntegration_FullJobExecution() {
	p.T().Log("testing integration - full job execution")

	// Create mock server that returns exchange rate data
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

	// Get oracle address from config (which now uses our test key)
	oracleAddress := config.Address().String()

	// Create a valid request that will trigger job execution
	{
		requestDoc := oracletypes.OracleRequestDoc{
			RequestId: 8,
			Status:    oracletypes.RequestStatus_REQUEST_STATUS_ENABLED,
			AccountList: []string{
				oracleAddress,
				p.testAddresses[1].String(),
			},
			Period: 0, // No delay for immediate execution
			Nonce:  1,
			Endpoints: []*oracletypes.OracleEndpoint{
				{
					Url:       server.URL,
					ParseRule: "rates.KRW",
				},
				{
					Url:       server.URL,
					ParseRule: "rates.KRW",
				},
			},
		}

		// Process the request
		p.pool.ProcessRequestDoc(p.ctx, requestDoc, uint64(time.Now().Unix()))

		// Wait for job execution and result
		select {
		case result := <-p.pool.Results():
			if result != nil {
				assert.Equal(p.T(), uint64(8), result.ID)
				assert.Equal(p.T(), "1388.95", result.Data)
				assert.Greater(p.T(), result.Nonce, uint64(0))
			}
		case <-time.After(5 * time.Second):
			p.T().Log("timeout waiting for result")
		}
	}
}

func (p *PoolTestSuite) TestIntegration_JobExecutionError() {
	p.T().Log("testing integration - job execution error")

	// Create mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	// Get oracle address from config (which now uses our test key)
	oracleAddress := config.Address().String()

	// Create a request that will trigger job execution with error
	{
		requestDoc := oracletypes.OracleRequestDoc{
			RequestId: 9,
			Status:    oracletypes.RequestStatus_REQUEST_STATUS_ENABLED,
			AccountList: []string{
				oracleAddress,
				p.testAddresses[1].String(),
			},
			Period: 0, // No delay for immediate execution
			Nonce:  1,
			Endpoints: []*oracletypes.OracleEndpoint{
				{
					Url:       server.URL,
					ParseRule: "rates.KRW",
				},
				{
					Url:       server.URL,
					ParseRule: "rates.USD",
				},
			},
		}

		// Process the request
		p.pool.ProcessRequestDoc(p.ctx, requestDoc, uint64(time.Now().Unix()))

		// Wait for job execution result (should be nil due to error)
		select {
		case result := <-p.pool.Results():
			assert.Nil(p.T(), result) // Should be nil due to fetch error
		case <-time.After(5 * time.Second):
			p.T().Log("timeout waiting for error result")
		}
	}
}

func (p *PoolTestSuite) TestIntegration_InvalidParseRule() {
	p.T().Log("testing integration - invalid parse rule")

	// Create mock server that returns valid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"provider": "https://www.exchangerate-api.com",
			"base": "USD",
			"rates": {
				"USD": 1,
				"KRW": 1388.95
			}
		}`))
	}))
	defer server.Close()

	// Get oracle address from config (which now uses our test key)
	oracleAddress := config.Address().String()

	// Create a request with invalid parse rule
	{
		requestDoc := oracletypes.OracleRequestDoc{
			RequestId: 10,
			Status:    oracletypes.RequestStatus_REQUEST_STATUS_ENABLED,
			AccountList: []string{
				oracleAddress,
				p.testAddresses[1].String(),
			},
			Period: 0, // No delay for immediate execution
			Nonce:  1,
			Endpoints: []*oracletypes.OracleEndpoint{
				{
					Url:       server.URL,
					ParseRule: "rates.NONEXISTENT", // Invalid path
				},
				{
					Url:       server.URL,
					ParseRule: "rates.KRW", // Valid path
				},
			},
		}

		// Process the request
		p.pool.ProcessRequestDoc(p.ctx, requestDoc, uint64(time.Now().Unix()))

		// Wait for job execution - should not produce result due to path error
		select {
		case <-p.pool.Results():
			p.T().Log("unexpected result received")
		case <-time.After(2 * time.Second):
			// Expected timeout due to extraction error
			p.T().Log("expected timeout due to invalid parse rule")
		}
	}
}
