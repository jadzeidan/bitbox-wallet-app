// SPDX-License-Identifier: Apache-2.0

package market

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxBitcoinNews    = 6
	newsRequestTimout = 15 * time.Second
	bitBoxBlogName    = "BitBox Blog"
)

var (
	htmlTagPattern        = regexp.MustCompile("<[^>]+>")
	fallbackBitBoxArticle = NewsArticle{
		PublishedAt: "",
		Summary:     "Product updates, security insights, and ecosystem news from the BitBox team.",
		Title:       "Latest from the BitBox Blog",
		URL:         "https://blog.bitbox.swiss/en/",
		Source:      bitBoxBlogName,
	}
)

type newsFeed struct {
	name string
	url  string
}

var newsFeeds = []newsFeed{
	{
		name: bitBoxBlogName,
		url:  "https://blog.bitbox.swiss/en/rss/",
	},
	{
		name: "Bitcoin Magazine",
		url:  "https://bitcoinmagazine.com/feed",
	},
	{
		name: "NewsBTC",
		url:  "https://www.newsbtc.com/feed/",
	},
	{
		name: "CoinDesk",
		url:  "https://www.coindesk.com/arc/outboundfeeds/rss/",
	},
}

// NewsArticle is a bitcoin market news article for the frontend.
type NewsArticle struct {
	PublishedAt string `json:"publishedAt"`
	Summary     string `json:"summary"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Source      string `json:"source"`
}

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Description string `xml:"description"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Title       string `xml:"title"`
}

// BitcoinNews returns up to 6 recent bitcoin-related articles from configured RSS feeds.
func BitcoinNews(httpClient *http.Client) ([]NewsArticle, error) {
	allArticles := make([]NewsArticle, 0, maxBitcoinNews*len(newsFeeds))
	var lastErr error
	var latestBitBoxArticle *NewsArticle
	bitBoxFeedConfigured := false

	for _, feedSource := range newsFeeds {
		if feedSource.name == bitBoxBlogName {
			bitBoxFeedConfigured = true
		}
		articles, err := fetchBitcoinNewsFromFeed(httpClient, feedSource)
		if err != nil {
			lastErr = err
			if feedSource.name != bitBoxBlogName {
				continue
			}
		}

		if feedSource.name == bitBoxBlogName {
			bitBoxArticles, bitBoxErr := fetchLatestNewsFromFeed(httpClient, feedSource)
			if bitBoxErr == nil && len(bitBoxArticles) > 0 {
				article := bitBoxArticles[0]
				latestBitBoxArticle = &article
			}
		}

		allArticles = append(allArticles, articles...)
	}

	if len(allArticles) == 0 {
		if latestBitBoxArticle != nil {
			return []NewsArticle{*latestBitBoxArticle}, nil
		}
		if bitBoxFeedConfigured && lastErr != nil {
			return []NewsArticle{fallbackBitBoxArticle}, nil
		}
		if bitBoxFeedConfigured {
			return []NewsArticle{fallbackBitBoxArticle}, nil
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no bitcoin news available")
	}

	sortNewsArticles(allArticles)

	uniqueArticles := make([]NewsArticle, 0, maxBitcoinNews)
	seenURLs := map[string]struct{}{}
	for _, article := range allArticles {
		if _, seen := seenURLs[article.URL]; seen {
			continue
		}
		seenURLs[article.URL] = struct{}{}
		uniqueArticles = append(uniqueArticles, article)
		if len(uniqueArticles) == maxBitcoinNews {
			break
		}
	}

	if bitBoxFeedConfigured && !hasSource(uniqueArticles, bitBoxBlogName) {
		bitBoxArticle := fallbackBitBoxArticle
		if latestBitBoxArticle != nil {
			bitBoxArticle = *latestBitBoxArticle
		}
		if len(uniqueArticles) == maxBitcoinNews {
			uniqueArticles = uniqueArticles[:maxBitcoinNews-1]
		}
		uniqueArticles = append(uniqueArticles, bitBoxArticle)
		sortNewsArticles(uniqueArticles)
	}

	return uniqueArticles, nil
}

func isBitcoinNews(title string, summary string) bool {
	text := strings.ToLower(title + " " + summary)
	return strings.Contains(text, "bitcoin") || strings.Contains(text, " btc ")
}

func cleanText(input string) string {
	withoutTags := htmlTagPattern.ReplaceAllString(input, " ")
	unescaped := html.UnescapeString(withoutTags)
	return strings.Join(strings.Fields(strings.TrimSpace(unescaped)), " ")
}

func summarize(input string, maxLen int) string {
	if len(input) <= maxLen {
		return input
	}
	return strings.TrimSpace(input[:maxLen-3]) + "..."
}

func parsePubDate(value string) string {
	for _, layout := range []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.RFC3339,
	} {
		timestamp, err := time.Parse(layout, strings.TrimSpace(value))
		if err == nil {
			return timestamp.UTC().Format(time.RFC3339)
		}
	}
	return strings.TrimSpace(value)
}

func parseRFC3339(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func sortNewsArticles(articles []NewsArticle) {
	sort.SliceStable(articles, func(i, j int) bool {
		iTime, iOK := parseRFC3339(articles[i].PublishedAt)
		jTime, jOK := parseRFC3339(articles[j].PublishedAt)

		if iOK && jOK {
			return iTime.After(jTime)
		}
		if iOK != jOK {
			return iOK
		}

		// Fallback to deterministic ordering if a feed has an invalid date format.
		return articles[i].PublishedAt > articles[j].PublishedAt
	})
}

func hasSource(articles []NewsArticle, source string) bool {
	for _, article := range articles {
		if article.Source == source {
			return true
		}
	}
	return false
}

func normalizeNewsURL(rawURL string) string {
	trimmedURL := strings.TrimSpace(rawURL)
	parsedURL, err := neturl.Parse(trimmedURL)
	if err != nil || parsedURL.Host == "" {
		return trimmedURL
	}

	parsedURL.Scheme = "https"
	parsedURL.Host = strings.ToLower(parsedURL.Host)
	return parsedURL.String()
}

func fetchBitcoinNewsFromFeed(httpClient *http.Client, feedSource newsFeed) ([]NewsArticle, error) {
	return fetchNewsFromFeed(httpClient, feedSource, true, maxBitcoinNews)
}

func fetchLatestNewsFromFeed(httpClient *http.Client, feedSource newsFeed) ([]NewsArticle, error) {
	return fetchNewsFromFeed(httpClient, feedSource, false, 1)
}

func fetchNewsFromFeed(httpClient *http.Client, feedSource newsFeed, onlyBitcoin bool, maxArticles int) ([]NewsArticle, error) {
	ctx, cancel := context.WithTimeout(context.Background(), newsRequestTimout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feedSource.url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "BitBoxApp/markets-news")

	client := httpClient
	if client == nil {
		client = &http.Client{Timeout: newsRequestTimout}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("failed to fetch market news from %s: %s", feedSource.name, response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}

	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}

	articles := make([]NewsArticle, 0, maxArticles)
	for _, item := range feed.Channel.Items {
		title := cleanText(item.Title)
		summary := cleanText(item.Description)
		if onlyBitcoin && !isBitcoinNews(title, summary) {
			continue
		}
		if title == "" || item.Link == "" {
			continue
		}
		articles = append(articles, NewsArticle{
			PublishedAt: parsePubDate(item.PubDate),
			Summary:     summarize(summary, 220),
			Title:       title,
			URL:         normalizeNewsURL(item.Link),
			Source:      feedSource.name,
		})
		if len(articles) == maxArticles {
			break
		}
	}

	if len(articles) == 0 {
		if !onlyBitcoin {
			return nil, fmt.Errorf("no market news available from %s", feedSource.name)
		}
		return nil, fmt.Errorf("no bitcoin news available from %s", feedSource.name)
	}

	return articles, nil
}
