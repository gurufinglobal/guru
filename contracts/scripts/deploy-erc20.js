const { ethers } = require("hardhat");

async function main() {
  console.log("🚀 Starting ERC20 token deployment test on GXN network...");
  
  // Get the deployer account
  const [deployer] = await ethers.getSigners();
  console.log(`📝 Deploying contracts with account: ${deployer.address}`);
  
  // Check balance
  const balance = await deployer.getBalance();
  console.log(`💰 Account balance: ${ethers.utils.formatEther(balance)} GXN`);
  
  // Get the ERC20MinterBurnerDecimals factory
  console.log("📦 Getting ERC20MinterBurnerDecimals factory...");
  const ERC20MinterBurnerDecimals = await ethers.getContractFactory("ERC20MinterBurnerDecimals");
  
  console.log("⏳ Deploying ERC20 token...");
  const tokenName = "GXN Test Token";
  const tokenSymbol = "GTT";
  const decimals = 18;
  
  // Deploy the contract
  const token = await ERC20MinterBurnerDecimals.deploy(tokenName, tokenSymbol, decimals);
  
  console.log("⌛ Waiting for deployment confirmation...");
  await token.deployed();
  
  console.log(`✅ ERC20 token deployed to: ${token.address}`);
  console.log(`📋 Transaction hash: ${token.deployTransaction.hash}`);
  
  // Verify the deployment by calling some functions
  console.log("\n🔍 Verifying deployment...");
  
  const name = await token.name();
  const symbol = await token.symbol();
  const tokenDecimals = await token.decimals();
  const totalSupply = await token.totalSupply();
  const deployerBalance = await token.balanceOf(deployer.address);
  
  console.log(`✅ Token name: ${name}`);
  console.log(`✅ Token symbol: ${symbol}`);
  console.log(`✅ Token decimals: ${tokenDecimals}`);
  console.log(`✅ Total supply: ${ethers.utils.formatEther(totalSupply)}`);
  console.log(`✅ Deployer balance: ${ethers.utils.formatEther(deployerBalance)}`);
  
  // Test minting
  console.log("\n💰 Testing token minting...");
  const mintAmount = ethers.utils.parseEther("1000");
  const mintTx = await token.mint(deployer.address, mintAmount);
  console.log(`⌛ Waiting for mint transaction: ${mintTx.hash}`);
  await mintTx.wait();
  
  const newBalance = await token.balanceOf(deployer.address);
  const newTotalSupply = await token.totalSupply();
  console.log(`✅ New deployer balance: ${ethers.utils.formatEther(newBalance)}`);
  console.log(`✅ New total supply: ${ethers.utils.formatEther(newTotalSupply)}`);
  
  // Test transfer
  console.log("\n📤 Testing token transfer...");
  const recipient = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"; // Second Hardhat account
  const transferAmount = ethers.utils.parseEther("100");
  
  const transferTx = await token.transfer(recipient, transferAmount);
  console.log(`⌛ Waiting for transfer transaction: ${transferTx.hash}`);
  await transferTx.wait();
  
  const recipientBalance = await token.balanceOf(recipient);
  const senderBalance = await token.balanceOf(deployer.address);
  console.log(`✅ Recipient balance: ${ethers.utils.formatEther(recipientBalance)}`);
  console.log(`✅ Sender balance: ${ethers.utils.formatEther(senderBalance)}`);
  
  // Test approval and transferFrom
  console.log("\n🔐 Testing token approval...");
  const approveAmount = ethers.utils.parseEther("50");
  const approveTx = await token.approve(recipient, approveAmount);
  console.log(`⌛ Waiting for approve transaction: ${approveTx.hash}`);
  await approveTx.wait();
  
  const allowance = await token.allowance(deployer.address, recipient);
  console.log(`✅ Allowance set: ${ethers.utils.formatEther(allowance)}`);
  
  console.log("\n🎉 ERC20 token deployment and testing completed successfully!");
  console.log("\n📊 Summary:");
  console.log(`   Token Contract: ${token.address}`);
  console.log(`   Token Name: ${name}`);
  console.log(`   Token Symbol: ${symbol}`);
  console.log(`   Decimals: ${tokenDecimals}`);
  console.log(`   Total Supply: ${ethers.utils.formatEther(newTotalSupply)} ${symbol}`);
  console.log(`   Deployer: ${deployer.address}`);
  console.log(`   Network: GXN (631)`);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error("❌ ERC20 deployment failed:");
    console.error(error);
    process.exit(1);
  });
