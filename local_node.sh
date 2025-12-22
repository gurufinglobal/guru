#!/bin/bash

CHAINID="${CHAIN_ID:-guru_631-1}"
MONIKER="localtestnet"
# Remember to change to other types of keyring like 'file' in-case exposing to outside world,
# otherwise your balance will be wiped quickly
# The keyring test does not require private key to steal tokens from you
KEYRING="test"
KEYALGO="eth_secp256k1"

LOGLEVEL="info"
# Set dedicated home directory for the gurud instance
HOMEDIR="$HOME/.gurud"

BASEFEE=630000000000

# Path variables
CONFIG=$HOMEDIR/config/config.toml
APP_TOML=$HOMEDIR/config/app.toml
GENESIS=$HOMEDIR/config/genesis.json
TMP_GENESIS=$HOMEDIR/config/tmp_genesis.json

# validate dependencies are installed
command -v jq >/dev/null 2>&1 || {
	echo >&2 "jq not installed. More info: https://stedolan.github.io/jq/download/"
	exit 1
}

# used to exit on first error (any non-zero exit code)
set -e

# Parse input flags
install=true
overwrite=""
BUILD_FOR_DEBUG=false

while [[ $# -gt 0 ]]; do
	key="$1"
	case $key in
	-y)
		echo "Flag -y passed -> Overwriting the previous chain data."
		overwrite="y"
		shift # Move past the flag
		;;
	-n)
		echo "Flag -n passed -> Not overwriting the previous chain data."
		overwrite="n"
		shift # Move past the argument
		;;
	--no-install)
		echo "Flag --no-install passed -> Skipping installation of the gurud binary."
		install=false
		shift # Move past the flag
		;;
	--remote-debugging)
		echo "Flag --remote-debugging passed -> Building with remote debugging options."
		BUILD_FOR_DEBUG=true
		shift # Move past the flag
		;;
	*)
		echo "Unknown flag passed: $key -> Exiting script!"
		exit 1
		;;
	esac
done

if [[ $install == true ]]; then
	if [[ $BUILD_FOR_DEBUG == true ]]; then
		# for remote debugging the optimization should be disabled and the debug info should not be stripped
		make install COSMOS_BUILD_OPTIONS=nooptimization,nostrip
	else
		make install
	fi
fi

# User prompt if neither -y nor -n was passed as a flag
# and an existing local node configuration is found.
if [[ $overwrite = "" ]]; then
	if [ -d "$HOMEDIR" ]; then
		printf "\nAn existing folder at '%s' was found. You can choose to delete this folder and start a new local node with new keys from genesis. When declined, the existing local node is started. \n" "$HOMEDIR"
		echo "Overwrite the existing configuration and start a new local node? [y/n]"
		read -r overwrite
	else
		overwrite="y"
	fi
fi

# Setup local node if overwrite is set to Yes, otherwise skip setup
if [[ $overwrite == "y" || $overwrite == "Y" ]]; then
	# Remove the previous folder
	rm -rf "$HOMEDIR"

	# Set client config
	gurud config set client chain-id "$CHAINID" --home "$HOMEDIR"
	gurud config set client keyring-backend "$KEYRING" --home "$HOMEDIR"

	# myKey address 0x7cb61d4117ae31a12e393a1cfa3bac666481d02e | guru10jmp6sgh4cc6zt3e8gw05wavvejgr5pwggsdaj
	VAL_KEY="mykey"
	VAL_MNEMONIC="gesture inject test cycle original hollow east ridge hen combine junk child bacon zero hope comfort vacuum milk pitch cage oppose unhappy lunar seat"

	# dev0 address 0xc6fe5d33615a1c52c08018c47e8bc53646a0e101 | guru1cml96vmptgw99syqrrz8az79xer2pcgpawsch9
	USER1_KEY="dev0"
	USER1_MNEMONIC="copper push brief egg scan entry inform record adjust fossil boss egg comic alien upon aspect dry avoid interest fury window hint race symptom"

	# dev1 address 0x963ebdf2e1f8db8707d05fc75bfeffba1b5bac17 | guru1jcltmuhplrdcwp7stlr4hlhlhgd4htqhtx0smk
	USER2_KEY="dev1"
	USER2_MNEMONIC="maximum display century economy unlock van census kite error heart snow filter midnight usage egg venture cash kick motor survey drastic edge muffin visual"

	# dev2 address 0x40a0cb1C63e026A81B55EE1308586E21eec1eFa9 | guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3
	USER3_KEY="dev2"
	USER3_MNEMONIC="will wear settle write dance topic tape sea glory hotel oppose rebel client problem era video gossip glide during yard balance cancel file rose"

	# dev3 address 0x498B5AeC5D439b733dC2F58AB489783A23FB26dA | guru1fx944mzagwdhx0wz7k9tfztc8g3lkfk6ecee3f
	USER4_KEY="dev3"
	USER4_MNEMONIC="doll midnight silk carpet brush boring pluck office gown inquiry duck chief aim exit gain never tennis crime fragile ship cloud surface exotic patch"

	MODERATOR_KEY="moderator"
	MODERATOR_MNEMONIC="piece edge candy scare bicycle grass tackle have mango virus either erosion setup tuition inmate choice sun rule depth suffer debris purse whale napkin"

	# Import keys from mnemonics
	echo "$VAL_MNEMONIC" | gurud keys add "$VAL_KEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOMEDIR"
	echo "$USER1_MNEMONIC" | gurud keys add "$USER1_KEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOMEDIR"
	echo "$USER2_MNEMONIC" | gurud keys add "$USER2_KEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOMEDIR"
	echo "$USER3_MNEMONIC" | gurud keys add "$USER3_KEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOMEDIR"
	echo "$USER4_MNEMONIC" | gurud keys add "$USER4_KEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOMEDIR"
	echo "$MODERATOR_MNEMONIC" | gurud keys add "$MODERATOR_KEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOMEDIR"

	# Set moniker and chain-id for the example chain (Moniker can be anything, chain-id must be an integer)
	gurud init $MONIKER -o --chain-id "$CHAINID" --home "$HOMEDIR"

	# Change parameter token denominations to desired value
	jq '.app_state["staking"]["params"]["bond_denom"]="agxn"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["gov"]["deposit_params"]["min_deposit"][0]["denom"]="agxn"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["gov"]["params"]["min_deposit"][0]["denom"]="agxn"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["gov"]["params"]["expedited_min_deposit"][0]["denom"]="agxn"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["evm"]["params"]["evm_denom"]="agxn"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# feemarket: elasticity_multiplier = 1, base_fee = 6.3*10^-7, min_gas_price = 6.3*10^-7
	jq '.app_state["feemarket"]["params"]["elasticity_multiplier"]="1"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["feemarket"]["params"]["base_fee"]="630000000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["feemarket"]["params"]["min_gas_price"]="630000000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["feemarket"]["params"]["gas_price_adjustment_factor"]="630000000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["feemarket"]["params"]["max_change_rate"]="0.1"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["feemarket"]["params"]["no_base_fee"]=true' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# Set claims start time
	current_date=$(date -u +"%Y-%m-%dT%TZ")
	jq -r --arg current_date "$current_date" '.app_state["claims"]["params"]["airdrop_start_time"]=$current_date' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# disable minting for inflation
	jq '.app_state["mint"]["minter"]["inflation"]="0.000000000000000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["mint"]["minter"]["annual_provisions"]="0.000000000000000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["mint"]["params"]["inflation_rate_change"]="0.000000000000000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["mint"]["params"]["inflation_max"]="0.000000000000000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["mint"]["params"]["inflation_min"]="0.000000000000000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	jq '.app_state["mint"]["params"]["mint_denom"]="agxn"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# Add default token metadata to genesis
	jq '.app_state["bank"]["denom_metadata"]=[{"description":"The native staking token for gurud.","denom_units":[{"denom":"agxn","exponent":0,"aliases":["attogxn"]},{"denom":"gxn","exponent":18,"aliases":[]}],"base":"agxn","display":"gxn","name":"GXN Token","symbol":"GXN","uri":"","uri_hash":""}]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# Enable precompiles in EVM params
	jq '.app_state["evm"]["params"]["active_static_precompiles"]=["0x0000000000000000000000000000000000000100","0x0000000000000000000000000000000000000400","0x0000000000000000000000000000000000000800","0x0000000000000000000000000000000000000801","0x0000000000000000000000000000000000000802","0x0000000000000000000000000000000000000803","0x0000000000000000000000000000000000000804","0x0000000000000000000000000000000000000805","0x0000000000000000000000000000000000000806","0x0000000000000000000000000000000000000807"]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# Set EVM config
	jq '.app_state["evm"]["params"]["evm_denom"]="agxn"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# Enable native denomination as a token pair for STRv2
	jq '.app_state.erc20.native_precompiles=["0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state.erc20.token_pairs=[{contract_owner:1,erc20_address:"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",denom:"atest",enabled:true}]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# Set gas limit in genesis
	jq '.consensus.params.block.max_gas="10000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# Set distribution addresses
	jq -r --arg moderator_address "$(gurud keys show moderator --address --keyring-backend "$KEYRING" --home "$HOMEDIR")" '.app_state["distribution"]["moderator_address"] = $moderator_address' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq -r --arg base_address "$(gurud keys show dev3 --address --keyring-backend "$KEYRING" --home "$HOMEDIR")" '.app_state["distribution"]["base_address"] = $base_address' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# Set feepolicy addresses
	jq -r --arg moderator_address "$(gurud keys show moderator --address --keyring-backend "$KEYRING" --home "$HOMEDIR")" '.app_state["feepolicy"]["moderator_address"] = $moderator_address' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# Set oracle moderator address
	jq -r --arg moderator_address "$(gurud keys show moderator --address --keyring-backend "$KEYRING" --home "$HOMEDIR")" '.app_state["oracle"]["moderator_address"] = $moderator_address' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	# Set auth module parameters for transaction costs
	jq '.app_state["auth"]["params"]["tx_size_cost_per_byte"]="2"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["auth"]["params"]["sig_verify_cost_ed25519"]="118"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
	jq '.app_state["auth"]["params"]["sig_verify_cost_secp256k1"]="200"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

	if [[ $1 == "pending" ]]; then
		if [[ "$OSTYPE" == "darwin"* ]]; then
			sed -i '' 's/timeout_propose = "3s"/timeout_propose = "30s"/g' "$CONFIG"
			sed -i '' 's/timeout_propose_delta = "500ms"/timeout_propose_delta = "5s"/g' "$CONFIG"
			sed -i '' 's/timeout_prevote = "1s"/timeout_prevote = "10s"/g' "$CONFIG"
			sed -i '' 's/timeout_prevote_delta = "500ms"/timeout_prevote_delta = "5s"/g' "$CONFIG"
			sed -i '' 's/timeout_precommit = "1s"/timeout_precommit = "10s"/g' "$CONFIG"
			sed -i '' 's/timeout_precommit_delta = "500ms"/timeout_precommit_delta = "5s"/g' "$CONFIG"
			sed -i '' 's/timeout_commit = "5s"/timeout_commit = "150s"/g' "$CONFIG"
			sed -i '' 's/timeout_broadcast_tx_commit = "10s"/timeout_broadcast_tx_commit = "150s"/g' "$CONFIG"
		else
			sed -i 's/timeout_propose = "3s"/timeout_propose = "30s"/g' "$CONFIG"
			sed -i 's/timeout_propose_delta = "500ms"/timeout_propose_delta = "5s"/g' "$CONFIG"
			sed -i 's/timeout_prevote = "1s"/timeout_prevote = "10s"/g' "$CONFIG"
			sed -i 's/timeout_prevote_delta = "500ms"/timeout_prevote_delta = "5s"/g' "$CONFIG"
			sed -i 's/timeout_precommit = "1s"/timeout_precommit = "10s"/g' "$CONFIG"
			sed -i 's/timeout_precommit_delta = "500ms"/timeout_precommit_delta = "5s"/g' "$CONFIG"
			sed -i 's/timeout_commit = "5s"/timeout_commit = "150s"/g' "$CONFIG"
			sed -i 's/timeout_broadcast_tx_commit = "10s"/timeout_broadcast_tx_commit = "150s"/g' "$CONFIG"
		fi
	fi

	# enable prometheus metrics and all APIs for dev node
	if [[ "$OSTYPE" == "darwin"* ]]; then
		sed -i '' 's/prometheus = false/prometheus = true/' "$CONFIG"
		sed -i '' 's/prometheus-retention-time = 0/prometheus-retention-time  = 1000000000000/g' "$APP_TOML"
		sed -i '' 's/enabled = false/enabled = true/g' "$APP_TOML"
		sed -i '' 's/enable = false/enable = true/g' "$APP_TOML"
	else
		sed -i 's/prometheus = false/prometheus = true/' "$CONFIG"
		sed -i 's/prometheus-retention-time  = "0"/prometheus-retention-time  = "1000000000000"/g' "$APP_TOML"
		sed -i 's/enabled = false/enabled = true/g' "$APP_TOML"
		sed -i 's/enable = false/enable = true/g' "$APP_TOML"
	fi

	# Change proposal periods to pass within a reasonable time for local testing
	sed -i.bak 's/"max_deposit_period": "172800s"/"max_deposit_period": "30s"/g' "$GENESIS"
	sed -i.bak 's/"voting_period": "172800s"/"voting_period": "30s"/g' "$GENESIS"
	sed -i.bak 's/"expedited_voting_period": "86400s"/"expedited_voting_period": "15s"/g' "$GENESIS"

	# set custom pruning settings
	sed -i.bak 's/pruning = "default"/pruning = "custom"/g' "$APP_TOML"
	sed -i.bak 's/pruning-keep-recent = "0"/pruning-keep-recent = "2"/g' "$APP_TOML"
	sed -i.bak 's/pruning-interval = "0"/pruning-interval = "10"/g' "$APP_TOML"

	# Allocate genesis accounts (cosmos formatted addresses)
	gurud genesis add-genesis-account "$VAL_KEY" 100000000000000000000000000agxn --keyring-backend "$KEYRING" --home "$HOMEDIR"
	gurud genesis add-genesis-account "$USER1_KEY" 1000000000000000000000agxn --keyring-backend "$KEYRING" --home "$HOMEDIR"
	gurud genesis add-genesis-account "$USER2_KEY" 1000000000000000000000agxn --keyring-backend "$KEYRING" --home "$HOMEDIR"
	gurud genesis add-genesis-account "$USER3_KEY" 1000000000000000000000agxn --keyring-backend "$KEYRING" --home "$HOMEDIR"
	gurud genesis add-genesis-account "$USER4_KEY" 1000000000000000000000agxn --keyring-backend "$KEYRING" --home "$HOMEDIR"
	gurud genesis add-genesis-account "$MODERATOR_KEY" 1000000000000000000000agxn --keyring-backend "$KEYRING" --home "$HOMEDIR"

	# Sign genesis transaction
	gurud genesis gentx "$VAL_KEY" 1000000000000000000000agxn --gas-prices ${BASEFEE}agxn --keyring-backend "$KEYRING" --chain-id "$CHAINID" --home "$HOMEDIR"
	## In case you want to create multiple validators at genesis
	## 1. Back to `gurud keys add` step, init more keys
	## 2. Back to `gurud add-genesis-account` step, add balance for those
	## 3. Clone this ~/.gurud home directory into some others, let's say `~/.clonedOsd`
	## 4. Run `gentx` in each of those folders
	## 5. Copy the `gentx-*` folders under `~/.clonedOsd/config/gentx/` folders into the original `~/.gurud/config/gentx`

	# Collect genesis tx
	gurud genesis collect-gentxs --home "$HOMEDIR"

	# Run this to ensure everything worked and that the genesis file is setup correctly
	gurud genesis validate-genesis --home "$HOMEDIR"

	if [[ $1 == "pending" ]]; then
		echo "pending mode is on, please wait for the first block committed."
	fi
fi

# Start the node
gurud start "$TRACE" \
	--log_level $LOGLEVEL \
	--minimum-gas-prices=0.0001agxn \
	--home "$HOMEDIR" \
	--json-rpc.api eth,txpool,personal,net,debug,web3 \
	--chain-id "$CHAINID"
