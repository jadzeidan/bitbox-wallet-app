// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useState } from 'react';
import {
  type TLightningPayment,
  type TReceivePaymentResponse,
  getListPayments,
  subscribeListPayments,
} from '@/api/lightning';
import { useMountedRef } from '@/hooks/mount';
import { unsubscribe } from '@/utils/subscriptions';

export type TReceiveStep = 'create-invoice' | 'wait' | 'invoice' | 'success';

type TProps = {
  onSuccess: () => void;
  receivePaymentResponse?: TReceivePaymentResponse;
  step: TReceiveStep;
};

export const useReceivePaymentSuccess = ({
  onSuccess,
  receivePaymentResponse,
  step,
}: TProps) => {
  const mounted = useMountedRef();
  const [payments, setPayments] = useState<TLightningPayment[]>();
  const [receivedPayment, setReceivedPayment] = useState<TLightningPayment>();

  const resetReceivedPayment = useCallback(() => {
    setReceivedPayment(undefined);
  }, []);

  const loadPayments = useCallback(() => {
    getListPayments()
      .then((payments) => {
        if (mounted.current) {
          setPayments(payments);
        }
      })
      .catch(console.error);
  }, [mounted]);

  // load the initial payment list, then reload it whenever the backend reports a payment change.
  useEffect(() => {
    loadPayments();
    const subscriptions = [subscribeListPayments(loadPayments)];
    return () => unsubscribe(subscriptions);
  }, [loadPayments]);

  // created invoices can be matched exactly by their invoice string.
  useEffect(() => {
    if (!payments || !receivePaymentResponse || step !== 'invoice') {
      return;
    }

    const payment = payments.find((payment) => payment.type === 'receive' && payment.invoice === receivePaymentResponse.invoice);
    if (payment?.status === 'complete') {
      setReceivedPayment(payment);
      onSuccess();
    }
  }, [onSuccess, payments, receivePaymentResponse, step]);

  return {
    receivedPayment,
    resetReceivedPayment,
  };
};
