// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getMarketNews, TMarketNewsArticle, TMarketNewsResponse } from '@/api/market';
import { A } from '@/components/anchor/anchor';
import { GlobalBanners } from '@/components/banners';
import { ContentWrapper } from '@/components/contentwrapper/contentwrapper';
import { ExternalLink } from '@/components/icon';
import { GuideWrapper, GuidedContent, Header, Main } from '@/components/layout';
import { Spinner } from '@/components/spinner/Spinner';
import { View, ViewContent } from '@/components/view/view';
import { useDarkmode } from '@/hooks/darkmode';
import styles from './markets.module.css';

const FALLBACK_NEWS_ARTICLES: TMarketNewsArticle[] = [
  {
    publishedAt: '2026-04-06T00:00:00Z',
    source: 'Bitcoin Magazine',
    summary: 'Bitcoin moved back above $70,000 as geopolitical headlines, oil volatility, and ETF flows kept traders focused on macro risk.',
    title: 'Bitcoin Price Briefly Tops $70,000 as Donald Trump’s Iran Signals Rattle Markets',
    url: 'https://bitcoinmagazine.com/markets/bitcoin-price-briefly-tops-70000'
  },
  {
    publishedAt: '2026-04-02T00:00:00Z',
    source: 'Bitcoin Magazine',
    summary: 'Bitcoin slid after renewed escalation signals triggered a broader risk-off move and raised the odds of a deeper pullback.',
    title: 'Bitcoin Price Continues Sliding as President Trump Signals Iran Escalation, Raising Risk of Drop Toward $60,000',
    url: 'https://bitcoinmagazine.com/markets/bitcoin-price-continues-sliding'
  },
  {
    publishedAt: '2026-03-31T00:00:00Z',
    source: 'Bitcoin Magazine',
    summary: 'Exchange inflows and institutional absorption highlighted a split market as Bitcoin drifted toward another weak monthly finish.',
    title: 'Bitcoin Price Faces Rising Sell Pressure as ETF Demand Absorbs Distribution',
    url: 'https://bitcoinmagazine.com/markets/bitcoin-price-faces-rising-selling'
  },
  {
    publishedAt: '2026-03-28T00:00:00Z',
    source: 'Bitcoin Magazine',
    summary: 'Bitcoin stayed range-bound as Iran negotiations, macro pressure, and options positioning kept traders focused on headline risk.',
    title: 'Bitcoin Price Teeters on Iran Talks as Geopolitics and Options Flows Trap Price in Narrow Range',
    url: 'https://bitcoinmagazine.com/markets/bitcoin-price-teeters-on-iran-talks'
  },
  {
    publishedAt: '2026-03-27T00:00:00Z',
    source: 'Bitcoin Magazine',
    summary: 'The Bitcoin Fear and Greed Index dropped to 13, reflecting extreme fear as price weakness and liquidations hit market sentiment.',
    title: 'Bitcoin Fear and Greed Index Hits Extreme Fear at 13 Out of 100',
    url: 'https://bitcoinmagazine.com/markets/bitcoin-fear-index-hits-extreme-fear'
  },
  {
    publishedAt: '2026-03-27T00:00:00Z',
    source: 'Bitcoin Magazine',
    summary: 'Bitcoin fell to a two-week low as long liquidations topped $300 million and macro stress weighed on broader risk appetite.',
    title: 'Bitcoin Price Slides to Two-Week Low as Liquidations Top $300 Million and Macro Pressure Builds',
    url: 'https://bitcoinmagazine.com/markets/bitcoin-price-slides-to-two-week-low'
  }
];

export const Markets = () => {
  const { t, i18n } = useTranslation();
  const { isDarkMode } = useDarkmode();
  const chartRef = useRef<HTMLDivElement>(null);
  const [marketNews, setMarketNews] = useState<TMarketNewsResponse>();
  const chartWidgetClassName = [styles.chartWidget, 'tradingview-widget-container']
    .filter((className): className is string => Boolean(className))
    .join(' ');

  useEffect(() => {
    const chartElement = chartRef.current;

    if (!chartElement) {
      return;
    }

    const previousScript = chartElement.querySelector('script');
    if (previousScript) {
      previousScript.remove();
    }

    const script = document.createElement('script');
    script.type = 'text/javascript';
    script.src = 'https://s3.tradingview.com/external-embedding/embed-widget-advanced-chart.js';
    script.async = true;
    script.text = JSON.stringify({
      calendar: false,
      details: false,
      height: 310,
      hide_side_toolbar: true,
      hide_top_toolbar: true,
      interval: '240',
      locale: 'en',
      range: '1M',
      save_image: false,
      style: '1',
      symbol: 'BITSTAMP:BTCUSD',
      theme: isDarkMode ? 'dark' : 'light',
      timezone: 'Etc/UTC',
      width: '100%',
      withdateranges: true
    });

    chartElement.appendChild(script);

    return () => {
      chartElement.innerHTML = '';
    };
  }, [isDarkMode]);

  useEffect(() => {
    let cancelled = false;

    const loadNews = async () => {
      try {
        const response = await getMarketNews();
        if (!cancelled) {
          setMarketNews(response);
        }
      } catch {
        if (!cancelled) {
          setMarketNews({
            success: false,
            errorMessage: 'loadFailed',
          });
        }
      }
    };

    loadNews();
    const intervalID = window.setInterval(loadNews, 15 * 60 * 1000);

    return () => {
      cancelled = true;
      window.clearInterval(intervalID);
    };
  }, []);

  const formatDate = (date: string) => (
    new Intl.DateTimeFormat(i18n.language, {
      day: 'numeric',
      month: 'short',
      year: 'numeric',
    }).format(new Date(date))
  );

  const articles = marketNews?.success ? marketNews.articles : FALLBACK_NEWS_ARTICLES;
  const showingFallbackNews = marketNews?.success === false;

  return (
    <GuideWrapper>
      <GuidedContent>
        <Main>
          <ContentWrapper>
            <GlobalBanners devices={{}} />
          </ContentWrapper>
          <Header title={<h2>{t('markets.title')}</h2>} />
          <View>
            <ViewContent>
              <div className={styles.container}>
                <div className={styles.chartHeader}>
                  <div>
                    <p className={styles.eyebrow}>{t('markets.price.heading')}</p>
                    <h3 className={styles.chartTitle}>{t('markets.price.title')}</h3>
                  </div>
                </div>

                <div className={styles.chartContainer}>
                  <div className={chartWidgetClassName} ref={chartRef}>
                    <div className="tradingview-widget-container__widget" />
                  </div>
                </div>

                <section className={styles.section}>
                  <div className={styles.sectionHeader}>
                    <div>
                      <p className={styles.eyebrow}>{t('markets.news.heading')}</p>
                      <h3 className={styles.sectionTitle}>{t('markets.news.title')}</h3>
                    </div>
                    <div>
                      <p className={styles.sectionText}>{t('markets.news.description')}</p>
                      {showingFallbackNews && (
                        <p className={styles.sectionText}>{t('markets.news.fallbackNotice')}</p>
                      )}
                    </div>
                  </div>

                  <div className={styles.newsGrid}>
                    {!marketNews && <Spinner text={t('loading')} />}
                    {articles.map(({ publishedAt, source, summary, title, url }) => (
                      <A
                        key={url}
                        className={styles.newsCard}
                        href={url}
                        title={title}
                      >
                        <div className={styles.newsMeta}>
                          <span className={styles.newsSource}>{source}</span>
                          <span className={styles.newsDate}>{formatDate(publishedAt)}</span>
                        </div>
                        <div className={styles.newsCardHeader}>
                          <span className={styles.newsTitle}>{title}</span>
                          <ExternalLink />
                        </div>
                        <p className={styles.newsCopy}>{summary}</p>
                      </A>
                    ))}
                  </div>
                </section>
              </div>
            </ViewContent>
          </View>
        </Main>
      </GuidedContent>
    </GuideWrapper>
  );
};
