// SPDX-License-Identifier: Apache-2.0

import { WavelengthError } from '@lightninglabs/wavelength-web';
import { TLightningErrorCode, TSdkError } from '@/api/lightning-errors';

// Maps wavelength SDK error codes to the lightning error codes the UI translates.
const wavelengthErrorCodes: Partial<Record<string, TLightningErrorCode>> = {
  insufficient_balance: TLightningErrorCode.INSUFFICIENT_FUNDS,
  invalid_request: TLightningErrorCode.INVALID_PAYMENT_INPUT,
};

// SDK 0.1.0 reports all daemon-side failures with the generic code
// 'wavelength_error' (a richer mapping is planned upstream), so recognize the
// important cases by message until the SDK ships real codes.
const codeFromMessage = (message: string): TLightningErrorCode | undefined => {
  if (/already\s+(paid|used|exists)|duplicate/i.test(message)) {
    return TLightningErrorCode.INVOICE_ALREADY_USED;
  }
  if (/insufficient/i.test(message)) {
    return TLightningErrorCode.INSUFFICIENT_FUNDS;
  }
  return undefined;
};

/**
 * Converts an error thrown by the wavelength SDK (or anything else) into the
 * `TSdkError` shape the lightning UI expects. Known wavelength error codes are
 * mapped to a `TLightningErrorCode` so they get translated; everything else
 * passes its message through untranslated.
 */
export const toSdkError = (error: unknown, defaultMessage: string = 'Error'): TSdkError => {
  if (error instanceof TSdkError) {
    return error;
  }
  if (error instanceof WavelengthError) {
    return new TSdkError(
      error.message || defaultMessage,
      wavelengthErrorCodes[error.code] ?? codeFromMessage(error.message || ''));
  }
  if (error instanceof Error) {
    return new TSdkError(error.message || defaultMessage);
  }
  return new TSdkError(error === undefined || error === null ? defaultMessage : String(error));
};
