# Constitution

`x/constitution` owns chain policy that must be applied consistently by every
node. In addition to validator self-bond and fee separation policy, it schedules
oracle-driven chain-wide minimum gas price updates.

## Oracle-Driven Minimum Gas Price

The current target oracle symbol is hard-coded as `TRX/USD`. When `x/oracle`
accepts a positive numeric value for that symbol, it calls the constitution
hook with the accepted value and the source task's `submission_interval`.

The hook stores one pending minimum gas price update:

- formula: `min_gas_price = floor(scale_factor * 1e18 / oracle_price_atoms)`
- scale factor: `630000000000`
- clamp: at most 10% up or down from the current feemarket minimum gas price
- delay: `min(source_task.submission_interval, 10)` blocks
- replacement: a newer accepted target value replaces the existing pending item

The `submission_interval` is therefore an operational UX rule. Validators should
configure the target oracle task cadence so users can predict fee changes, while
the 10-block cap prevents refreshed oracle data from waiting too long when the
task interval is large.

The scheduled value is applied in `EndBlock` for the block immediately before
its `effective_height`, so the new feemarket value is active for transactions in
the effective block.

Because genesis feemarket `min_gas_price` is non-zero, genesis gentx workflows
must include a sufficient fee.
