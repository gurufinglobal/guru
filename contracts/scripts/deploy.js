const { ethers } = require("hardhat");

async function main() {
  console.log("🚀 Starting smart contract deployment test on GXN network...");
  
  // Get the deployer account
  const [deployer] = await ethers.getSigners();
  console.log(`📝 Deploying contracts with account: ${deployer.address}`);
  
  // Check balance
  const balance = await deployer.getBalance();
  console.log(`💰 Account balance: ${ethers.utils.formatEther(balance)} GXN`);
  
  // Get network info
  const network = await ethers.provider.getNetwork();
  console.log(`🌐 Network: ${network.name} (chainId: ${network.chainId})`);
  
  // Get the TestContract factory
  console.log("📦 Getting TestContract factory...");
  const TestContract = await ethers.getContractFactory("TestContract");
  
  console.log("⏳ Deploying TestContract...");
  const initialValue = 42;
  const initialMessage = "Hello GXN Network!";
  
  // Deploy the contract
  const testContract = await TestContract.deploy(initialValue, initialMessage);
  
  console.log("⌛ Waiting for deployment confirmation...");
  await testContract.deployed();
  
  console.log(`✅ TestContract deployed to: ${testContract.address}`);
  console.log(`📋 Transaction hash: ${testContract.deployTransaction.hash}`);
  
  // Verify the deployment by calling some functions
  console.log("\n🔍 Verifying deployment...");
  
  const storedValue = await testContract.getValue();
  const storedMessage = await testContract.getMessage();
  const contractOwner = await testContract.getOwner();
  
  console.log(`✅ Stored value: ${storedValue}`);
  console.log(`✅ Stored message: ${storedMessage}`);
  console.log(`✅ Contract owner: ${contractOwner}`);
  
  // Test a transaction
  console.log("\n📝 Testing contract interaction...");
  const newValue = 123;
  const setValueTx = await testContract.setValue(newValue);
  console.log(`⌛ Waiting for setValue transaction: ${setValueTx.hash}`);
  await setValueTx.wait();
  
  const updatedValue = await testContract.getValue();
  console.log(`✅ Updated value: ${updatedValue}`);
  
  // Test gas estimation
  console.log("\n⛽ Testing gas estimation...");
  const gasEstimate = await testContract.estimateGas.complexOperation();
  console.log(`✅ Gas estimate for complexOperation: ${gasEstimate}`);
  
  // Execute complex operation (view function - no transaction needed)
  const complexResult = await testContract.complexOperation();
  console.log(`✅ Complex operation result: ${complexResult}`);
  
  console.log("\n🎉 Smart contract deployment and testing completed successfully!");
  console.log("\n📊 Summary:");
  console.log(`   Contract Address: ${testContract.address}`);
  console.log(`   Deployer: ${deployer.address}`);
  console.log(`   Network: ${network.name} (${network.chainId})`);
  console.log(`   Initial Value: ${initialValue} → Updated Value: ${updatedValue}`);
  console.log(`   Initial Message: "${initialMessage}"`);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error("❌ Deployment failed:");
    console.error(error);
    process.exit(1);
  });
