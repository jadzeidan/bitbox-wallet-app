// SPDX-License-Identifier: Apache-2.0

// Minimal LNURL-pay client (LUD-06) with lightning-address support (LUD-16).
// The wavelength SDK has no LNURL support, so resolving a pay request to a
// bolt11 invoice happens here; the invoice is then paid like any other.

import { classifyDestination } from '@lightninglabs/wavelength-web';
import { TLightningErrorCode, TSdkError } from '@/api/lightning-errors';

// LUD-16 internet identifier: <name>@<domain>.
const lightningAddressRegex = /^[a-z0-9\-_.+]+@[a-z0-9\-_.]+\.[a-z]{2,}$/i;

export type TLnurlPayEndpoint = {
  // The https URL serving the LNURL-pay parameters.
  url: string;
  // Set when the input was a lightning address.
  address?: string;
  domain: string;
};

export type TLnurlPayParams = {
  callback: string;
  minSendableMsat: number;
  maxSendableMsat: number;
  description?: string;
};

// LUD-01 mandates https; only onion services may use http.
const isAllowedLnurlUrl = (url: URL): boolean => {
  return url.protocol === 'https:'
    || (url.protocol === 'http:' && url.hostname.endsWith('.onion'));
};

const fetchTimeoutMs = 30000;
// LNURL responses are tiny; anything bigger is not a pay endpoint.
const maxResponseSize = 100000;

const bech32Charset = 'qpzry9x8gf2tvdw0s3jn54khce6mua7l';
const bech32Generator = [0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3];

const bech32Polymod = (values: number[]): number => {
  let chk = 1;
  for (const value of values) {
    const top = chk >> 25;
    chk = ((chk & 0x1ffffff) << 5) ^ value;
    for (let i = 0; i < 5; i++) {
      if ((top >> i) & 1) {
        chk ^= bech32Generator[i]!;
      }
    }
  }
  return chk;
};

const bech32HrpExpand = (hrp: string): number[] => {
  const expanded: number[] = [];
  for (const c of hrp) {
    expanded.push(c.charCodeAt(0) >> 5);
  }
  expanded.push(0);
  for (const c of hrp) {
    expanded.push(c.charCodeAt(0) & 31);
  }
  return expanded;
};

// Decodes a bech32 string into its human-readable part and 5-bit data words.
// LNURL strings exceed the 90 character limit of BIP-173, so no length limit
// is enforced. Returns null on any malformed input.
const bech32Decode = (input: string): { hrp: string; words: number[] } | null => {
  if (input !== input.toLowerCase() && input !== input.toUpperCase()) {
    return null;
  }
  const lower = input.toLowerCase();
  const separator = lower.lastIndexOf('1');
  if (separator < 1 || separator + 7 > lower.length) {
    return null;
  }
  const hrp = lower.slice(0, separator);
  const words: number[] = [];
  for (const c of lower.slice(separator + 1)) {
    const value = bech32Charset.indexOf(c);
    if (value === -1) {
      return null;
    }
    words.push(value);
  }
  if (bech32Polymod([...bech32HrpExpand(hrp), ...words]) !== 1) {
    return null;
  }
  return { hrp, words: words.slice(0, -6) };
};

// Regroups 5-bit words into bytes, rejecting non-zero padding.
const bech32WordsToBytes = (words: number[]): Uint8Array | null => {
  let accumulator = 0;
  let bits = 0;
  const bytes: number[] = [];
  for (const word of words) {
    accumulator = (accumulator << 5) | word;
    bits += 5;
    while (bits >= 8) {
      bits -= 8;
      bytes.push((accumulator >> bits) & 0xff);
    }
  }
  if (bits >= 5 || ((accumulator << (8 - bits)) & 0xff) !== 0) {
    return null;
  }
  return new Uint8Array(bytes);
};

/**
 * Parses a lightning address (`name@domain`, LUD-16) or a bech32 encoded
 * `lnurl1...` string (LUD-06) into the https endpoint serving the LNURL-pay
 * parameters. Returns null when the input is neither.
 */
export const parseLnurlPayInput = (input: string): TLnurlPayEndpoint | null => {
  if (lightningAddressRegex.test(input)) {
    const address = input.toLowerCase();
    const separator = address.indexOf('@');
    const name = address.slice(0, separator);
    const domain = address.slice(separator + 1);
    return {
      url: `https://${domain}/.well-known/lnurlp/${name}`,
      address,
      domain,
    };
  }
  if (!/^lnurl1/i.test(input)) {
    return null;
  }
  const decoded = bech32Decode(input);
  if (!decoded || decoded.hrp !== 'lnurl') {
    return null;
  }
  const bytes = bech32WordsToBytes(decoded.words);
  if (!bytes) {
    return null;
  }
  try {
    const url = new TextDecoder().decode(bytes);
    const parsed = new URL(url);
    if (!isAllowedLnurlUrl(parsed)) {
      return null;
    }
    return { url, domain: parsed.hostname };
  } catch {
    return null;
  }
};

const fetchJson = async (url: string, errorMessage: string): Promise<any> => {
  let json: any;
  try {
    const response = await fetch(url, { signal: AbortSignal.timeout(fetchTimeoutMs) });
    if (!response.ok) {
      throw new Error(`unexpected status ${response.status}`);
    }
    const text = await response.text();
    if (text.length > maxResponseSize) {
      throw new Error('response too large');
    }
    json = JSON.parse(text);
  } catch {
    throw new TSdkError(errorMessage, TLightningErrorCode.INVALID_PAYMENT_INPUT);
  }
  if (json && json.status === 'ERROR') {
    throw new TSdkError(json.reason || errorMessage, TLightningErrorCode.INVALID_PAYMENT_INPUT);
  }
  return json;
};

// Extracts the human-readable description from LUD-06 metadata: a JSON encoded
// array of [mimetype, content] pairs; the `text/plain` entry is mandatory.
const descriptionFromMetadata = (metadata: unknown): string | undefined => {
  if (typeof metadata !== 'string') {
    return undefined;
  }
  try {
    const entries = JSON.parse(metadata);
    if (!Array.isArray(entries)) {
      return undefined;
    }
    const textEntry = entries.find(entry => (
      Array.isArray(entry) && entry[0] === 'text/plain' && typeof entry[1] === 'string'
    ));
    return textEntry ? textEntry[1] : undefined;
  } catch {
    return undefined;
  }
};

/**
 * Fetches and validates the LNURL-pay parameters from a pay endpoint.
 */
export const getLnurlPayParams = async (endpoint: TLnurlPayEndpoint): Promise<TLnurlPayParams> => {
  const errorMessage = `Invalid lightning address or LNURL (${endpoint.domain})`;
  const params = await fetchJson(endpoint.url, errorMessage);
  if (
    !params
    || params.tag !== 'payRequest'
    || typeof params.callback !== 'string'
    || typeof params.minSendable !== 'number'
    || typeof params.maxSendable !== 'number'
    || params.minSendable <= 0
    || params.maxSendable < params.minSendable
  ) {
    throw new TSdkError(errorMessage, TLightningErrorCode.INVALID_PAYMENT_INPUT);
  }
  return {
    callback: params.callback,
    minSendableMsat: params.minSendable,
    maxSendableMsat: params.maxSendable,
    description: descriptionFromMetadata(params.metadata),
  };
};

/**
 * Requests a bolt11 invoice for the given amount from an LNURL-pay callback
 * and verifies that the returned invoice actually encodes that amount.
 */
export const requestLnurlInvoice = async (callback: string, amountMsat: number): Promise<string> => {
  const errorMessage = 'Could not get an invoice for this lightning address or LNURL';
  let url: URL;
  try {
    url = new URL(callback);
  } catch {
    throw new TSdkError(errorMessage, TLightningErrorCode.INVALID_PAYMENT_INPUT);
  }
  if (!isAllowedLnurlUrl(url)) {
    throw new TSdkError(errorMessage, TLightningErrorCode.INVALID_PAYMENT_INPUT);
  }
  url.searchParams.set('amount', String(amountMsat));
  const response = await fetchJson(url.toString(), errorMessage);
  const invoice = response?.pr;
  if (typeof invoice !== 'string') {
    throw new TSdkError(errorMessage, TLightningErrorCode.INVALID_PAYMENT_INPUT);
  }
  const destination = classifyDestination(invoice);
  if (
    destination.kind !== 'invoice'
    || destination.amount.status !== 'known'
    || destination.amount.sat !== Math.floor(amountMsat / 1000)
  ) {
    throw new TSdkError(errorMessage, TLightningErrorCode.INVALID_PAYMENT_INPUT);
  }
  return invoice;
};
