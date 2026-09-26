// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

package settle

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"

	"cosmossdk.io/math"

	cmn "github.com/cosmos/evm/precompiles/common"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/provenance"
	appstypes "github.com/muthu2201/vapor-chain/chain/x/apps/types"
	settlekeeper "github.com/muthu2201/vapor-chain/chain/x/settle/keeper"
)

var (
	errBadArgs       = errors.New("settle: invalid arguments")
	errAmountTooWide = errors.New("settle: amount exceeds 255 bits")
	errNotBankNative = errors.New("settle: token is not a bank-native coin registered in x/erc20")
	errNeedApp       = errors.New("settle: caller is not a contract of an ACTIVE registered app")
)

// maxAmountBits keeps every amount inside math.Int's 256-bit range with room
// for the fee multiplication.
const maxAmountBits = 255

func argAddress(args []interface{}, i int) (common.Address, error) {
	if i >= len(args) {
		return common.Address{}, errBadArgs
	}
	a, ok := args[i].(common.Address)
	if !ok {
		return common.Address{}, errBadArgs
	}
	return a, nil
}

func argUint(args []interface{}, i int) (*big.Int, error) {
	if i >= len(args) {
		return nil, errBadArgs
	}
	v, ok := args[i].(*big.Int)
	if !ok || v == nil || v.Sign() < 0 {
		return nil, errBadArgs
	}
	return v, nil
}

func argAmount(args []interface{}, i int) (math.Int, error) {
	v, err := argUint(args, i)
	if err != nil {
		return math.Int{}, err
	}
	if v.BitLen() > maxAmountBits {
		return math.Int{}, errAmountTooWide
	}
	return math.NewIntFromBigInt(v), nil
}

func argUint64(args []interface{}, i int) (uint64, error) {
	if i >= len(args) {
		return 0, errBadArgs
	}
	v, ok := args[i].(uint64)
	if !ok {
		return 0, errBadArgs
	}
	return v, nil
}

func acc(a common.Address) sdk.AccAddress { return sdk.AccAddress(a.Bytes()) }

// denomOfToken maps an ERC-20 address to its bank denom. Only bank-native
// coins (ERC-20 precompile representations, e.g. IBC USDC) are accepted, so
// Settle never has to call into arbitrary ERC-20 bytecode.
func (p Precompile) denomOfToken(ctx sdk.Context, token common.Address) (string, error) {
	id := p.erc20.GetTokenPairID(ctx, token.Hex())
	if len(id) == 0 {
		return "", errNotBankNative
	}
	pair, found := p.erc20.GetTokenPair(ctx, id)
	if !found || !pair.Enabled || !pair.IsNativeCoin() {
		return "", errNotBankNative
	}
	return pair.Denom, nil
}

func (p Precompile) appOfCaller(ctx sdk.Context, caller common.Address) *appstypes.App {
	if app, ok := p.apps.AppOfContract(ctx, caller); ok {
		return &app
	}
	return nil
}

func (p Precompile) emit(ctx sdk.Context, stateDB vm.StateDB, event string, indexed []interface{}, data ...interface{}) error {
	ev, ok := p.Events[event]
	if !ok {
		return fmt.Errorf("settle: unknown event %s", event)
	}
	topics := make([]common.Hash, 0, 1+len(indexed))
	topics = append(topics, ev.ID)
	for _, v := range indexed {
		t, err := cmn.MakeTopic(v)
		if err != nil {
			return err
		}
		topics = append(topics, t)
	}
	var nonIndexed abi.Arguments
	for _, in := range ev.Inputs {
		if !in.Indexed {
			nonIndexed = append(nonIndexed, in)
		}
	}
	packed, err := nonIndexed.Pack(data...)
	if err != nil {
		return err
	}
	stateDB.AddLog(&ethtypes.Log{
		Address:     p.Address(),
		Topics:      topics,
		Data:        packed,
		BlockNumber: uint64(ctx.BlockHeight()), //nolint:gosec // block height is positive
	})
	return nil
}

func (p Precompile) emitSettled(ctx sdk.Context, stateDB vm.StateDB, res settlekeeper.PayResult, payer, payee, token, referrer common.Address, amount math.Int) error {
	return p.emit(ctx, stateDB, "Settled",
		[]interface{}{res.AppID, payer, payee},
		token, amount.BigInt(), res.Fee.BigInt(), res.Net.BigInt(), referrer)
}

// pay(token, amount, payee, referrer): funds come from msg.sender.
func (p Precompile) pay(ctx sdk.Context, stateDB vm.StateDB, caller common.Address, method *abi.Method, args []interface{}) ([]byte, error) {
	token, err := argAddress(args, 0)
	if err != nil {
		return nil, err
	}
	amount, err := argAmount(args, 1)
	if err != nil {
		return nil, err
	}
	payee, err := argAddress(args, 2)
	if err != nil {
		return nil, err
	}
	referrer, err := argAddress(args, 3)
	if err != nil {
		return nil, err
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	req := settlekeeper.PayRequest{
		Payer: acc(caller), Payee: acc(payee), Amount: sdk.NewCoin(denom, amount),
		App: p.appOfCaller(ctx, caller),
	}
	if referrer != (common.Address{}) {
		req.Referrer = acc(referrer)
	}
	res, err := p.settle.Pay(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := p.emitSettled(ctx, stateDB, res, caller, payee, token, referrer, amount); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(res.Fee.BigInt(), res.Net.BigInt())
}

// payFrom(payer, token, amount, payee, referrer): pulls from payer via the
// allowance payer granted to the CALLER's app.
func (p Precompile) payFrom(ctx sdk.Context, stateDB vm.StateDB, caller common.Address, method *abi.Method, args []interface{}) ([]byte, error) {
	payer, err := argAddress(args, 0)
	if err != nil {
		return nil, err
	}
	token, err := argAddress(args, 1)
	if err != nil {
		return nil, err
	}
	amount, err := argAmount(args, 2)
	if err != nil {
		return nil, err
	}
	payee, err := argAddress(args, 3)
	if err != nil {
		return nil, err
	}
	referrer, err := argAddress(args, 4)
	if err != nil {
		return nil, err
	}
	app := p.appOfCaller(ctx, caller)
	if app == nil {
		return nil, errNeedApp
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if payer != caller {
		if err := p.settle.SpendAllowance(ctx, acc(payer), app.AppId, denom, amount); err != nil {
			return nil, err
		}
	}
	req := settlekeeper.PayRequest{Payer: acc(payer), Payee: acc(payee), Amount: sdk.NewCoin(denom, amount), App: app}
	if referrer != (common.Address{}) {
		req.Referrer = acc(referrer)
	}
	res, err := p.settle.Pay(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := p.emitSettled(ctx, stateDB, res, payer, payee, token, referrer, amount); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(res.Fee.BigInt(), res.Net.BigInt())
}

// tabPay(payer, token, amount, payee)
func (p Precompile) tabPay(ctx sdk.Context, stateDB vm.StateDB, caller common.Address, method *abi.Method, args []interface{}) ([]byte, error) {
	payer, err := argAddress(args, 0)
	if err != nil {
		return nil, err
	}
	token, err := argAddress(args, 1)
	if err != nil {
		return nil, err
	}
	amount, err := argAmount(args, 2)
	if err != nil {
		return nil, err
	}
	payee, err := argAddress(args, 3)
	if err != nil {
		return nil, err
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	app := p.appOfCaller(ctx, caller)
	if payer != caller {
		if app == nil {
			return nil, errNeedApp
		}
		if err := p.settle.SpendAllowance(ctx, acc(payer), app.AppId, denom, amount); err != nil {
			return nil, err
		}
	}
	settled, res, err := p.settle.TabDeposit(ctx, app, acc(payer), acc(payee), sdk.NewCoin(denom, amount), true)
	if err != nil {
		return nil, err
	}
	var appID uint64
	if app != nil {
		appID = app.AppId
	}
	if err := p.emit(ctx, stateDB, "TabDeposited", []interface{}{appID, payer, payee}, token, amount.BigInt(), settled); err != nil {
		return nil, err
	}
	if settled {
		if err := p.emitSettled(ctx, stateDB, res, payer, payee, token, common.Address{}, res.Fee.Add(res.Net)); err != nil {
			return nil, err
		}
	}
	return method.Outputs.Pack(settled)
}

// closeTab(appId, payer, payee, token)
func (p Precompile) closeTab(ctx sdk.Context, stateDB vm.StateDB, caller common.Address, method *abi.Method, args []interface{}) ([]byte, error) {
	appID, err := argUint64(args, 0)
	if err != nil {
		return nil, err
	}
	payer, err := argAddress(args, 1)
	if err != nil {
		return nil, err
	}
	payee, err := argAddress(args, 2)
	if err != nil {
		return nil, err
	}
	token, err := argAddress(args, 3)
	if err != nil {
		return nil, err
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	res, err := p.settle.CloseTab(ctx, acc(caller), appID, acc(payer), acc(payee), denom)
	if err != nil {
		return nil, err
	}
	if err := p.emitSettled(ctx, stateDB, res, payer, payee, token, common.Address{}, res.Fee.Add(res.Net)); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(res.Fee.BigInt(), res.Net.BigInt())
}

// approveApp(appId, token, amount): msg.sender authorizes ONE app.
func (p Precompile) approveApp(ctx sdk.Context, stateDB vm.StateDB, caller common.Address, method *abi.Method, args []interface{}) ([]byte, error) {
	appID, err := argUint64(args, 0)
	if err != nil {
		return nil, err
	}
	token, err := argAddress(args, 1)
	if err != nil {
		return nil, err
	}
	raw, err := argUint(args, 2)
	if err != nil {
		return nil, err
	}
	amount := settlekeeper.MaxAllowance
	if raw.BitLen() <= maxAmountBits && math.NewIntFromBigInt(raw).LT(settlekeeper.MaxAllowance) {
		amount = math.NewIntFromBigInt(raw)
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := p.settle.ApproveApp(ctx, acc(caller), appID, denom, amount); err != nil {
		return nil, err
	}
	if err := p.emit(ctx, stateDB, "AppApproval", []interface{}{caller, appID}, token, amount.BigInt()); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) appAllowance(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	owner, err := argAddress(args, 0)
	if err != nil {
		return nil, err
	}
	appID, err := argUint64(args, 1)
	if err != nil {
		return nil, err
	}
	token, err := argAddress(args, 2)
	if err != nil {
		return nil, err
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(p.settle.Allowance(ctx, acc(owner).String(), appID, denom).BigInt())
}

// registerApp(revenueRecipient, metadataUri, referrerBps): owner = msg.sender.
func (p Precompile) registerApp(ctx sdk.Context, stateDB vm.StateDB, caller common.Address, method *abi.Method, args []interface{}) ([]byte, error) {
	recipient, err := argAddress(args, 0)
	if err != nil {
		return nil, err
	}
	if len(args) < 3 {
		return nil, errBadArgs
	}
	uri, ok := args[1].(string)
	if !ok {
		return nil, errBadArgs
	}
	refBps, ok := args[2].(uint32)
	if !ok {
		return nil, errBadArgs
	}
	rec := ""
	if recipient != (common.Address{}) {
		rec = acc(recipient).String()
	}
	id, err := p.apps.RegisterApp(ctx, acc(caller), rec, uri, "", refBps)
	if err != nil {
		return nil, err
	}
	if err := p.emit(ctx, stateDB, "AppRegistered", []interface{}{id, caller}); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(id)
}

// claimRegistration(appId): the CALLING CONTRACT asks to join appId.
func (p Precompile) claimRegistration(ctx sdk.Context, stateDB vm.StateDB, caller common.Address, method *abi.Method, args []interface{}) ([]byte, error) {
	appID, err := argUint64(args, 0)
	if err != nil {
		return nil, err
	}
	if err := p.apps.ClaimRegistration(ctx, caller, appID); err != nil {
		return nil, err
	}
	if err := p.emit(ctx, stateDB, "RegistrationClaimed", []interface{}{appID, caller}); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

// acceptContractClaim(appId, contract): msg.sender must own the app.
func (p Precompile) acceptContractClaim(ctx sdk.Context, caller common.Address, method *abi.Method, args []interface{}) ([]byte, error) {
	appID, err := argUint64(args, 0)
	if err != nil {
		return nil, err
	}
	contractAddr, err := argAddress(args, 1)
	if err != nil {
		return nil, err
	}
	pending, _, err := p.apps.AcceptContractClaim(ctx, acc(caller).String(), appID, contractAddr.Hex())
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(pending)
}

// bondApp(appId, token, amount): the app owner (msg.sender) locks capital
// behind the app; base sponsorship quota is linear in it (x/apps BaseQuota).
func (p Precompile) bondApp(ctx sdk.Context, caller common.Address, method *abi.Method, args []interface{}) ([]byte, error) {
	appID, coin, err := p.bondArgs(ctx, args)
	if err != nil {
		return nil, err
	}
	if err := p.apps.BondApp(ctx, acc(caller).String(), appID, coin); err != nil {
		return nil, err
	}
	bonded, _ := p.apps.BondInfo(ctx, appID)
	return method.Outputs.Pack(bonded.BigInt())
}

// unbondApp(appId, token, amount): stops counting now; released to the owner
// after the unbonding period. Returns the release height.
func (p Precompile) unbondApp(ctx sdk.Context, caller common.Address, method *abi.Method, args []interface{}) ([]byte, error) {
	appID, coin, err := p.bondArgs(ctx, args)
	if err != nil {
		return nil, err
	}
	release, err := p.apps.UnbondApp(ctx, acc(caller).String(), appID, coin)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(uint64(release)) //nolint:gosec // block heights are positive
}

func (p Precompile) bondArgs(ctx sdk.Context, args []interface{}) (uint64, sdk.Coin, error) {
	appID, err := argUint64(args, 0)
	if err != nil {
		return 0, sdk.Coin{}, err
	}
	token, err := argAddress(args, 1)
	if err != nil {
		return 0, sdk.Coin{}, err
	}
	amount, err := argAmount(args, 2)
	if err != nil {
		return 0, sdk.Coin{}, err
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return 0, sdk.Coin{}, err
	}
	return appID, sdk.NewCoin(denom, amount), nil
}

// appBond(appId) view: bonded capital and the base quota it buys per epoch.
func (p Precompile) appBond(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	appID, err := argUint64(args, 0)
	if err != nil {
		return nil, err
	}
	bonded, base := p.apps.BondInfo(ctx, appID)
	return method.Outputs.Pack(bonded.BigInt(), base)
}

func (p Precompile) appOf(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	c, err := argAddress(args, 0)
	if err != nil {
		return nil, err
	}
	app, ok := p.apps.AppOfContract(ctx, c)
	if !ok {
		return method.Outputs.Pack(uint64(0), false)
	}
	return method.Outputs.Pack(app.AppId, true)
}

// claim(appId, token): pays claimable revenue to the app's recipient.
func (p Precompile) claim(ctx sdk.Context, stateDB vm.StateDB, method *abi.Method, args []interface{}) ([]byte, error) {
	appID, err := argUint64(args, 0)
	if err != nil {
		return nil, err
	}
	token, err := argAddress(args, 1)
	if err != nil {
		return nil, err
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	app, err := p.apps.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	claimed, err := p.settle.ClaimRevenue(ctx, appID, []string{denom})
	if err != nil {
		return nil, err
	}
	amt := claimed.AmountOf(denom)
	recipient, err := sdk.AccAddressFromBech32(app.RevenueRecipient)
	if err != nil {
		return nil, err
	}
	if err := p.emit(ctx, stateDB, "RevenueClaimed", []interface{}{appID, common.BytesToAddress(recipient)}, token, amt.BigInt()); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(amt.BigInt())
}

func (p Precompile) claimable(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	appID, err := argUint64(args, 0)
	if err != nil {
		return nil, err
	}
	token, err := argAddress(args, 1)
	if err != nil {
		return nil, err
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(p.settle.ClaimableOf(ctx, appID, denom).BigInt())
}

// buyCredits(token, amount): credits are minted to msg.sender. The balance
// handler mirrors the mint into the EVM StateDB so the new CREDIT balance is
// visible for the rest of the transaction.
func (p Precompile) buyCredits(ctx sdk.Context, stateDB vm.StateDB, caller common.Address, method *abi.Method, args []interface{}) ([]byte, error) {
	token, err := argAddress(args, 0)
	if err != nil {
		return nil, err
	}
	amount, err := argAmount(args, 1)
	if err != nil {
		return nil, err
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	out, err := p.settle.BuyCredits(ctx, acc(caller), acc(caller), sdk.NewCoin(denom, amount))
	if err != nil {
		return nil, err
	}
	if err := p.emit(ctx, stateDB, "CreditsBought", []interface{}{caller, caller}, token, amount.BigInt(), out.Amount.BigInt()); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(out.Amount.BigInt())
}

func (p Precompile) quoteCredits(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	token, err := argAddress(args, 0)
	if err != nil {
		return nil, err
	}
	amount, err := argAmount(args, 1)
	if err != nil {
		return nil, err
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	c, err := p.settle.QuoteCredits(ctx, denom, amount)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(c.BigInt())
}

func (p Precompile) quoteFee(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	token, err := argAddress(args, 0)
	if err != nil {
		return nil, err
	}
	amount, err := argAmount(args, 1)
	if err != nil {
		return nil, err
	}
	denom, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	params := p.settle.GetParams(ctx)
	asset, ok := params.Asset(denom)
	if !ok || !asset.Enabled {
		return nil, fmt.Errorf("settle: %s is not an enabled settlement asset", denom)
	}
	fee := settleFee(amount, asset, params.FeeBps)
	return method.Outputs.Pack(fee.BigInt(), amount.Sub(fee).BigInt())
}

func (p Precompile) denomOf(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	token, err := argAddress(args, 0)
	if err != nil {
		return nil, err
	}
	d, err := p.denomOfToken(ctx, token)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(d)
}

func (p Precompile) provenance(method *abi.Method) ([]byte, error) {
	return method.Outputs.Pack(provenance.Bytes())
}
