package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
)

type Article struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	HTMLURL   string `json:"html_url"`
	SectionID int64  `json:"section_id"`
}

type ArticleResponse struct {
	Article Article `json:"article"`
}

func main() {
	releaseNotesURLs := []string{
		"https://support.talkdesk.com/hc/en-us/sections/200263245-Release-Notes?page=1#articles",
		"https://support.talkdesk.com/hc/en-us/sections/200263245-Release-Notes?page=2#articles",
	}

	c := colly.NewCollector()

	// Set a realistic User-Agent to avoid being blocked
	c.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"

	// Add some delays to be respectful
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 1,
		Delay:       1 * time.Second,
	})

	// Store article URLs
	var articleURLs []string

	// Extract article links from release notes pages
	c.OnHTML("a[href*='/articles/']", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		// Convert relative URLs to absolute
		if strings.HasPrefix(href, "/") {
			href = "https://support.talkdesk.com" + href
		}

		// Only collect article URLs, avoid duplicates
		for _, existing := range articleURLs {
			if existing == href {
				return
			}
		}
		articleURLs = append(articleURLs, href)
		fmt.Printf("Found article: %s\n", href)
	})

	c.OnRequest(func(r *colly.Request) {
		fmt.Printf("Visiting: %s\n", r.URL)
	})

	c.OnError(func(r *colly.Response, err error) {
		fmt.Printf("Error: %s\n", err.Error())
	})

	// Visit release notes pages to collect article URLs
	for _, url := range releaseNotesURLs {
		c.Visit(url)
	}

	fmt.Printf("\nFound %d articles. Fetching JSON data...\n", len(articleURLs))

	// Fetch JSON data concurrently with rate limiting
	maxConcurrency := 3 // Limit concurrent requests
	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var articles []ArticleResponse

	for _, articleURL := range articleURLs {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Add delay between requests
			time.Sleep(500 * time.Millisecond)

			jsonData := getArticleJSON(url)
			if jsonData != nil {
				mu.Lock()
				articles = append(articles, *jsonData)
				fmt.Printf("Fetched: %s\n", jsonData.Article.Title)
				mu.Unlock()
			}
		}(articleURL)
	}

	wg.Wait()
	fmt.Printf("\nSuccessfully fetched %d articles\n", len(articles))
}

func getArticleJSON(articleURL string) *ArticleResponse {
	// Extract article ID from URL
	re := regexp.MustCompile(`/articles/(\d+)`)
	matches := re.FindStringSubmatch(articleURL)
	if len(matches) < 2 {
		fmt.Printf("Could not extract article ID from: %s\n", articleURL)
		return nil
	}

	articleID := matches[1]

	// Zendesk API endpoint for article JSON
	jsonURL := fmt.Sprintf("https://support.talkdesk.com/api/v2/help_center/articles/%s.json", articleID)

	// Create HTTP client with realistic headers
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", jsonURL, nil)
	if err != nil {
		fmt.Printf("Error creating request for article %s: %v\n", articleID, err)
		return nil
	}

	// Set realistic headers
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error fetching JSON for article %s: %v\n", articleID, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Printf("HTTP %d for article %s JSON\n", resp.StatusCode, articleID)
		return nil
	}

	var articleData ArticleResponse
	if err := json.NewDecoder(resp.Body).Decode(&articleData); err != nil {
		fmt.Printf("Error decoding JSON for article %s: %v\n", articleID, err)
		return nil
	}

	return &articleData
}
