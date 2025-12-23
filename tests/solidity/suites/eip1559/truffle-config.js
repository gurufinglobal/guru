// const HDWalletProvider = require('@truffle/hdwallet-provider');

module.exports = {
  networks: {
    cosmos: { // Truffle 'development' network uses defaults.
      host: '127.0.0.1', // JSON-RPC host.
      port: 8545, // JSON-RPC port.
      network_id: '*', // Any network id.
      gas: 5000000, // Gas limit per tx.
      gasPrice: 630000000000 // Gas price (wei). Must be >= chain min_gas_price.
    }
  },
  compilers: {
    solc: {
      version: '0.8.18'
    }
  }
}
