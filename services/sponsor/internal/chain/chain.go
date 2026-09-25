// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Package chain reads the authoritative sponsorship inputs from VaporChain:
// app registry, contract attribution and the earned quota (x/apps), plus
// account code (for EIP-7702 delegation checks) over JSON-RPC.
package chain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type App struct {
	AppID            uint64
	Owner            string
	RevenueRecipient string
	Status           string
	Domain           string
}

type Quota struct {
	Epoch     uint64
	Gas       uint64
	EpochEnds int64
	Diversity uint32
	UniqueEst uint64
}

type Client struct {
	rest string
	http *http.Client
	eth  *ethclient.Client

	mu       sync.Mutex
	bindings map[common.Address]cached[uint64] // contract -> app id (0 = none)
	apps     map[uint64]cached[App]
	cacheTTL time.Duration
}

type cached[T any] struct {
	v   T
	exp time.Time
}

func New(restURL string, eth *ethclient.Client, ttl time.Duration) *Client {
	return &Client{
		rest: strings.TrimRight(restURL, "/"), eth: eth,
		http:     &http.Client{Timeout: 5 * time.Second},
		bindings: map[common.Address]cached[uint64]{}, apps: map[uint64]cached[App]{}, cacheTTL: ttl,
	}
}

var ErrNotFound = errors.New("not found")

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.rest+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound || (resp.StatusCode >= 400 && strings.Contains(string(body), "not")) {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

func u64(s string) uint64 {
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}

// App returns an app by id (cached).
func (c *Client) App(ctx context.Context, id uint64) (App, error) {
	c.mu.Lock()
	if e, ok := c.apps[id]; ok && time.Now().Before(e.exp) {
		c.mu.Unlock()
		return e.v, nil
	}
	c.mu.Unlock()
	var r struct {
		App struct {
			AppID            string `json:"app_id"`
			Owner            string `json:"owner"`
			RevenueRecipient string `json:"revenue_recipient"`
			Status           string `json:"status"`
			Domain           string `json:"domain"`
		} `json:"app"`
	}
	if err := c.get(ctx, fmt.Sprintf("/vaporchain/apps/v1/apps/%d", id), &r); err != nil {
		return App{}, err
	}
	a := App{AppID: u64(r.App.AppID), Owner: r.App.Owner, RevenueRecipient: r.App.RevenueRecipient, Status: r.App.Status, Domain: r.App.Domain}
	c.mu.Lock()
	c.apps[id] = cached[App]{v: a, exp: time.Now().Add(c.cacheTTL)}
	c.mu.Unlock()
	return a, nil
}

// AppOfContract returns the app a contract is attributed to (0 if none).
func (c *Client) AppOfContract(ctx context.Context, contract common.Address) (uint64, error) {
	c.mu.Lock()
	if e, ok := c.bindings[contract]; ok && time.Now().Before(e.exp) {
		c.mu.Unlock()
		return e.v, nil
	}
	c.mu.Unlock()
	var r struct {
		Binding struct {
			AppID string `json:"app_id"`
		} `json:"binding"`
	}
	var id uint64
	err := c.get(ctx, "/vaporchain/apps/v1/contracts/"+strings.ToLower(contract.Hex()), &r)
	switch {
	case errors.Is(err, ErrNotFound):
		id = 0
	case err != nil:
		return 0, err
	default:
		id = u64(r.Binding.AppID)
	}
	c.mu.Lock()
	c.bindings[contract] = cached[uint64]{v: id, exp: time.Now().Add(c.cacheTTL)}
	c.mu.Unlock()
	return id, nil
}

// Quota returns the app's earned sponsorship quota for the current epoch.
func (c *Client) Quota(ctx context.Context, appID uint64) (Quota, error) {
	var r struct {
		Quota struct {
			Epoch        string `json:"epoch"`
			Gas          string `json:"gas"`
			UniqueEst    string `json:"unique_payers_estimate"`
			DiversityBps uint32 `json:"diversity_bps"`
		} `json:"quota"`
		EpochEnds string `json:"epoch_ends_at_height"`
	}
	if err := c.get(ctx, fmt.Sprintf("/vaporchain/apps/v1/apps/%d/quota", appID), &r); err != nil {
		return Quota{}, err
	}
	ends, _ := strconv.ParseInt(r.EpochEnds, 10, 64)
	return Quota{Epoch: u64(r.Quota.Epoch), Gas: u64(r.Quota.Gas), EpochEnds: ends, Diversity: r.Quota.DiversityBps, UniqueEst: u64(r.Quota.UniqueEst)}, nil
}

// Code returns the runtime code at an address.
func (c *Client) Code(ctx context.Context, a common.Address) ([]byte, error) {
	return c.eth.CodeAt(ctx, a, nil)
}

// Delegation returns the EIP-7702 implementation an EOA delegates to, if any.
func Delegation(code []byte) (common.Address, bool) {
	if len(code) == 23 && code[0] == 0xef && code[1] == 0x01 && code[2] == 0x00 {
		return common.BytesToAddress(code[3:]), true
	}
	return common.Address{}, false
}
