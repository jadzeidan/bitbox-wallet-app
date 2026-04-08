// SPDX-License-Identifier: Apache-2.0

package market

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testRSSItem struct {
	description string
	link        string
	pubDate     string
	title       string
}

func makeRSS(items []testRSSItem) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel>`)
	for _, item := range items {
		builder.WriteString("<item>")
		builder.WriteString("<title>" + html.EscapeString(item.title) + "</title>")
		builder.WriteString("<link>" + html.EscapeString(item.link) + "</link>")
		builder.WriteString("<description>" + html.EscapeString(item.description) + "</description>")
		builder.WriteString("<pubDate>" + html.EscapeString(item.pubDate) + "</pubDate>")
		builder.WriteString("</item>")
	}
	builder.WriteString("</channel></rss>")
	return builder.String()
}

func makeRSSServer(feed string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
}

func makeStatusServer(statusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(statusCode)
	}))
}

func TestBitcoinNewsAggregatesAcrossFeedsAndSortsByDate(t *testing.T) {
	older := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC).Format(time.RFC1123Z)
	newer := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC1123Z)

	feedA := makeRSS([]testRSSItem{
		{
			title:       "Bitcoin adoption rises in test market",
			description: "Some summary",
			link:        "https://example.com/a",
			pubDate:     older,
		},
		{
			title:       "Ethereum only article",
			description: "Altcoin update only",
			link:        "https://example.com/non-btc",
			pubDate:     newer,
		},
	})
	feedB := makeRSS([]testRSSItem{
		{
			title:       "Bitcoin volatility cools after rally",
			description: "Some summary",
			link:        "https://example.com/b",
			pubDate:     newer,
		},
	})

	serverA := makeRSSServer(feedA)
	defer serverA.Close()
	serverB := makeRSSServer(feedB)
	defer serverB.Close()

	previousFeeds := newsFeeds
	t.Cleanup(func() {
		newsFeeds = previousFeeds
	})
	newsFeeds = []newsFeed{
		{name: "Feed A", url: serverA.URL},
		{name: "Feed B", url: serverB.URL},
	}

	articles, err := BitcoinNews(nil)
	require.NoError(t, err)
	require.Len(t, articles, 2)
	require.Equal(t, "Feed B", articles[0].Source)
	require.Equal(t, "https://example.com/b", articles[0].URL)
	require.Equal(t, "Feed A", articles[1].Source)
	require.Equal(t, "https://example.com/a", articles[1].URL)
}

func TestBitcoinNewsLimitsToSixAndDeduplicatesByURL(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	items := make([]testRSSItem, 0, 8)
	for i := 0; i < 8; i++ {
		items = append(items, testRSSItem{
			title:       fmt.Sprintf("Bitcoin headline %d", i),
			description: "Some summary",
			link:        fmt.Sprintf("https://example.com/article-%d", i),
			pubDate:     now.Add(-time.Duration(i) * time.Hour).Format(time.RFC1123Z),
		})
	}
	// Duplicate URL with a newer timestamp in a second feed; should appear only once.
	duplicateFeed := makeRSS([]testRSSItem{
		{
			title:       "Bitcoin duplicate",
			description: "Some summary",
			link:        "https://example.com/article-0",
			pubDate:     now.Add(time.Hour).Format(time.RFC1123Z),
		},
	})

	serverA := makeRSSServer(makeRSS(items))
	defer serverA.Close()
	serverB := makeRSSServer(duplicateFeed)
	defer serverB.Close()

	previousFeeds := newsFeeds
	t.Cleanup(func() {
		newsFeeds = previousFeeds
	})
	newsFeeds = []newsFeed{
		{name: "Feed A", url: serverA.URL},
		{name: "Feed B", url: serverB.URL},
	}

	articles, err := BitcoinNews(nil)
	require.NoError(t, err)
	require.Len(t, articles, 6)

	seen := map[string]struct{}{}
	for _, article := range articles {
		_, exists := seen[article.URL]
		require.False(t, exists, "duplicate URL in articles")
		seen[article.URL] = struct{}{}
	}
}

func TestBitcoinNewsAlwaysIncludesBitBoxWhenAvailable(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	bitBoxFeed := makeRSS([]testRSSItem{
		{
			title:       "New BitBox feature release",
			description: "Product update from BitBox team",
			link:        "https://blog.bitbox.swiss/en/new-feature/",
			pubDate:     now.Add(-24 * time.Hour).Format(time.RFC1123Z),
		},
	})

	otherItems := make([]testRSSItem, 0, 6)
	for i := 0; i < 6; i++ {
		otherItems = append(otherItems, testRSSItem{
			title:       fmt.Sprintf("Bitcoin market item %d", i),
			description: "Market summary",
			link:        fmt.Sprintf("https://example.com/market-%d", i),
			pubDate:     now.Add(-time.Duration(i) * time.Hour).Format(time.RFC1123Z),
		})
	}

	bitBoxServer := makeRSSServer(bitBoxFeed)
	defer bitBoxServer.Close()
	otherServer := makeRSSServer(makeRSS(otherItems))
	defer otherServer.Close()

	previousFeeds := newsFeeds
	t.Cleanup(func() {
		newsFeeds = previousFeeds
	})
	newsFeeds = []newsFeed{
		{name: bitBoxBlogName, url: bitBoxServer.URL},
		{name: "Other", url: otherServer.URL},
	}

	articles, err := BitcoinNews(nil)
	require.NoError(t, err)
	require.Len(t, articles, 6)
	require.True(t, hasSource(articles, bitBoxBlogName))
}

func TestBitcoinNewsReturnsBitBoxWhenOnlyNonBitcoinBitBoxExists(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	bitBoxFeed := makeRSS([]testRSSItem{
		{
			title:       "BitBox app release notes",
			description: "General product update",
			link:        "https://blog.bitbox.swiss/en/release-notes/",
			pubDate:     now.Format(time.RFC1123Z),
		},
	})
	otherFeed := makeRSS([]testRSSItem{
		{
			title:       "Ethereum update",
			description: "Altcoin article",
			link:        "https://example.com/eth-only",
			pubDate:     now.Format(time.RFC1123Z),
		},
	})

	bitBoxServer := makeRSSServer(bitBoxFeed)
	defer bitBoxServer.Close()
	otherServer := makeRSSServer(otherFeed)
	defer otherServer.Close()

	previousFeeds := newsFeeds
	t.Cleanup(func() {
		newsFeeds = previousFeeds
	})
	newsFeeds = []newsFeed{
		{name: bitBoxBlogName, url: bitBoxServer.URL},
		{name: "Other", url: otherServer.URL},
	}

	articles, err := BitcoinNews(nil)
	require.NoError(t, err)
	require.Len(t, articles, 1)
	require.Equal(t, bitBoxBlogName, articles[0].Source)
	require.Equal(t, "https://blog.bitbox.swiss/en/release-notes/", articles[0].URL)
}

func TestBitcoinNewsFallsBackToStaticBitBoxWhenBitBoxFeedUnavailable(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	bitBoxServer := makeStatusServer(http.StatusInternalServerError)
	defer bitBoxServer.Close()
	otherServer := makeRSSServer(makeRSS([]testRSSItem{
		{
			title:       "Bitcoin market update",
			description: "Market summary",
			link:        "https://example.com/btc-market",
			pubDate:     now.Format(time.RFC1123Z),
		},
	}))
	defer otherServer.Close()

	previousFeeds := newsFeeds
	t.Cleanup(func() {
		newsFeeds = previousFeeds
	})
	newsFeeds = []newsFeed{
		{name: bitBoxBlogName, url: bitBoxServer.URL},
		{name: "Other", url: otherServer.URL},
	}

	articles, err := BitcoinNews(nil)
	require.NoError(t, err)
	require.True(t, hasSource(articles, bitBoxBlogName))
}
