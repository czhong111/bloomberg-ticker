package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseRSS(t *testing.T) {
	const document = `<rss version="2.0"><channel>
		<item><title>全球市场上涨 &amp; 投资者关注</title></item>
		<item><title><![CDATA[科技公司发布新品]]></title></item>
		<item><title>   </title></item>
	</channel></rss>`

	items, err := parseRSS(strings.NewReader(document), "美国 · NPR")
	if err != nil {
		t.Fatalf("parseRSS returned an error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Title != "全球市场上涨 & 投资者关注" || items[0].Source != "美国 · NPR" {
		t.Fatalf("unexpected first item: %#v", items[0])
	}
	if items[1].Title != "科技公司发布新品" {
		t.Fatalf("unexpected second title: %q", items[1].Title)
	}
}

func TestParseRSSRejectsMalformedXML(t *testing.T) {
	_, err := parseRSS(strings.NewReader(`<rss><channel>`), "要闻")
	if err == nil {
		t.Fatal("parseRSS accepted malformed XML")
	}
}

func TestRefreshNewsFailureUpdatesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	originalFeeds := feeds
	dataMu.Lock()
	originalItems := newsItems
	originalStatus := statusText
	originalTicker := tickerText
	originalVersion := tickerVersion
	newsItems = nil
	statusText = "正在连接实时新闻源…"
	tickerText = statusText
	tickerVersion = 0
	dataMu.Unlock()
	feeds = []feed{{source: "test", url: server.URL}}
	defer func() {
		feeds = originalFeeds
		dataMu.Lock()
		newsItems = originalItems
		statusText = originalStatus
		tickerText = originalTicker
		tickerVersion = originalVersion
		dataMu.Unlock()
	}()

	refreshNews(server.Client())

	dataMu.RLock()
	defer dataMu.RUnlock()
	if !strings.Contains(statusText, "连接失败") {
		t.Fatalf("status remained %q after the feed failed", statusText)
	}
}

func TestRefreshNewsShowsPublisher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<rss version="2.0"><channel><item><title>测试快讯标题</title></item></channel></rss>`))
	}))
	defer server.Close()

	originalFeeds := feeds
	dataMu.Lock()
	originalItems := newsItems
	originalStatus := statusText
	originalTicker := tickerText
	originalVersion := tickerVersion
	newsItems = nil
	statusText = "正在连接实时新闻源…"
	tickerText = statusText
	tickerVersion = 0
	dataMu.Unlock()
	feeds = []feed{{source: "新加坡 · CNA", url: server.URL}}
	defer func() {
		feeds = originalFeeds
		dataMu.Lock()
		newsItems = originalItems
		statusText = originalStatus
		tickerText = originalTicker
		tickerVersion = originalVersion
		dataMu.Unlock()
	}()

	refreshNews(server.Client())

	dataMu.RLock()
	defer dataMu.RUnlock()
	if !strings.Contains(tickerText, "[新加坡 · CNA] 测试快讯标题") {
		t.Fatalf("ticker omitted the publisher: %q", tickerText)
	}
}
