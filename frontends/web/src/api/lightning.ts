// SPDX-License-Identifier: Apache-2.0

import { classifyDestination, type Balance, type PrepareSendResult } from '@lightninglabs/wavelength-web';
import type { AccountCode, TAmountWithConversions, TBalance, TTransactionStatus } from '@/api/account';
import type { TSubscriptionCallback, TUnsubscribe } from '@/api/subscribe';
import { subscribeEndpoint } from '@/api/subscribe';
import { apiGet, apiPost } from '@/utils/request';
import { TLightningErrorCode, TSdkError } from './lightning-errors';
import { awaitEngine } from './wavelength/engine';
import { toSdkError } from './wavelength/errors';
import { getLnurlPayParams, parseLnurlPayInput, requestLnurlInvoice } from './wavelength/lnurl';

export type TLightningResponse<T> =
  | {
    success: true;
    data: T;
  }
  | {
    success: false;
    errorMessage?: string;
    errorCode?: TLightningErrorCode;
  };

export type TLightningAccount = {
  rootFingerprint: string;
  code: AccountCode;
  num: number;
};

export type TLightningCredentials = {
  mnemonic: string;
  password: string;
  network: 'signet';
  // Account code, used to namespace the locally persisted wallet data.
  code: AccountCode;
};

export type TLightningBolt11Invoice = {
  invoice: string;
  description?: string;
  amountSat?: number;
};

export type TLightningLNURLPay = {
  input: string;
  address?: string;
  domain: string;
  description?: string;
  minAmountSat: number;
  maxAmountSat: number;
};

export type TBitcoinDepositState = 'confirming' | 'claiming' | 'complete' | 'unclaimed';

export type TBitcoinDeposit = {
  txid: string;
  vout: number;
  state: TBitcoinDepositState;
  claimError?: string;
};

export type TLightningPayment = {
  id: string;
  type: 'send' | 'receive';
  status: TTransactionStatus;
  time: string | null;
  description?: string;
  amount: TAmountWithConversions;
  amountAtTime: TAmountWithConversions;
  deductedAmountAtTime: TAmountWithConversions;
  fee: TAmountWithConversions;
  invoice?: string;
  bitcoinDeposit?: TBitcoinDeposit;
};

// Wallet state pushed to the backend (POST lightning/state); must match the
// walletState/walletPayment wire types in backend/lightning exactly.
export type TWalletPayment = {
  id: string;
  kind: 'send' | 'receive' | 'deposit' | 'exit';
  status: 'pending' | 'complete' | 'failed';
  // Signed: positive inbound, negative outbound.
  amountSat: number;
  feeSat: number;
  // Unix seconds.
  createdAt: number;
  note?: string;
  invoice?: string;
  paymentHash?: string;
  txid?: string;
  failureReason?: string;
};

export type TWalletState = {
  ready: boolean;
  serverConnected: boolean;
  blockHeight: number;
  balanceSat: number;
  pendingInSat: number;
  pendingOutSat: number;
  payments: TWalletPayment[];
};

export type TReceivePaymentRequest = {
  amountSat: number;
  description: string;
};

export type TReceivePaymentResponse = {
  invoice: string;
};

export type TSendPaymentRequest = {
  type: TPaymentInputType.BOLT11;
  paymentInput: string;
  amountSat?: number;
  approvedFeeSat: number;
} | {
  type: TPaymentInputType.LNURL_PAY;
  paymentInput: string;
  amountSat: number;
  approvedFeeSat: number;
};

export type TPreparePaymentRequest = {
  type: TPaymentInputType.BOLT11;
  paymentInput: string;
  amountSat?: number;
} | {
  type: TPaymentInputType.LNURL_PAY;
  paymentInput: string;
  amountSat: number;
};

export type TPreparePaymentResponse = {
  amountSat: number;
  feeSat: number;
  totalDebitSat: number;
};

export type TServiceStatus = {
  status: 'operational' | 'major' | 'unknown';
};

export enum TPaymentInputType {
  BOLT11 = 'bolt11',
  LNURL_PAY = 'lnurlPay',
}

export type TPaymentInput = {
  type: TPaymentInputType.BOLT11;
  invoice: TLightningBolt11Invoice;
} | {
  type: TPaymentInputType.LNURL_PAY;
  lnurlPay: TLightningLNURLPay;
};

export type TParsePaymentInputRequest = {
  s: string;
};

const getApiResponse = async <T>(url: string, defaultError: string = 'Error'): Promise<T> => {
  const response: TLightningResponse<T> = await apiGet(url);
  if (!response.success) {
    throw new TSdkError(response.errorMessage || defaultError, response.errorCode);
  }
  if (response.data === undefined) {
    throw new TSdkError(defaultError);
  }
  return response.data;
};

const postApiResponse = async <T, C extends object | undefined>(url: string, data: C, defaultError: string = 'Error'): Promise<T> => {
  const response: TLightningResponse<T> = await apiPost(url, data);
  if (!response.success) {
    throw new TSdkError(response.errorMessage || defaultError, response.errorCode);
  }
  if (response.data === undefined) {
    return undefined as T;
  }
  return response.data;
};

export const getLightningAccount = async (): Promise<TLightningAccount | null> => {
  return apiGet('lightning/account');
};

// Fetches the wallet credentials derived by the backend from the BitBox; used
// by the wavelength engine (api/wavelength/engine.ts) to open the wallet.
export const getLightningCredentials = async (): Promise<TLightningCredentials> => {
  return getApiResponse<TLightningCredentials>('lightning/credentials', 'Error calling getLightningCredentials');
};

// Pushes a wallet state snapshot to the backend, which serves balance and
// payment list from it; used by the wavelength engine (api/wavelength/engine.ts).
export const postLightningState = async (state: TWalletState): Promise<void> => {
  return postApiResponse<void, TWalletState>('lightning/state', state, 'Error calling postLightningState');
};

export const getLightningReady = async (): Promise<boolean> => {
  return getApiResponse<boolean>('lightning/ready', 'Error calling getLightningReady');
};

export const postActivate = async (): Promise<void> => {
  return postApiResponse<void, undefined>('lightning/activate', undefined, 'Error calling postActivate');
};

export const postDeactivate = async (): Promise<void> => {
  return postApiResponse<void, undefined>('lightning/deactivate', undefined, 'Error calling postDeactivate');
};

export const getLightningBalance = async (): Promise<TBalance> => {
  return getApiResponse<TBalance>('lightning/balance', 'Error calling getLightningBalance');
};

export const getBlockExplorerTxPrefix = async (): Promise<string> => {
  return getApiResponse<string>('lightning/block-explorer-tx-prefix', 'Error calling getBlockExplorerTxPrefix');
};

export const getServiceStatus = async (): Promise<TServiceStatus> => {
  return getApiResponse<TServiceStatus>('lightning/service-status', 'Error calling getServiceStatus');
};

export const getListPayments = async (): Promise<TLightningPayment[]> => {
  return getApiResponse<TLightningPayment[]>('lightning/list-payments', 'Error calling getListPayments');
};

// Send quotes are cached per payment input and amount, so that the quote
// approved by the user in the prepare step is the one that gets dispatched.
type TCachedQuote = {
  invoice: string;
  quote: PrepareSendResult;
};

const quoteCache = new Map<string, TCachedQuote>();

const quoteCacheKey = (paymentInput: string, amountSat?: number): string => {
  return `${amountSat ?? ''}:${paymentInput}`;
};

const isQuoteUsable = (cached: TCachedQuote | undefined): cached is TCachedQuote => {
  if (cached === undefined) {
    return false;
  }
  // A margin so a quote about to expire is not dispatched.
  return cached.quote.expiresAtUnix <= 0 || cached.quote.expiresAtUnix * 1000 > Date.now() + 5000;
};

const prepareSendQuote = async (invoice: string, amountSat?: number): Promise<PrepareSendResult> => {
  try {
    return await (await awaitEngine()).prepareSend(
      amountSat === undefined ? { invoice } : { invoice, amountSat },
    );
  } catch (error) {
    throw toSdkError(error, 'Error preparing the payment');
  }
};

const prepareLnurlPay = async (paymentInput: string, amountSat: number): Promise<TCachedQuote> => {
  const endpoint = parseLnurlPayInput(paymentInput);
  if (!endpoint) {
    throw new TSdkError('Invalid lightning address or LNURL', TLightningErrorCode.INVALID_PAYMENT_INPUT);
  }
  const params = await getLnurlPayParams(endpoint);
  const minAmountSat = Math.ceil(params.minSendableMsat / 1000);
  const maxAmountSat = Math.floor(params.maxSendableMsat / 1000);
  if (amountSat < minAmountSat || amountSat > maxAmountSat) {
    throw new TSdkError(
      `The amount must be between ${minAmountSat} and ${maxAmountSat} sats`,
      TLightningErrorCode.INVALID_AMOUNT,
    );
  }
  const invoice = await requestLnurlInvoice(params.callback, amountSat * 1000);
  return { invoice, quote: await prepareSendQuote(invoice) };
};

export const getParsePaymentInput = async ({ s }: TParsePaymentInputRequest): Promise<TPaymentInput> => {
  let input = s.trim();
  if (input.toLowerCase().startsWith('lightning:')) {
    input = input.slice('lightning:'.length);
  }
  const lnurlEndpoint = parseLnurlPayInput(input);
  if (lnurlEndpoint) {
    const params = await getLnurlPayParams(lnurlEndpoint);
    return {
      type: TPaymentInputType.LNURL_PAY,
      lnurlPay: {
        input,
        address: lnurlEndpoint.address,
        domain: lnurlEndpoint.domain,
        description: params.description,
        minAmountSat: Math.ceil(params.minSendableMsat / 1000),
        maxAmountSat: Math.floor(params.maxSendableMsat / 1000),
      },
    };
  }
  const destination = classifyDestination(input);
  // 'unrepresentable' invoice amounts (sub-satoshi or beyond the safe integer
  // range) cannot be paid, so reject them here instead of prompting the user
  // for an amount the invoice already encodes.
  if (destination.kind !== 'invoice' || destination.amount.status === 'unrepresentable') {
    throw new TSdkError('Invalid payment input', TLightningErrorCode.INVALID_PAYMENT_INPUT);
  }
  const amountSat = destination.amount.status === 'known' ? destination.amount.sat : undefined;
  let description: string | undefined;
  if (amountSat !== undefined) {
    // Best effort: a send quote carries the invoice description, and is cached
    // for reuse by the following prepare-payment call. Zero-amount invoices
    // cannot be quoted without an amount, so their description stays empty.
    try {
      const quote = await prepareSendQuote(input);
      quoteCache.set(quoteCacheKey(input), { invoice: input, quote });
      description = quote.invoiceDescription || undefined;
    } catch {
      // Parsing must still succeed when the wallet cannot quote right now.
    }
  }
  return {
    type: TPaymentInputType.BOLT11,
    invoice: {
      invoice: input,
      description,
      amountSat,
    },
  };
};

export const getBoardingAddress = async (): Promise<string> => {
  try {
    const result = await (await awaitEngine()).deposit();
    return result.address;
  } catch (error) {
    throw toSdkError(error, 'Error calling getBoardingAddress');
  }
};

const totalDebitSat = (quote: PrepareSendResult): number => {
  return quote.totalOutflowKnown
    ? quote.expectedTotalOutflowSat
    : quote.amountSat + quote.expectedFeeSat;
};

export const postPreparePayment = async (data: TPreparePaymentRequest): Promise<TPreparePaymentResponse> => {
  const key = quoteCacheKey(data.paymentInput, data.amountSat);
  let cached = quoteCache.get(key);
  if (!isQuoteUsable(cached)) {
    switch (data.type) {
    case TPaymentInputType.BOLT11:
      cached = {
        invoice: data.paymentInput,
        quote: await prepareSendQuote(data.paymentInput, data.amountSat),
      };
      break;
    case TPaymentInputType.LNURL_PAY:
      cached = await prepareLnurlPay(data.paymentInput, data.amountSat);
      break;
    }
    quoteCache.set(key, cached);
  }
  return {
    amountSat: cached.quote.amountSat,
    feeSat: cached.quote.expectedFeeSat,
    totalDebitSat: totalDebitSat(cached.quote),
  };
};

export const postSendPayment = async (data: TSendPaymentRequest): Promise<void> => {
  const key = quoteCacheKey(data.paymentInput, data.amountSat);
  let cached = quoteCache.get(key);
  if (!isQuoteUsable(cached)) {
    switch (data.type) {
    case TPaymentInputType.BOLT11:
      cached = {
        invoice: data.paymentInput,
        quote: await prepareSendQuote(data.paymentInput, data.amountSat),
      };
      break;
    case TPaymentInputType.LNURL_PAY:
      cached = await prepareLnurlPay(data.paymentInput, data.amountSat);
      break;
    }
    quoteCache.set(key, cached);
  }
  const { quote } = cached;
  if (quote.expectedFeeSat > data.approvedFeeSat) {
    // The cached quote stays around, so the re-prepare triggered by the UI
    // shows the updated fee for approval.
    throw new TSdkError('The network fee changed', TLightningErrorCode.PAYMENT_APPROVAL_REQUIRED);
  }
  let balance: Balance;
  try {
    balance = await (await awaitEngine()).client.balance();
  } catch (error) {
    throw toSdkError(error, 'Error calling postSendPayment');
  }
  if (totalDebitSat(quote) > balance.confirmedSat) {
    throw new TSdkError('Insufficient funds to send this payment', TLightningErrorCode.INSUFFICIENT_FUNDS);
  }
  try {
    await (await awaitEngine()).sendPrepared(quote);
  } catch (error) {
    throw toSdkError(error, 'Error calling postSendPayment');
  } finally {
    // Quotes are single use; a retry after failure has to re-prepare.
    quoteCache.delete(key);
  }
};

export const getReceivePayment = async ({ amountSat, description }: TReceivePaymentRequest): Promise<TReceivePaymentResponse> => {
  try {
    const result = await (await awaitEngine()).receive({
      amountSat,
      memo: description || undefined,
    });
    return { invoice: result.invoice };
  } catch (error) {
    throw toSdkError(error, 'Error calling getReceivePayment');
  }
};

export const subscribeLightningAccount = (cb: TSubscriptionCallback<TLightningAccount | null>): TUnsubscribe => {
  return subscribeEndpoint('lightning/account', cb);
};

export const subscribeLightningReady = (cb: TSubscriptionCallback<boolean>): TUnsubscribe => {
  return subscribeEndpoint('lightning/ready', cb);
};

export const subscribeListPayments = (cb: TSubscriptionCallback<TLightningPayment[]>) => {
  return subscribeEndpoint('lightning/list-payments', cb);
};
