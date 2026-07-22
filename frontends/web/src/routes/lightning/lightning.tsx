// SPDX-License-Identifier: Apache-2.0

import { type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import * as accountApi from '../../api/account';
import { getDeviceList } from '../../api/devices';
import {
  TLightningPayment,
  getBlockExplorerTxPrefix,
  getLightningBalance,
  getListPayments,
  subscribeListPayments,
  getServiceStatus,
  TServiceStatus,
} from '../../api/lightning';
import { Balance } from '../../components/balance/balance';
import { ContentWrapper } from '@/components/contentwrapper/contentwrapper';
import { View, ViewContent, ViewHeader } from '../../components/view/view';
import { GuideWrapper, GuidedContent, Header, Main } from '../../components/layout';
import { Spinner } from '../../components/spinner/Spinner';
import { ActionButtons } from './components/action-buttons';
import { LightningGuide } from './guide';
import { GlobalBanners } from '@/components/banners';
import { Status } from '../../components/status/status';
import { HideAmountsButton } from '../../components/hideamountsbutton/hideamountsbutton';
import { PaymentDetails } from './components/payment-details';
import { RatesContext } from '@/contexts/RatesContext';
import { useLoad } from '@/hooks/api';
import { useMountedRef } from '@/hooks/mount';
import { useLightning } from '@/hooks/lightning';
import { TransactionList } from '@/routes/account/components/transaction-list';
import type { TTransactionListItem } from '@/routes/account/components/transaction-list';
import { TransactionHistorySkeleton } from '@/routes/account/transaction-history-skeleton';
import { Button, SearchInput } from '@/components/forms';
import { LoupeBlue } from '@/components/icon';
import { SubTitle } from '@/components/title';
import { useDebounce } from '@/hooks/debounce';
import { useMediaQuery } from '@/hooks/mediaquery';
import { useScrollIntoView } from '@/hooks/scroll-into-view';
import accountStyle from '@/routes/account/account.module.css';

// The service status is a cheap read of backend-cached state; poll often so
// the banner tracks connection changes (e.g. right after the engine boots).
const serviceStatusPollInterval = 10 * 1000;

const bitcoinDepositTransactionStatus = (
  bitcoinDeposit: NonNullable<TLightningPayment['bitcoinDeposit']>,
): accountApi.TTransactionStatus => {
  switch (bitcoinDeposit.state) {
  case 'unclaimed':
    return 'failed';
  case 'complete':
    return 'complete';
  case 'claiming':
  case 'confirming':
    return 'pending';
  }
};

type TLightningPageLayoutProps = {
  accountDataLoaded: boolean;
  balance?: accountApi.TBalance;
  canSend?: boolean;
  children: ReactNode;
  statusBanners: ReactNode;
};

const LightningPageLayout = ({
  accountDataLoaded,
  balance,
  canSend,
  children,
  statusBanners,
}: TLightningPageLayoutProps) => {
  const { t } = useTranslation();

  return (
    <GuideWrapper>
      <GuidedContent>
        <Main>
          <ContentWrapper>
            {statusBanners}
          </ContentWrapper>
          <Header
            title={
              <h2>
                <span>{t('lightning.accountLabel')}</span>
              </h2>
            }
          >
            <HideAmountsButton />
          </Header>
          <View>
            <ViewHeader>
              <div className={accountStyle.balanceHeader}>
                <Balance balance={balance} />
                <ActionButtons
                  accountDataLoaded={accountDataLoaded}
                  canSend={canSend}
                />
              </div>
            </ViewHeader>
            <ViewContent>
              {children}
            </ViewContent>
          </View>
        </Main>
      </GuidedContent>
      <LightningGuide />
    </GuideWrapper>
  );
};

const paymentToTransaction = (
  payment: TLightningPayment,
  fallbackNote: string,
  bitcoinDepositNote: string,
  bitcoinDepositStateText: (state: NonNullable<TLightningPayment['bitcoinDeposit']>['state']) => string,
  bitcoinDepositStateShortText: (state: NonNullable<TLightningPayment['bitcoinDeposit']>['state']) => string,
): TTransactionListItem => {
  // A failed payment status wins over the deposit progress state, so a failed
  // or expired deposit is not rendered as eternally confirming.
  const status = payment.status !== 'failed' && payment.bitcoinDeposit
    ? bitcoinDepositTransactionStatus(payment.bitcoinDeposit)
    : payment.status;
  const isComplete = status === 'complete';
  const isFailed = status === 'failed';
  const statusProgress = isFailed
    ? undefined
    : payment.bitcoinDeposit?.state === 'confirming'
      ? 33
      : payment.bitcoinDeposit?.state === 'claiming'
        ? 66
        : undefined;

  return {
    addresses: [],
    amount: payment.amount,
    amountAtTime: payment.amountAtTime,
    deductedAmountAtTime: payment.deductedAmountAtTime,
    fee: payment.fee,
    feeRateInfo: '',
    gas: 0,
    internalID: payment.id,
    nonce: null,
    note: payment.bitcoinDeposit ? bitcoinDepositNote : payment.description || fallbackNote,
    numConfirmations: isComplete ? 1 : 0,
    numConfirmationsComplete: 1,
    size: 0,
    status,
    statusProgress,
    statusText: payment.bitcoinDeposit && !isComplete && !isFailed
      ? bitcoinDepositStateText(payment.bitcoinDeposit.state)
      : undefined,
    statusTextShort: payment.bitcoinDeposit && !isComplete && !isFailed
      ? bitcoinDepositStateShortText(payment.bitcoinDeposit.state)
      : undefined,
    time: payment.time,
    type: payment.type,
    txID: payment.id,
    vsize: 0,
    weight: 0,
  };
};

type TLightningInnerProps = {
  balance: accountApi.TBalance;
  explorerURL?: string;
  payments: TLightningPayment[];
  statusBanners: ReactNode;
};

const LightningInner = ({
  balance,
  explorerURL,
  payments,
  statusBanners,
}: TLightningInnerProps) => {
  const { t } = useTranslation();
  const isMobile = useMediaQuery('(max-width: 768px)');
  const [detailID, setDetailID] = useState<TLightningPayment['id'] | null>(null);
  const [showSearchBar, setShowSearchBar] = useState<boolean>(false);
  const [searchTerm, setSearchTerm] = useState<string>('');
  const debouncedSearchTerm = useDebounce(searchTerm, 200);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const hasPayments = payments.length > 0;
  const lightningTransactions = useMemo(() => {
    return payments.map(payment => paymentToTransaction(
      payment,
      payment.type === 'receive' ? t('generic.received') : t('generic.sent'),
      t('lightning.bitcoinDeposit.label'),
      (state) => t(`lightning.bitcoinDeposit.state.${state}`),
      (state) => t(`lightning.bitcoinDeposit.stateShort.${state}`),
    ));
  }, [payments, t]);
  const filteredTransactions = useMemo(() => {
    const searchLower = debouncedSearchTerm.toLowerCase().trim();

    if (!searchLower) {
      return lightningTransactions;
    }

    return lightningTransactions.filter(tx => (
      tx.note?.toLowerCase().includes(searchLower)
      || tx.txID.toLowerCase().includes(searchLower)
      || tx.status.toLowerCase().includes(searchLower)
      || tx.type.toLowerCase().includes(searchLower)
    ));
  }, [debouncedSearchTerm, lightningTransactions]);

  const scrollSearchIntoView = useScrollIntoView(searchInputRef, 48);

  useEffect(() => {
    if (showSearchBar && searchInputRef.current) {
      searchInputRef.current.focus();
      if (isMobile) {
        setTimeout(scrollSearchIntoView, 500);
      }
    }
  }, [showSearchBar, scrollSearchIntoView, isMobile]);

  return (
    <LightningPageLayout
      accountDataLoaded
      balance={balance}
      canSend={balance.hasAvailable}
      statusBanners={statusBanners}
    >
      <div className={accountStyle.accountHeader}>
        <div className={accountStyle.titleRow}>
          <SubTitle className={accountStyle.titleWithButton}>
            {t('accountSummary.transactionHistory')}
          </SubTitle>

          <Button
            className={accountStyle.searchButton}
            transparent
            disabled={!hasPayments}
            onClick={() => {
              if (showSearchBar) {
                setShowSearchBar(false);
                setSearchTerm('');
              } else {
                setShowSearchBar(true);
              }
            }}
          >
            {showSearchBar ? (
              <>✕ {t('generic.close')}</>
            ) : (
              <>
                <LoupeBlue className={accountStyle.loupe} />
                {t('generic.searchButton')}
              </>
            )}
          </Button>
        </div>

        <div className={`
          ${accountStyle.searchContainer || ''}
          ${!showSearchBar && accountStyle.searchHidden || ''}
        `}>
          <SearchInput
            ref={searchInputRef}
            placeholder={t('accountSummary.searchPlaceholder')}
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.currentTarget.value)}
          />
        </div>
      </div>

      <TransactionList
        transactionSuccess={true}
        filteredTransactions={filteredTransactions}
        debouncedSearchTerm={debouncedSearchTerm}
        onShowDetail={setDetailID}
      />

      <PaymentDetails
        id={detailID}
        explorerURL={explorerURL}
        payment={payments.find(payment => payment.id === detailID)}
        onClose={() => setDetailID(null)}
      />
    </LightningPageLayout>
  );
};

export const Lightning = () => {
  const { t } = useTranslation();
  const { btcUnit } = useContext(RatesContext);
  const { isLightningReady, lightningAccount } = useLightning();
  const [balance, setBalance] = useState<accountApi.TBalance>();
  const [syncedAddressesCount] = useState<number>();
  const [payments, setPayments] = useState<TLightningPayment[]>();
  const [serviceStatus, setServiceStatus] = useState<TServiceStatus>();
  const [error, setError] = useState<string>();
  const mounted = useMountedRef();
  const blockExplorerTxPrefix = useLoad(getBlockExplorerTxPrefix);
  const devices = useLoad(getDeviceList);

  const onStateChange = useCallback(async () => {
    try {
      setError(undefined);
      const [balance, payments] = await Promise.all([
        getLightningBalance(),
        getListPayments(),
      ]);
      if (!mounted.current) {
        return;
      }
      setBalance(balance);
      setPayments(payments);
    } catch (err: any) {
      if (!mounted.current) {
        return;
      }
      const errorMessage = err?.errorMessage || err?.message || String(err);
      setError(errorMessage);
    }
  }, [mounted]);

  useEffect(() => {
    if (!lightningAccount || !isLightningReady) {
      return;
    }

    onStateChange();

    return subscribeListPayments(onStateChange);
  }, [btcUnit, isLightningReady, lightningAccount, onStateChange]);

  const loadServiceStatus = useCallback(async () => {
    try {
      const status = await getServiceStatus();
      if (mounted.current) {
        setServiceStatus(status);
      }
    } catch (err) {
      console.error(err);
      if (mounted.current) {
        setServiceStatus({
          status: 'unknown',
        });
      }
    }
  }, [mounted]);

  // isLightningReady is a dependency so the boot-time status ('unknown' or
  // 'major' while the engine is still connecting) is replaced as soon as the
  // wallet reports ready, instead of lingering until the next poll.
  useEffect(() => {
    loadServiceStatus();
    const interval = window.setInterval(loadServiceStatus, serviceStatusPollInterval);
    return () => window.clearInterval(interval);
  }, [loadServiceStatus, isLightningReady]);

  const hasDataLoaded = balance !== undefined && payments !== undefined;

  const statusBanners = (
    <>
      <Status
        dismissibleKey="lightning-alpha-warning"
        type="warning">
        This is an alpha release intended for preview and testing. Only use lightning with a small amount of funds!
      </Status>
      <Status
        hidden={serviceStatus === undefined || serviceStatus.status === 'operational'}
        dismissibleKey=""
        type={serviceStatus?.status === 'major' ? 'error' : 'warning'}>
        {serviceStatus !== undefined && serviceStatus.status !== 'operational' && t(`lightning.serviceStatus.${serviceStatus.status}`)}
      </Status>
      <GlobalBanners devices={devices || {}} />
    </>
  );

  if (error) {
    return (
      <GuideWrapper>
        <GuidedContent>
          <Main>
            <ContentWrapper>
              {statusBanners}
            </ContentWrapper>
            <View textCenter verticallyCentered>
              <ViewHeader title={t('unknownError', { errorMessage: error })} />
            </View>
          </Main>
        </GuidedContent>
        <LightningGuide />
      </GuideWrapper>
    );
  }
  if (
    lightningAccount === undefined
    || isLightningReady === undefined
    || (lightningAccount && !isLightningReady)
  ) {
    return <Spinner text={t('lightning.initializing')} />;
  }

  const canSend = balance && balance.hasAvailable;

  const initializingSpinnerText =
    syncedAddressesCount !== undefined && syncedAddressesCount > 1
      ? '\n' +
        t('account.syncedAddressesCount', {
          count: syncedAddressesCount.toString(),
          defaultValue: 0
        } as any)
      : '';

  if (!hasDataLoaded) {
    return (
      <LightningPageLayout
        accountDataLoaded={false}
        balance={balance}
        canSend={canSend}
        statusBanners={statusBanners}
      >
        {initializingSpinnerText ? (
          <Spinner text={initializingSpinnerText} />
        ) : (
          <TransactionHistorySkeleton />
        )}
      </LightningPageLayout>
    );
  }

  return (
    <LightningInner
      balance={balance}
      explorerURL={blockExplorerTxPrefix}
      payments={payments}
      statusBanners={statusBanners}
    />
  );
};
