// SPDX-License-Identifier: Apache-2.0

import { useTranslation } from 'react-i18next';
import { useDarkmode } from '@/hooks/darkmode';
import { GuideWrapper, GuidedContent, Main, Header } from '@/components/layout';
import { View } from '@/components/view/view';
import { HideAmountsButton } from '@/components/hideamountsbutton/hideamountsbutton';
import { A } from '@/components/anchor/anchor';
import { PortfolioTabs } from './portfolio-tabs';
import style from './markets.module.css';

type TNewsArticle = {
  source: string;
  title: string;
  description: string;
  url: string;
};

const NEWS_ARTICLES: TNewsArticle[] = [
  {
    source: 'Bitcoin Magazine',
    title: 'Bitcoin Surpasses $100K as Institutional Demand Surges',
    description: 'Major financial institutions continue to increase their Bitcoin holdings, pushing the price to new all-time highs.',
    url: 'https://bitcoinmagazine.com/',
  },
  {
    source: 'CoinDesk',
    title: 'Lightning Network Capacity Hits Record Levels in 2026',
    description: 'The Bitcoin Lightning Network continues its growth trajectory with record-breaking capacity and adoption metrics.',
    url: 'https://www.coindesk.com/',
  },
  {
    source: 'Cointelegraph',
    title: 'Self-Custody Adoption Grows as Users Prioritize Security',
    description: 'More Bitcoin holders are moving to hardware wallets and self-custody solutions amid growing security awareness.',
    url: 'https://cointelegraph.com/',
  },
];

type TBlogArticle = {
  title: string;
  description: string;
  url: string;
};

const BLOG_ARTICLES: TBlogArticle[] = [
  {
    title: 'How Bitcoin vaults combine convenience with security',
    description: 'Learn how Bitcoin vaults add an extra layer of protection by introducing time-delayed withdrawals and recovery options.',
    url: 'https://blog.bitbox.swiss/en/how-bitcoin-vaults-combine-convenience-with-security/',
  },
  {
    title: 'Secure your bitcoin with BitBox and Unchained',
    description: 'Discover how to use multisig security with BitBox and Unchained for enhanced protection of your Bitcoin holdings.',
    url: 'https://blog.bitbox.swiss/en/secure-your-bitcoin-with-bitbox-and-unchained/',
  },
];

const TradingViewChart = () => {
  const { isDarkMode } = useDarkmode();
  const theme = isDarkMode ? 'dark' : 'light';

  const src = `https://s.tradingview.com/widgetembed/?frameElementId=tradingview_chart&symbol=COINBASE%3ABTCUSD&interval=D&theme=${theme}&style=1&locale=en&toolbarbg=f1f3f6&enable_publishing=false&allow_symbol_change=false&save_image=false&hideideas=1&hide_top_toolbar=0&hide_legend=0&withdateranges=1`;

  return (
    <div className={style.chartContainer}>
      <iframe
        key={theme}
        title="TradingView BTC/USD Chart"
        src={src}
        allowFullScreen
      />
    </div>
  );
};

const NewsCard = ({ source, title, description, url }: TNewsArticle) => (
  <A href={url} className={style.cardLink}>
    <div className={style.card}>
      <span className={style.cardSource}>{source}</span>
      <span className={style.cardTitle}>{title}</span>
      <span className={style.cardDescription}>{description}</span>
    </div>
  </A>
);

const BlogCard = ({ title, description, url }: TBlogArticle) => (
  <A href={url} className={style.cardLink}>
    <div className={`${style.card ?? ''} ${style.blogCard ?? ''}`}>
      <span className={style.cardSource}>BitBox Blog</span>
      <span className={style.cardTitle}>{title}</span>
      <span className={style.cardDescription}>{description}</span>
      <span className={style.readMore}>Read article &rarr;</span>
    </div>
  </A>
);

export const Markets = () => {
  const { t } = useTranslation();

  return (
    <GuideWrapper>
      <GuidedContent>
        <Main>
          <Header title={<h2>{t('accountSummary.title')}</h2>}>
            <HideAmountsButton />
          </Header>
          <PortfolioTabs />
          <View>
            <div className={style.marketsContainer}>
              <section>
                <h3 className={style.sectionTitle}>{t('accountSummary.markets.btcChart')}</h3>
                <TradingViewChart />
              </section>
              <section>
                <h3 className={style.sectionTitle}>{t('accountSummary.markets.news')}</h3>
                <div className={style.cardsGrid}>
                  {NEWS_ARTICLES.map((article) => (
                    <NewsCard key={article.url} {...article} />
                  ))}
                </div>
              </section>
              <section>
                <h3 className={style.sectionTitle}>{t('accountSummary.markets.blog')}</h3>
                <div className={style.cardsGrid}>
                  {BLOG_ARTICLES.map((article) => (
                    <BlogCard key={article.url} {...article} />
                  ))}
                </div>
              </section>
            </div>
          </View>
        </Main>
      </GuidedContent>
    </GuideWrapper>
  );
};
