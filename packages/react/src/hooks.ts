// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import type { Address, Hex } from 'viem'
import {
  VaporError,
  unifiedBalance,
  type Asset,
  type BridgeStage,
  type Call,
  type RouteRequest,
  type SkipRoute,
  type TokenLaunchParams,
  type TxHandle,
} from '@vaporchain/sdk'
import { useVapor, vaporKey } from './context.js'

/** The app's remaining sponsored gas this epoch (x/apps quota). Polls every 15s. */
export function useSponsorQuota(appId?: bigint) {
  const { client, network, appId: ctxApp } = useVapor()
  const id = appId ?? ctxApp
  return useQuery({
    queryKey: vaporKey(network, 'quota', id),
    queryFn: () => client.sponsor.quota(id!),
    enabled: id !== undefined,
    refetchInterval: 15_000,
  })
}

/**
 * One number per asset: the VaporChain balance plus balances on other chains
 * (supplied by `remote` lookups, e.g. from a Cosmos or EVM wallet connector).
 */
export function useUnifiedBalance(p: {
  symbol?: string
  asset?: Asset
  owner?: Address
  remote?: Array<() => Promise<{ chainId: string; amount: bigint }>>
}) {
  const { client, network, assets, account } = useVapor()
  const asset = p.asset ?? assets[p.symbol ?? 'USDC']
  const owner = p.owner ?? account?.address
  return useQuery({
    queryKey: vaporKey(network, 'balance', asset?.erc20, owner, p.remote?.length ?? 0),
    queryFn: () => unifiedBalance(client.publicClient, asset!, owner!, p.remote ?? []),
    enabled: !!asset && !!owner,
    refetchInterval: 10_000,
  })
}

/** Exact protocol fee for a Settle payment of `amount`, and whether gas is sponsored. */
export function useFeeQuote(p: { token?: Address; amount?: bigint; appId?: bigint }) {
  const { client, network } = useVapor()
  return useQuery({
    queryKey: vaporKey(network, 'fee', p.token, p.amount, p.appId),
    queryFn: () => client.estimateFee({ token: p.token!, amount: p.amount!, ...(p.appId !== undefined ? { appId: p.appId } : {}) }),
    enabled: !!p.token && p.amount !== undefined && p.amount > 0n,
  })
}

function useInvalidateVapor() {
  const qc = useQueryClient()
  const { network } = useVapor()
  return useCallback(() => qc.invalidateQueries({ queryKey: ['vapor', network.chain.id] }), [qc, network.chain.id])
}

/** Run calls; sponsor='app' (default) sends a gasless UserOp paid by the app's quota. */
export function usePay() {
  const { client } = useVapor()
  const invalidate = useInvalidateVapor()
  return useMutation<TxHandle, Error, { calls: Call[]; sponsor?: 'app' | 'user' }>({
    mutationFn: (i) => client.pay(i),
    onSuccess: () => void invalidate(),
  })
}

/** Approve the app + pay an order in ONE gasless signature. */
export function useCheckout() {
  const { client } = useVapor()
  const invalidate = useInvalidateVapor()
  return useMutation<TxHandle, Error, { checkout: Address; orderId: Hex; token: Address; amount: bigint; referrer?: Address; approve?: bigint }>({
    mutationFn: (i) => client.checkout(i),
    onSuccess: () => void invalidate(),
  })
}

/** Address the token will have, before launching (stable for the same params). */
export function usePredictedTokenAddress(p?: TokenLaunchParams) {
  const { client, network, account } = useVapor()
  return useQuery({
    queryKey: vaporKey(network, 'predict', account?.address, p?.name, p?.symbol, p?.owner, p?.salt, p?.initialSupply, p?.cap, p?.decimals, p?.metadataURI),
    queryFn: () => client.tokens.predict(account!.address, p!),
    enabled: !!account && !!p && p.name.length > 0 && p.symbol.length > 0,
  })
}

/** Launch an ERC-20 through the VaporChain token factory. */
export function useLaunchToken(opts?: { sponsored?: boolean }) {
  const { client } = useVapor()
  const invalidate = useInvalidateVapor()
  const sponsored = opts?.sponsored ?? false
  return useMutation({
    mutationFn: (p: TokenLaunchParams) => client.tokens.launch(p, { sponsored }),
    onSuccess: () => void invalidate(),
  })
}

export type BringInStage = 'idle' | 'routing' | 'routed' | BridgeStage

/**
 * Bring assets in from another chain with Skip:Go:
 *   const b = useBringIn()
 *   const route = await b.route({...})           // show amount_out, fees, ETA
 *   // user signs route's msgs on the SOURCE chain with their wallet, then:
 *   b.track(txHash, sourceChainId)                // stage -> bridging -> final
 */
export function useBringIn() {
  const { client } = useVapor()
  const invalidate = useInvalidateVapor()
  const [stage, setStage] = useState<BringInStage>('idle')
  const [routeResult, setRoute] = useState<SkipRoute | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const generation = useRef(0)
  useEffect(() => () => void generation.current++, [])

  const route = useCallback(
    async (req: RouteRequest) => {
      const gen = ++generation.current
      setError(null)
      setStage('routing')
      try {
        const r = await client.bridge(req)
        if (gen === generation.current) {
          setRoute(r)
          setStage('routed')
        }
        return r
      } catch (e) {
        if (gen === generation.current) {
          setError(e as Error)
          setStage('failed')
        }
        throw e
      }
    },
    [client],
  )

  const track = useCallback(
    async (txHash: string, sourceChainId: string) => {
      const gen = ++generation.current
      setError(null)
      try {
        for await (const s of client.status({ txHash, chainId: sourceChainId })) {
          if (gen !== generation.current) return // superseded or unmounted
          setStage(s)
          if (s === 'final') void invalidate()
        }
      } catch (e) {
        if (gen === generation.current) {
          setError(e instanceof VaporError ? e : new VaporError(String(e), 'ROUTE', e))
          setStage('failed')
        }
      }
    },
    [client, invalidate],
  )

  const reset = useCallback(() => {
    generation.current++
    setStage('idle')
    setRoute(null)
    setError(null)
  }, [])

  return { stage, route, routeResult, track, reset, error }
}
