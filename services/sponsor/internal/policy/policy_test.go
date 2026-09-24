// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4

package policy

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/muthu2201/vapor-chain/services/sponsor/internal/chain"
	"github.com/muthu2201/vapor-chain/services/sponsor/internal/userop"
)

var (
	ep        = common.HexToAddress("0x4337084D9E255Ff0702461CF8895CE9E3b5Ff108")
	settle    = common.HexToAddress("0x0000000000000000000000000000000000000900")
	impl7702  = common.HexToAddress("0x4Cd241E8d1510e30b2076397afc7508Ae59C66c9")
	rogueImpl = common.HexToAddress("0x000000000000000000000000000000000000bad1")
	shop      = common.HexToAddress("0x00000000000000000000000000000000000005a0")
	otherApp  = common.HexToAddress("0x00000000000000000000000000000000000005a1")
	factory   = common.HexToAddress("0x00000000000000000000000000000000000000fa")
	sender    = common.HexToAddress("0x93385970124281998783D037704AAA53a559BBfd")
	chainID   = big.NewInt(779700)
)

type fakeChain struct {
	apps  map[uint64]chain.App
	owner map[common.Address]uint64
	code  map[common.Address][]byte
	fail  bool
}

func (f *fakeChain) App(_ context.Context, id uint64) (chain.App, error) {
	if f.fail {
		return chain.App{}, errors.New("rest down")
	}
	a, ok := f.apps[id]
	if !ok {
		return chain.App{}, chain.ErrNotFound
	}
	return a, nil
}
func (f *fakeChain) AppOfContract(_ context.Context, c common.Address) (uint64, error) {
	return f.owner[c], nil
}
func (f *fakeChain) Quota(context.Context, uint64) (chain.Quota, error) {
	return chain.Quota{Epoch: 3, Gas: 50_000_000}, nil
}
func (f *fakeChain) Code(_ context.Context, a common.Address) ([]byte, error) { return f.code[a], nil }

func delegation(impl common.Address) []byte { return append([]byte{0xef, 0x01, 0x00}, impl.Bytes()...) }

func newEngine() (*Engine, *fakeChain) {
	fc := &fakeChain{
		apps: map[uint64]chain.App{
			6: {AppID: 6, Status: "APP_STATUS_ACTIVE"},
			7: {AppID: 7, Status: "APP_STATUS_ACTIVE"},
			8: {AppID: 8, Status: "APP_STATUS_SUSPENDED"},
		},
		owner: map[common.Address]uint64{shop: 6, otherApp: 7},
		code:  map[common.Address][]byte{},
	}
	return New(Config{
		ChainID: chainID, EntryPoint: ep, Settle: settle,
		MaxGasPerOp: 2_000_000, MaxFeePerGas: big.NewInt(100e9),
		SenderDailyCap: 50, NewSenderDailyCap: 5,
		AllowedDelegates: map[common.Address]bool{impl7702: true},
		AllowedFactories: map[common.Address]bool{factory: true},
	}, fc), fc
}

var accountABI, _ = abi.JSON(strings.NewReader(`[
 {"type":"function","name":"execute","inputs":[{"name":"target","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"}]},
 {"type":"function","name":"executeBatch","inputs":[{"name":"calls","type":"tuple[]","components":[{"name":"target","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"}]}]}
]`))

type call struct {
	Target common.Address
	Value  *big.Int
	Data   []byte
}

func baseOp(calls ...call) *userop.UserOperation {
	var cd []byte
	if len(calls) == 1 {
		cd, _ = accountABI.Pack("execute", calls[0].Target, calls[0].Value, calls[0].Data)
	} else {
		cd, _ = accountABI.Pack("executeBatch", calls)
	}
	h := func(v int64) *hexutil.Big { return (*hexutil.Big)(big.NewInt(v)) }
	return &userop.UserOperation{
		Sender: sender, Nonce: h(0), CallData: cd,
		CallGasLimit: h(200_000), VerificationGasLimit: h(300_000), PreVerificationGas: h(60_000),
		MaxFeePerGas: h(2e9), MaxPriorityFeePerGas: h(1e9),
		PaymasterVerificationGasLimit: h(80_000), PaymasterPostOpGasLimit: h(25_000),
	}
}

func approveApp(appWord [32]byte) []byte {
	sel := []byte{0xba, 0x30, 0x14, 0xbf}
	out := append(sel, appWord[:]...)
	out = append(out, make([]byte, 64)...) // spender, cap
	return out
}

func word(id uint64) (w [32]byte) {
	new(big.Int).SetUint64(id).FillBytes(w[:])
	return
}

func eval(t *testing.T, e *Engine, op *userop.UserOperation, app uint64) (Decision, error) {
	t.Helper()
	return e.Evaluate(context.Background(), op, ep, chainID, app)
}

func wantDeny(t *testing.T, err error, substr string) {
	t.Helper()
	var pe *PolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("want policy denial containing %q, got %v", substr, err)
	}
	if !strings.Contains(pe.Reason, substr) {
		t.Fatalf("denial %q does not mention %q", pe.Reason, substr)
	}
}

func TestSponsorsCallsIntoOwnApp(t *testing.T) {
	e, _ := newEngine()
	op := baseOp(call{shop, big.NewInt(0), []byte{1}}, call{settle, big.NewInt(0), approveApp(word(6))})
	d, err := eval(t, e, op, 6)
	if err != nil {
		t.Fatal(err)
	}
	if d.AppID != 6 || d.Epoch != 3 || d.SenderCap != 5 /* brand-new sender */ {
		t.Fatalf("decision %+v", d)
	}
}

func TestRefusesSpendingQuotaOutsideTheApp(t *testing.T) {
	e, _ := newEngine()
	_, err := eval(t, e, baseOp(call{otherApp, big.NewInt(0), nil}), 6)
	wantDeny(t, err, "not a contract of app 6")
	_, err = eval(t, e, baseOp(call{common.HexToAddress("0xdead"), big.NewInt(0), nil}), 6)
	wantDeny(t, err, "not a contract of app 6")
	// one bad call poisons a batch
	_, err = eval(t, e, baseOp(call{shop, big.NewInt(0), nil}, call{otherApp, big.NewInt(0), nil}), 6)
	wantDeny(t, err, "not a contract of app 6")
}

func TestRefusesNativeValue(t *testing.T) {
	e, _ := newEngine()
	_, err := eval(t, e, baseOp(call{shop, big.NewInt(1), nil}), 6)
	wantDeny(t, err, "native value")
}

func TestSettleOnlyApproveAppForSameApp(t *testing.T) {
	e, _ := newEngine()
	_, err := eval(t, e, baseOp(call{settle, big.NewInt(0), approveApp(word(7))}), 6)
	wantDeny(t, err, "sponsoring app")
	// dirty high bytes that truncate to 6 must not pass
	w := word(6)
	w[0] = 0x01
	_, err = eval(t, e, baseOp(call{settle, big.NewInt(0), approveApp(w)}), 6)
	wantDeny(t, err, "sponsoring app")
	// any other Settle method (e.g. pay, withdraw) is refused
	_, err = eval(t, e, baseOp(call{settle, big.NewInt(0), append([]byte{1, 2, 3, 4}, make([]byte, 96)...)}), 6)
	wantDeny(t, err, "only SETTLE.approveApp")
}

func TestAppStatus(t *testing.T) {
	e, fc := newEngine()
	_, err := eval(t, e, baseOp(call{shop, big.NewInt(0), nil}), 8)
	wantDeny(t, err, "APP_STATUS_SUSPENDED")
	_, err = eval(t, e, baseOp(call{shop, big.NewInt(0), nil}), 99)
	wantDeny(t, err, "does not exist")
	// infrastructure failure is NOT a policy denial (maps to -32603, retryable)
	fc.fail = true
	_, err = eval(t, e, baseOp(call{shop, big.NewInt(0), nil}), 6)
	var pe *PolicyError
	if err == nil || errors.As(err, &pe) {
		t.Fatalf("want internal error, got %v", err)
	}
}

func TestEnvelopeChecks(t *testing.T) {
	e, _ := newEngine()
	op := baseOp(call{shop, big.NewInt(0), nil})
	if _, err := e.Evaluate(context.Background(), op, common.HexToAddress("0x01"), chainID, 6); err == nil {
		t.Fatal("foreign entrypoint accepted")
	}
	if _, err := e.Evaluate(context.Background(), op, ep, big.NewInt(1), 6); err == nil {
		t.Fatal("foreign chain accepted")
	}
	if _, err := e.Evaluate(context.Background(), op, ep, chainID, 0); err == nil {
		t.Fatal("app 0 accepted")
	}
	op.MaxFeePerGas = (*hexutil.Big)(big.NewInt(101e9))
	_, err := eval(t, e, op, 6)
	wantDeny(t, err, "maxFeePerGas")
	op = baseOp(call{shop, big.NewInt(0), nil})
	op.CallGasLimit = (*hexutil.Big)(big.NewInt(5_000_000))
	_, err = eval(t, e, op, 6)
	wantDeny(t, err, "per-op cap")
}

func TestFactoryAllowlist(t *testing.T) {
	e, _ := newEngine()
	op := baseOp(call{shop, big.NewInt(0), nil})
	op.Factory = userop.Factory(common.HexToAddress("0x0bad").Bytes())
	_, err := eval(t, e, op, 6)
	wantDeny(t, err, "not allowlisted")
	op.Factory = userop.Factory(factory.Bytes())
	if _, err := eval(t, e, op, 6); err != nil {
		t.Fatal(err)
	}
}

func auth(impl common.Address) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"address": impl.Hex(), "chainId": "0xbe5b4", "nonce": "0x0"})
	return b
}

func TestEIP7702Policy(t *testing.T) {
	e, fc := newEngine()
	op := baseOp(call{shop, big.NewInt(0), nil})
	op.Factory = userop.Factory{0x77, 0x02}

	// first-time 7702: authorization must point at an allowlisted delegate
	op.EIP7702Auth = auth(rogueImpl)
	_, err := eval(t, e, op, 6)
	wantDeny(t, err, "eip7702Auth")
	op.EIP7702Auth = auth(impl7702)
	if _, err := eval(t, e, op, 6); err != nil {
		t.Fatal(err)
	}

	// marker with neither auth nor existing delegation cannot validate
	op.EIP7702Auth = nil
	_, err = eval(t, e, op, 6)
	wantDeny(t, err, "0x7702 marker")

	// already delegated to the allowlisted implementation
	fc.code[sender] = delegation(impl7702)
	if _, err := eval(t, e, op, 6); err != nil {
		t.Fatal(err)
	}
	// already delegated to a drainer: refused even without a factory
	fc.code[sender] = delegation(rogueImpl)
	op.Factory = nil
	_, err = eval(t, e, op, 6)
	wantDeny(t, err, "non-allowlisted implementation")
}

func TestUndecodableCallDataFailsClosed(t *testing.T) {
	e, _ := newEngine()
	op := baseOp(call{shop, big.NewInt(0), nil})
	op.CallData = []byte{0x12, 0x34, 0x56, 0x78}
	_, err := eval(t, e, op, 6)
	wantDeny(t, err, "unsupported account callData")
}

func TestBatchSizeBound(t *testing.T) {
	e, _ := newEngine()
	calls := make([]call, 17)
	for i := range calls {
		calls[i] = call{shop, big.NewInt(0), nil}
	}
	_, err := eval(t, e, baseOp(calls...), 6)
	wantDeny(t, err, "between 1 and 16")
}
