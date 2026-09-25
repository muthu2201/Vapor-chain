// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

package keeper

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/muthu2201/vapor-chain/chain/x/apps/types"
)

// ownerSelector is keccak256("owner()")[:4].
var ownerSelector = []byte{0x8d, 0xa5, 0xcb, 0x5b}

// ownableGasCap bounds the static owner() call used as an ownership proof so
// a hostile contract cannot burn the tx's gas.
var ownableGasCap = big.NewInt(100_000)

// Keeper of x/apps: the application registry and sponsorship-quota engine.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	authority    string

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	evmKeeper     types.EVMKeeper

	Schema        collections.Schema
	Params        collections.Item[types.Params]
	NextAppID     collections.Sequence
	Apps          collections.Map[uint64, types.App]
	Bindings      collections.Map[string, types.ContractBinding]
	AppContracts  collections.KeySet[collections.Pair[uint64, string]]
	PendingMoves  collections.Map[string, types.PendingMove]
	MovesByUnlock collections.KeySet[collections.Pair[int64, string]]
	PendingClaims collections.Map[collections.Pair[uint64, string], types.PendingClaim]
	EpochStats    collections.Map[collections.Pair[uint64, uint64], types.EpochStats]
	Quotas        collections.Map[uint64, types.Quota]
	Attestations  collections.Map[collections.Pair[uint64, string], types.DomainAttestation]
	EpochNumber   collections.Item[uint64]
	EpochStart    collections.Item[int64]
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	ek types.EVMKeeper,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Errorf("apps: invalid authority %q: %w", authority, err))
	}
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc:           cdc,
		storeService:  storeService,
		authority:     authority,
		accountKeeper: ak,
		bankKeeper:    bk,
		evmKeeper:     ek,
		Params:        collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		NextAppID:     collections.NewSequence(sb, types.NextAppIDKey, "next_app_id"),
		Apps:          collections.NewMap(sb, types.AppsKey, "apps", collections.Uint64Key, codec.CollValue[types.App](cdc)),
		Bindings:      collections.NewMap(sb, types.BindingsKey, "bindings", collections.StringKey, codec.CollValue[types.ContractBinding](cdc)),
		AppContracts:  collections.NewKeySet(sb, types.AppContractsKey, "app_contracts", collections.PairKeyCodec(collections.Uint64Key, collections.StringKey)),
		PendingMoves:  collections.NewMap(sb, types.PendingMovesKey, "pending_moves", collections.StringKey, codec.CollValue[types.PendingMove](cdc)),
		MovesByUnlock: collections.NewKeySet(sb, types.MovesByUnlockKey, "moves_by_unlock", collections.PairKeyCodec(collections.Int64Key, collections.StringKey)),
		PendingClaims: collections.NewMap(sb, types.PendingClaimsKey, "pending_claims", collections.PairKeyCodec(collections.Uint64Key, collections.StringKey), codec.CollValue[types.PendingClaim](cdc)),
		EpochStats:    collections.NewMap(sb, types.EpochStatsKey, "epoch_stats", collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key), codec.CollValue[types.EpochStats](cdc)),
		Quotas:        collections.NewMap(sb, types.QuotasKey, "quotas", collections.Uint64Key, codec.CollValue[types.Quota](cdc)),
		Attestations:  collections.NewMap(sb, types.AttestationsKey, "attestations", collections.PairKeyCodec(collections.Uint64Key, collections.StringKey), codec.CollValue[types.DomainAttestation](cdc)),
		EpochNumber:   collections.NewItem(sb, types.EpochNumberKey, "epoch_number", collections.Uint64Value),
		EpochStart:    collections.NewItem(sb, types.EpochStartKey, "epoch_start", collections.Int64Value),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema
	return k
}

func (k Keeper) Authority() string { return k.authority }

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	p, err := k.Params.Get(ctx)
	if err != nil {
		return types.DefaultParams()
	}
	return p
}

// SetParams validates and stores params (tests/genesis).
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return k.Params.Set(ctx, p)
}

// GetApp returns an app or ErrAppNotFound.
func (k Keeper) GetApp(ctx sdk.Context, appID uint64) (types.App, error) {
	app, err := k.Apps.Get(ctx, appID)
	if errors.Is(err, collections.ErrNotFound) {
		return app, types.ErrAppNotFound.Wrapf("app %d", appID)
	}
	return app, err
}

func (k Keeper) ownedApp(ctx sdk.Context, owner string, appID uint64) (types.App, error) {
	app, err := k.GetApp(ctx, appID)
	if err != nil {
		return app, err
	}
	if app.Owner != owner {
		return app, types.ErrUnauthorized.Wrapf("signer %s does not own app %d", owner, appID)
	}
	if app.Status == types.APP_STATUS_REVOKED {
		return app, types.ErrAppInactive.Wrap("app is revoked")
	}
	return app, nil
}

func validateURI(uri string) error {
	if len(uri) > types.MaxMetadataURILen {
		return types.ErrInvalidField.Wrapf("metadata_uri longer than %d", types.MaxMetadataURILen)
	}
	return nil
}

func validateDomain(d string) error {
	if d == "" {
		return nil
	}
	if len(d) > types.MaxDomainLen || strings.Count(d, ".") == 0 {
		return types.ErrInvalidField.Wrap("domain must be a fully-qualified hostname")
	}
	for _, c := range d {
		if !(c == '.' || c == '-' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return types.ErrInvalidField.Wrap("domain must be lowercase [a-z0-9.-]")
		}
	}
	return nil
}

func (k Keeper) validateRecipient(addr string) error {
	acc, err := sdk.AccAddressFromBech32(addr)
	if err != nil {
		return types.ErrInvalidField.Wrapf("revenue_recipient: %v", err)
	}
	if k.bankKeeper.BlockedAddr(acc) {
		return types.ErrInvalidField.Wrap("revenue_recipient is a blocked (module/precompile) address")
	}
	return nil
}

// RegisterApp creates a new app. The registration fee is burned.
func (k Keeper) RegisterApp(ctx sdk.Context, owner sdk.AccAddress, recipient, metadataURI, domain string, referrerBps uint32) (uint64, error) {
	params := k.GetParams(ctx)
	if recipient == "" {
		recipient = owner.String()
	}
	if err := k.validateRecipient(recipient); err != nil {
		return 0, err
	}
	if err := validateURI(metadataURI); err != nil {
		return 0, err
	}
	if err := validateDomain(domain); err != nil {
		return 0, err
	}
	if referrerBps > params.MaxReferrerBps {
		return 0, types.ErrInvalidField.Wrapf("referrer_bps %d > max %d", referrerBps, params.MaxReferrerBps)
	}
	if params.RegistrationFee.IsPositive() {
		fee := sdk.NewCoins(params.RegistrationFee)
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, owner, types.ModuleName, fee); err != nil {
			return 0, err
		}
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, fee); err != nil {
			return 0, err
		}
	}
	id, err := k.NextAppID.Next(ctx)
	if err != nil {
		return 0, err
	}
	if id == 0 { // app id 0 is the reserved no-app bucket
		if id, err = k.NextAppID.Next(ctx); err != nil {
			return 0, err
		}
	}
	app := types.App{
		AppId:            id,
		Owner:            owner.String(),
		RevenueRecipient: recipient,
		MetadataUri:      metadataURI,
		Domain:           domain,
		ReferrerBps:      referrerBps,
		Status:           types.APP_STATUS_ACTIVE,
		Version:          1,
		CreatedHeight:    ctx.BlockHeight(),
	}
	if err := k.Apps.Set(ctx, id, app); err != nil {
		return 0, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("app_registered",
		sdk.NewAttribute("app_id", fmt.Sprint(id)),
		sdk.NewAttribute("owner", app.Owner),
		sdk.NewAttribute("recipient", recipient),
	))
	return id, nil
}

// UpdateApp replaces mutable fields. Changing the domain resets verification.
func (k Keeper) UpdateApp(ctx sdk.Context, owner string, appID uint64, recipient, metadataURI, domain string, referrerBps uint32) error {
	app, err := k.ownedApp(ctx, owner, appID)
	if err != nil {
		return err
	}
	params := k.GetParams(ctx)
	if recipient != "" {
		if err := k.validateRecipient(recipient); err != nil {
			return err
		}
		app.RevenueRecipient = recipient
	}
	if err := validateURI(metadataURI); err != nil {
		return err
	}
	if err := validateDomain(domain); err != nil {
		return err
	}
	if referrerBps > params.MaxReferrerBps {
		return types.ErrInvalidField.Wrapf("referrer_bps %d > max %d", referrerBps, params.MaxReferrerBps)
	}
	app.MetadataUri = metadataURI
	if domain != app.Domain {
		app.Domain = domain
		app.DomainVerified = false
	}
	app.ReferrerBps = referrerBps
	app.Version++
	if err := k.Apps.Set(ctx, appID, app); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("app_updated", sdk.NewAttribute("app_id", fmt.Sprint(appID))))
	return nil
}

func (k Keeper) TransferOwnership(ctx sdk.Context, owner string, appID uint64, newOwner string) error {
	app, err := k.ownedApp(ctx, owner, appID)
	if err != nil {
		return err
	}
	if _, err := sdk.AccAddressFromBech32(newOwner); err != nil {
		return types.ErrInvalidField.Wrapf("new_owner: %v", err)
	}
	app.PendingOwner = newOwner
	return k.Apps.Set(ctx, appID, app)
}

func (k Keeper) AcceptOwnership(ctx sdk.Context, newOwner string, appID uint64) error {
	app, err := k.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	if app.PendingOwner == "" || app.PendingOwner != newOwner {
		return types.ErrNoPendingOwner
	}
	old := app.Owner
	app.Owner = newOwner
	app.PendingOwner = ""
	app.Version++
	if err := k.Apps.Set(ctx, appID, app); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("app_ownership_transferred",
		sdk.NewAttribute("app_id", fmt.Sprint(appID)),
		sdk.NewAttribute("from", old),
		sdk.NewAttribute("to", newOwner),
	))
	return nil
}

// VerifyProof checks a contract-ownership proof for signer.
func (k Keeper) VerifyProof(ctx sdk.Context, signer common.Address, contract common.Address, proof types.OwnershipProof) error {
	switch proof.Type {
	case types.PROOF_TYPE_DEPLOYER_CREATE:
		if crypto.CreateAddress(signer, proof.Nonce) != contract {
			return types.ErrInvalidProof.Wrap("CREATE address mismatch")
		}
		return nil
	case types.PROOF_TYPE_DEPLOYER_CREATE2:
		salt := common.FromHex(proof.Salt)
		ich := common.FromHex(proof.InitCodeHash)
		if len(salt) != 32 || len(ich) != 32 {
			return types.ErrInvalidProof.Wrap("salt and init_code_hash must be 32 bytes")
		}
		var s32 [32]byte
		copy(s32[:], salt)
		if crypto.CreateAddress2(signer, s32, ich) != contract {
			return types.ErrInvalidProof.Wrap("CREATE2 address mismatch")
		}
		return nil
	case types.PROOF_TYPE_OWNABLE:
		if !k.evmKeeper.IsContract(ctx, contract) {
			return types.ErrNotContract
		}
		// the module account is created on first use so it has a sequence
		from := common.BytesToAddress(k.accountKeeper.GetModuleAccount(ctx, types.ModuleName).GetAddress())
		// Run the call in a branched context so a malicious owner() can never
		// commit state, and cap its gas.
		cacheCtx, _ := ctx.CacheContext()
		res, err := k.evmKeeper.CallEVMWithData(cacheCtx, nil, from, &contract, ownerSelector, false, false, ownableGasCap)
		if err != nil {
			return types.ErrInvalidProof.Wrapf("owner() call failed: %v", err)
		}
		if len(res.Ret) < 32 {
			return types.ErrInvalidProof.Wrap("owner() returned malformed data")
		}
		if common.BytesToAddress(res.Ret[12:32]) != signer {
			return types.ErrInvalidProof.Wrap("owner() is not the signer")
		}
		return nil
	default:
		return types.ErrInvalidProof.Wrapf("unsupported proof type %s", proof.Type)
	}
}

// bindContract attributes contract to appID, or schedules a timelocked move if
// the contract already belongs to another app.
func (k Keeper) bindContract(ctx sdk.Context, contract string, appID uint64, proofType types.ProofType) (bool, int64, error) {
	params := k.GetParams(ctx)
	app, err := k.GetApp(ctx, appID)
	if err != nil {
		return false, 0, err
	}
	if app.ContractCount >= params.MaxContractsPerApp {
		return false, 0, types.ErrTooManyContracts
	}
	existing, err := k.Bindings.Get(ctx, contract)
	switch {
	case errors.Is(err, collections.ErrNotFound):
		// fresh binding
	case err != nil:
		return false, 0, err
	case existing.AppId == appID:
		return false, 0, types.ErrAlreadyBound
	default:
		if has, _ := k.PendingMoves.Has(ctx, contract); has {
			return false, 0, types.ErrMovePending
		}
		unlock := ctx.BlockHeight() + params.ContractMoveDelayBlocks
		mv := types.PendingMove{Contract: contract, FromAppId: existing.AppId, ToAppId: appID, ProofType: proofType, UnlockHeight: unlock}
		if err := k.PendingMoves.Set(ctx, contract, mv); err != nil {
			return false, 0, err
		}
		if err := k.MovesByUnlock.Set(ctx, collections.Join(unlock, contract)); err != nil {
			return false, 0, err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent("app_contract_move_scheduled",
			sdk.NewAttribute("contract", contract),
			sdk.NewAttribute("from_app_id", fmt.Sprint(existing.AppId)),
			sdk.NewAttribute("to_app_id", fmt.Sprint(appID)),
			sdk.NewAttribute("unlock_height", fmt.Sprint(unlock)),
		))
		return true, unlock, nil
	}
	if err := k.setBinding(ctx, contract, appID, proofType); err != nil {
		return false, 0, err
	}
	return false, 0, nil
}

func (k Keeper) setBinding(ctx sdk.Context, contract string, appID uint64, proofType types.ProofType) error {
	app, err := k.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	b := types.ContractBinding{Contract: contract, AppId: appID, ProofType: proofType, AddedHeight: ctx.BlockHeight(), Version: app.Version}
	if err := k.Bindings.Set(ctx, contract, b); err != nil {
		return err
	}
	if err := k.AppContracts.Set(ctx, collections.Join(appID, contract)); err != nil {
		return err
	}
	app.ContractCount++
	if err := k.Apps.Set(ctx, appID, app); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("app_contract_bound",
		sdk.NewAttribute("contract", contract),
		sdk.NewAttribute("app_id", fmt.Sprint(appID)),
		sdk.NewAttribute("proof", proofType.String()),
	))
	return nil
}

func (k Keeper) unbind(ctx sdk.Context, contract string, appID uint64) error {
	if err := k.Bindings.Remove(ctx, contract); err != nil {
		return err
	}
	if err := k.AppContracts.Remove(ctx, collections.Join(appID, contract)); err != nil {
		return err
	}
	app, err := k.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	if app.ContractCount > 0 {
		app.ContractCount--
	}
	return k.Apps.Set(ctx, appID, app)
}

// AddContract proves ownership and binds (or schedules moving) a contract.
func (k Keeper) AddContract(ctx sdk.Context, owner sdk.AccAddress, appID uint64, contractHex string, proof types.OwnershipProof) (bool, int64, error) {
	if _, err := k.ownedApp(ctx, owner.String(), appID); err != nil {
		return false, 0, err
	}
	if !common.IsHexAddress(contractHex) {
		return false, 0, types.ErrInvalidField.Wrap("contract must be a 0x address")
	}
	contract := common.HexToAddress(contractHex)
	if proof.Type == types.PROOF_TYPE_SELF_CLAIM {
		return false, 0, types.ErrInvalidProof.Wrap("self-claims are accepted with MsgAcceptContractClaim")
	}
	if err := k.VerifyProof(ctx, common.BytesToAddress(owner), contract, proof); err != nil {
		return false, 0, err
	}
	return k.bindContract(ctx, types.NormalizeContract(contractHex), appID, proof.Type)
}

// ClaimRegistration is called by the Settle precompile when a contract calls
// SETTLE.claimRegistration(appId). It only records a pending claim: the app
// owner must accept it, otherwise any contract could attach itself to a
// victim's app and consume the victim's sponsorship quota.
func (k Keeper) ClaimRegistration(ctx sdk.Context, contract common.Address, appID uint64) error {
	app, err := k.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	if app.Status != types.APP_STATUS_ACTIVE {
		return types.ErrAppInactive
	}
	c := types.NormalizeContract(contract.Hex())
	return k.PendingClaims.Set(ctx, collections.Join(appID, c), types.PendingClaim{Contract: c, AppId: appID, ClaimedHeight: ctx.BlockHeight()})
}

func (k Keeper) AcceptContractClaim(ctx sdk.Context, owner string, appID uint64, contractHex string) (bool, int64, error) {
	if _, err := k.ownedApp(ctx, owner, appID); err != nil {
		return false, 0, err
	}
	c := types.NormalizeContract(contractHex)
	key := collections.Join(appID, c)
	if has, _ := k.PendingClaims.Has(ctx, key); !has {
		return false, 0, types.ErrNoPendingClaim
	}
	if err := k.PendingClaims.Remove(ctx, key); err != nil {
		return false, 0, err
	}
	return k.bindContract(ctx, c, appID, types.PROOF_TYPE_SELF_CLAIM)
}

func (k Keeper) RemoveContract(ctx sdk.Context, owner string, appID uint64, contractHex string) error {
	if _, err := k.ownedApp(ctx, owner, appID); err != nil {
		return err
	}
	c := types.NormalizeContract(contractHex)
	b, err := k.Bindings.Get(ctx, c)
	if err != nil || b.AppId != appID {
		return types.ErrNotBound
	}
	if mv, err := k.PendingMoves.Get(ctx, c); err == nil {
		_ = k.MovesByUnlock.Remove(ctx, collections.Join(mv.UnlockHeight, c))
		_ = k.PendingMoves.Remove(ctx, c)
	}
	return k.unbind(ctx, c, appID)
}

// CancelContractMove lets the owner of the app that CURRENTLY holds the
// contract veto a pending move (the timelock's purpose).
func (k Keeper) CancelContractMove(ctx sdk.Context, owner, contractHex string) error {
	c := types.NormalizeContract(contractHex)
	mv, err := k.PendingMoves.Get(ctx, c)
	if err != nil {
		return types.ErrNoPendingMove
	}
	from, err := k.GetApp(ctx, mv.FromAppId)
	if err != nil {
		return err
	}
	to, _ := k.GetApp(ctx, mv.ToAppId)
	if from.Owner != owner && to.Owner != owner {
		return types.ErrUnauthorized.Wrap("only the current or the requesting app owner can cancel a move")
	}
	if err := k.MovesByUnlock.Remove(ctx, collections.Join(mv.UnlockHeight, c)); err != nil {
		return err
	}
	return k.PendingMoves.Remove(ctx, c)
}

func (k Keeper) finalizeMoves(ctx sdk.Context) error {
	rng := new(collections.Range[collections.Pair[int64, string]]).EndInclusive(collections.Join(ctx.BlockHeight(), "\xff"))
	var due []collections.Pair[int64, string]
	err := k.MovesByUnlock.Walk(ctx, rng, func(key collections.Pair[int64, string]) (bool, error) {
		due = append(due, key)
		return false, nil
	})
	if err != nil {
		return err
	}
	for _, key := range due {
		c := key.K2()
		if err := k.MovesByUnlock.Remove(ctx, key); err != nil {
			return err
		}
		mv, err := k.PendingMoves.Get(ctx, c)
		if err != nil {
			continue
		}
		if err := k.PendingMoves.Remove(ctx, c); err != nil {
			return err
		}
		to, err := k.GetApp(ctx, mv.ToAppId)
		if err != nil || to.Status == types.APP_STATUS_REVOKED {
			continue
		}
		// Re-check the cap at finalize time: the destination app may have added
		// contracts between scheduling and unlock. If it is now full, drop the
		// move (the contract stays with its current app) rather than exceeding
		// the cap.
		if to.ContractCount >= k.GetParams(ctx).MaxContractsPerApp {
			ctx.EventManager().EmitEvent(sdk.NewEvent("app_contract_move_dropped",
				sdk.NewAttribute("contract", c),
				sdk.NewAttribute("to_app_id", fmt.Sprint(mv.ToAppId)),
				sdk.NewAttribute("reason", "destination app is full")))
			continue
		}
		if b, err := k.Bindings.Get(ctx, c); err == nil {
			if err := k.unbind(ctx, c, b.AppId); err != nil {
				return err
			}
		}
		if err := k.setBinding(ctx, c, mv.ToAppId, mv.ProofType); err != nil {
			return err
		}
	}
	return nil
}

func (k Keeper) AttestDomain(ctx sdk.Context, attestor string, appID uint64, domain string) (bool, error) {
	params := k.GetParams(ctx)
	if !params.IsAttestor(attestor) {
		return false, types.ErrNotAttestor
	}
	app, err := k.GetApp(ctx, appID)
	if err != nil {
		return false, err
	}
	if app.Domain == "" || app.Domain != domain {
		return false, types.ErrDomainMismatch
	}
	if err := k.Attestations.Set(ctx, collections.Join(appID, attestor), types.DomainAttestation{AppId: appID, Domain: domain, Attestor: attestor, Height: ctx.BlockHeight()}); err != nil {
		return false, err
	}
	var count uint32
	err = k.Attestations.Walk(ctx, collections.NewPrefixedPairRange[uint64, string](appID), func(_ collections.Pair[uint64, string], a types.DomainAttestation) (bool, error) {
		if a.Domain == app.Domain && params.IsAttestor(a.Attestor) {
			count++
		}
		return false, nil
	})
	if err != nil {
		return false, err
	}
	if count >= params.AttestationThreshold && !app.DomainVerified {
		app.DomainVerified = true
		if err := k.Apps.Set(ctx, appID, app); err != nil {
			return false, err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent("app_domain_verified",
			sdk.NewAttribute("app_id", fmt.Sprint(appID)), sdk.NewAttribute("domain", domain)))
	}
	return app.DomainVerified, nil
}

func (k Keeper) SetAppStatus(ctx sdk.Context, appID uint64, status types.AppStatus) error {
	if status == types.APP_STATUS_UNSPECIFIED {
		return types.ErrInvalidField.Wrap("status unspecified")
	}
	app, err := k.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	if app.Status == types.APP_STATUS_REVOKED {
		return types.ErrAppInactive.Wrap("revocation is final")
	}
	app.Status = status
	app.Version++
	if err := k.Apps.Set(ctx, appID, app); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("app_status_changed",
		sdk.NewAttribute("app_id", fmt.Sprint(appID)), sdk.NewAttribute("status", status.String())))
	return nil
}

// AppOfContract resolves attribution for the Settle precompile: the CALLING
// contract (msg.sender of the precompile call) decides the app, never a
// user-supplied id, so attribution cannot be spoofed. Only ACTIVE apps earn.
func (k Keeper) AppOfContract(ctx sdk.Context, contract common.Address) (types.App, bool) {
	b, err := k.Bindings.Get(ctx, types.NormalizeContract(contract.Hex()))
	if err != nil {
		return types.App{}, false
	}
	app, err := k.GetApp(ctx, b.AppId)
	if err != nil || app.Status != types.APP_STATUS_ACTIVE {
		return types.App{}, false
	}
	return app, true
}

// CurrentEpoch returns (epoch number, start height).
func (k Keeper) CurrentEpoch(ctx sdk.Context) (uint64, int64) {
	n, err := k.EpochNumber.Get(ctx)
	if err != nil {
		n = 0
	}
	s, err := k.EpochStart.Get(ctx)
	if err != nil {
		s = ctx.BlockHeight()
	}
	return n, s
}

// RecordSettlement feeds the quota engine (called by x/settle for every
// attributed payment).
func (k Keeper) RecordSettlement(ctx sdk.Context, appID uint64, payer []byte, feeWeight math.Int) error {
	if appID == types.NoApp {
		return nil
	}
	epoch, _ := k.CurrentEpoch(ctx)
	key := collections.Join(epoch, appID)
	st, err := k.EpochStats.Get(ctx, key)
	if errors.Is(err, collections.ErrNotFound) {
		st = types.EpochStats{AppId: appID, Epoch: epoch, FeeWeight: math.ZeroInt(), Hll: types.NewHLL()}
	} else if err != nil {
		return err
	}
	st.FeeWeight = st.FeeWeight.Add(feeWeight)
	st.Payments++
	st.Hll = types.HLLAdd(st.Hll, payer)
	return k.EpochStats.Set(ctx, key, st)
}

// ComputeQuota applies:  quota = min(max, base + fee_weight * diversity)
// where diversity = min(1, unique_payers * diversity_target / payments) and
// base is BaseGasPerEpoch only for a domain-verified app (see BaseQuota).
func ComputeQuota(params types.Params, st types.EpochStats, verified bool) types.Quota {
	unique := types.HLLEstimate(st.Hll)
	if unique > st.Payments {
		unique = st.Payments
	}
	var diversityBps uint64
	if st.Payments > 0 {
		diversityBps = unique * uint64(params.DiversityTarget) * types.MaxBps / st.Payments
		if diversityBps > types.MaxBps {
			diversityBps = types.MaxBps
		}
	}
	earned := st.FeeWeight.Mul(math.NewIntFromUint64(diversityBps)).Quo(math.NewInt(types.MaxBps))
	total := math.NewIntFromUint64(BaseQuota(params, verified)).Add(earned)
	maxQ := math.NewIntFromUint64(params.MaxGasPerEpoch)
	if total.GT(maxQ) {
		total = maxQ
	}
	return types.Quota{AppId: st.AppId, Gas: total.Uint64(), UniquePayersEstimate: unique, DiversityBps: uint32(diversityBps)} //nolint:gosec // <= 10000
}

// BaseQuota is the protocol-sponsored gas an app gets per epoch without having
// paid any fees. It is granted ONLY to domain-verified apps: registration is
// cheap and permissionless, so an unconditional base would let anyone mint
// fake apps and have the protocol paymaster pay for their gas (the Sybil gap
// measured in docs/benchmarks/mainnet-sim.md, F-1). Verification is done by
// governance-appointed attestors, which is the anti-Sybil gate. Unverified
// apps still earn quota from the fees their contracts generate.
func BaseQuota(params types.Params, verified bool) uint64 {
	if !verified {
		return 0
	}
	return params.BaseGasPerEpoch
}

// QuotaFor returns the app's quota for the current epoch (its base quota if it
// earned nothing last epoch).
func (k Keeper) QuotaFor(ctx sdk.Context, appID uint64) types.Quota {
	epoch, _ := k.CurrentEpoch(ctx)
	app, err := k.GetApp(ctx, appID)
	if err != nil || app.Status != types.APP_STATUS_ACTIVE {
		return types.Quota{AppId: appID, Epoch: epoch}
	}
	q, err := k.Quotas.Get(ctx, appID)
	if err == nil && q.Epoch == epoch {
		return q
	}
	return types.Quota{AppId: appID, Epoch: epoch, Gas: BaseQuota(k.GetParams(ctx), app.DomainVerified)}
}

// rolloverEpoch finalizes quotas from the ending epoch's stats and prunes
// stats two epochs old. Work is proportional to apps that were active.
func (k Keeper) rolloverEpoch(ctx sdk.Context) error {
	params := k.GetParams(ctx)
	epoch, start := k.CurrentEpoch(ctx)
	if ctx.BlockHeight() < start+params.EpochLengthBlocks-1 {
		return nil
	}
	next := epoch + 1
	err := k.EpochStats.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](epoch), func(_ collections.Pair[uint64, uint64], st types.EpochStats) (bool, error) {
		app, err := k.GetApp(ctx, st.AppId)
		if err != nil || app.Status != types.APP_STATUS_ACTIVE {
			return false, nil
		}
		q := ComputeQuota(params, st, app.DomainVerified)
		q.Epoch = next
		return false, k.Quotas.Set(ctx, st.AppId, q)
	})
	if err != nil {
		return err
	}
	if epoch > 0 {
		var stale []collections.Pair[uint64, uint64]
		_ = k.EpochStats.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](epoch-1), func(key collections.Pair[uint64, uint64], _ types.EpochStats) (bool, error) {
			stale = append(stale, key)
			return false, nil
		})
		for _, key := range stale {
			if err := k.EpochStats.Remove(ctx, key); err != nil {
				return err
			}
		}
	}
	if err := k.EpochNumber.Set(ctx, next); err != nil {
		return err
	}
	if err := k.EpochStart.Set(ctx, ctx.BlockHeight()+1); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent("apps_epoch_rollover", sdk.NewAttribute("epoch", fmt.Sprint(next))))
	return nil
}

// EndBlock finalizes due contract moves and rolls the quota epoch.
func (k Keeper) EndBlock(ctx sdk.Context) error {
	if err := k.finalizeMoves(ctx); err != nil {
		return err
	}
	return k.rolloverEpoch(ctx)
}
