const { expect } = require('chai')
const hre = require('hardhat')

describe('Staking', function () {
  it('should stake to a validator', async function () {
    // NOTE: This chain uses `guru` bech32 prefixes, so validator operator address
    // must start with `guruvaloper` (not `cosmosvaloper`).
    // Derivation check: `gurud keys show mykey --bech val -a --home ~/.gurud`
    const valAddr = 'guruvaloper10jmp6sgh4cc6zt3e8gw05wavvejgr5pwrdtkut'
    const stakeAmount = hre.ethers.parseEther('0.001')

    const staking = await hre.ethers.getContractAt(
      'StakingI',
      '0x0000000000000000000000000000000000000800'
    )

    const [signer] = await hre.ethers.getSigners()
    // The chain enforces a high global minimum gas price (min_gas_price).
    // Hardhat sends EIP-1559 txs by default, so set fees high enough.
    const minGasPrice = hre.ethers.parseUnits('630', 'gwei')

    const tx = await staking
      .connect(signer)
      .delegate(signer, valAddr, stakeAmount, {
        maxFeePerGas: minGasPrice,
        maxPriorityFeePerGas: minGasPrice
      })
    await new Promise(r => setTimeout(r, 200));
    await tx.wait(1)

    // Query delegation
    const delegation = await staking.delegation(signer, valAddr)
    expect(delegation.balance.amount).to.equal(
      stakeAmount,
      'Stake amount does not match'
    )
  })
})
