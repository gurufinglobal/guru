const { ethers } = require("hardhat");

async function main() {
  console.log("🔌 Testing connection to GXN network...");
  
  try {
    // Get provider
    const provider = ethers.provider;
    
    // Test basic connectivity
    const blockNumber = await provider.getBlockNumber();
    console.log(`✅ Current block number: ${blockNumber}`);
    
    const block = await provider.getBlock(blockNumber);
    console.log(`✅ Latest block hash: ${block.hash}`);
    console.log(`✅ Block timestamp: ${new Date(block.timestamp * 1000).toISOString()}`);
    
    // Get network info
    const network = await provider.getNetwork();
    console.log(`✅ Network name: ${network.name}`);
    console.log(`✅ Chain ID: ${network.chainId}`);
    
    // Test accounts
    const [deployer] = await ethers.getSigners();
    console.log(`✅ Test account: ${deployer.address}`);
    
    const balance = await deployer.getBalance();
    console.log(`✅ Account balance: ${ethers.utils.formatEther(balance)} GXN`);
    
    // Test gas price
    const gasPrice = await provider.getGasPrice();
    console.log(`✅ Current gas price: ${ethers.utils.formatUnits(gasPrice, "gwei")} gwei`);
    
    // Test transaction count
    const nonce = await provider.getTransactionCount(deployer.address);
    console.log(`✅ Transaction count (nonce): ${nonce}`);
    
    console.log("\n🎉 Network connectivity test successful!");
    
  } catch (error) {
    console.error("❌ Network test failed:");
    console.error(error.message);
    process.exit(1);
  }
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error(error);
    process.exit(1);
  });
