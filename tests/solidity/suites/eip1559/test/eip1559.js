/* eslint-disable no-undef */

contract('Transaction', async function (accounts) {
  it('should send a transaction with EIP-1559 flag', async function () {
    // The chain enforces a high global minimum gas price (min_gas_price).
    // For EIP-1559 txs, ensure maxFeePerGas/maxPriorityFeePerGas are high enough.
    // (Example: min_gas_price=630 gwei => 630_000_000_000 wei)
    const minGasPriceWei = '630000000000' // 630 gwei

    const tx = await web3.eth.sendTransaction({
      from: accounts[0],
      to: accounts[1]
        ? accounts[1]
        : '0x0000000000000000000000000000000000000000',
      value: '10000000',
      gas: '21000',
      type: '0x2',
      maxFeePerGas: minGasPriceWei,
      maxPriorityFeePerGas: minGasPriceWei,
      common: {
        hardfork: 'london'
      }
    })
    assert.equal(tx.type, '0x2', 'Tx type should be 0x2')
  })
})
