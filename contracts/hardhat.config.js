require("@nomiclabs/hardhat-waffle");

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
  solidity: {
    compilers: [
      {
        version: "0.8.20",
      },
      // This version is required to compile the werc9 contract.
      {
        version: "0.4.22",
      },
    ],
  },
  paths: {
    sources: "./solidity",
  },
  networks: {
    guru: {
      url: "http://127.0.0.1:8545", // gurud JSON-RPC endpoint
      chainId: 631, // guru_631-1 chain ID
      gas: 6000000,
      gasPrice: 20000000000, // 20 gwei
      accounts: [
        // Test private key - DO NOT use in production
        "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
      ]
    },
    localhost: {
      url: "http://127.0.0.1:8545",
      chainId: 631,
      gas: 6000000,
      gasPrice: 20000000000,
      accounts: [
        "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
      ]
    }
  },
  defaultNetwork: "guru"
};
