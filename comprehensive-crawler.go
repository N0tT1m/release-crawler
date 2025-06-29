package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
)

type Article struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Sitemap struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []URL    `xml:"url"`
}

type URL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

type Config struct {
	FetchConcurrency int
	FetchDelay       time.Duration
	MaxRetries       int
	RequestTimeout   time.Duration
}

type FetchResult struct {
	Article *Article
	Error   error
	URL     string
	Retries int
}

type ElasticsearchConfig struct {
	Enabled  bool
	URL      string
	Index    string
	Username string
	Password string
}

type ProcessorFunc func(*Article) error

func main() {
	config := Config{
		FetchConcurrency: 15,
		FetchDelay:       200 * time.Millisecond,
		MaxRetries:       3,
		RequestTimeout:   30 * time.Second,
	}

	fmt.Println("🌍 Starting comprehensive Talkdesk documentation crawler...")

	// Phase 1: Get all URLs from sitemap
	fmt.Println("📋 Fetching sitemap...")
	articleURLs, err := fetchSitemapURLs()
	if err != nil {
		fmt.Printf("❌ Error fetching sitemap: %v\n", err)
		return
	}

	// Filter to only English article URLs
	filteredURLs := filterEnglishArticles(articleURLs)
	fmt.Printf("📄 Found %d English articles to crawl\n", len(filteredURLs))

	// Phase 2: Setup Elasticsearch
	esConfig := ElasticsearchConfig{
		Enabled: getEnvBool("ELASTICSEARCH_ENABLED", true),
		URL:     getEnv("ELASTICSEARCH_URL", "http://localhost:9200"),
		Index:   getEnv("ELASTICSEARCH_INDEX", "documentation-articles"),
	}

	var processors []ProcessorFunc
	if esConfig.Enabled {
		if err := createElasticsearchIndex(esConfig); err != nil {
			fmt.Printf("⚠ Failed to create Elasticsearch index: %v\n", err)
		}
		processors = append(processors, createElasticsearchProcessor(esConfig))
	}

	// Phase 3: Crawl all articles concurrently
	fmt.Println("🚀 Starting concurrent article crawling...")
	results := fetchArticlesConcurrently(filteredURLs, config)

	// Process results
	var successfulArticles []Article
	var errors []error

	for result := range results {
		if result.Error != nil {
			errors = append(errors, fmt.Errorf("failed to fetch %s after %d retries: %v", result.URL, result.Retries, result.Error))
		} else {
			successfulArticles = append(successfulArticles, *result.Article)
			fmt.Printf("✓ Fetched: %s\n", result.Article.Title)

			// Apply processors (e.g., Elasticsearch indexing)
			for _, processor := range processors {
				if err := processor(result.Article); err != nil {
					fmt.Printf("⚠ Processor error for %s: %v\n", result.Article.Title, err)
				}
			}
		}
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Successfully fetched: %d articles\n", len(successfulArticles))
	fmt.Printf("Failed: %d articles\n", len(errors))

	if len(errors) > 0 && len(errors) <= 10 {
		fmt.Printf("\nErrors:\n")
		for _, err := range errors {
			fmt.Printf("- %v\n", err)
		}
	} else if len(errors) > 10 {
		fmt.Printf("\nShowing first 10 errors of %d total:\n", len(errors))
		for _, err := range errors[:10] {
			fmt.Printf("- %v\n", err)
		}
	}
}

func fetchSitemapURLs() ([]string, error) {
	// Use optimized HTTP client with connection pooling
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	sitemapURL := getEnv("SITEMAP_URL", "https://support.talkdesk.com/sitemap.xml")
	resp, err := client.Get(sitemapURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sitemap from %s: %v", sitemapURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("sitemap returned HTTP %d from %s", resp.StatusCode, sitemapURL)
	}

	var sitemap Sitemap
	if err := xml.NewDecoder(resp.Body).Decode(&sitemap); err != nil {
		return nil, fmt.Errorf("failed to parse XML sitemap from %s (got HTML?): %v", sitemapURL, err)
	}

	var urls []string
	for _, url := range sitemap.URLs {
		urls = append(urls, url.Loc)
	}

	return urls, nil
}

func filterEnglishArticles(urls []string) []string {
	var filtered []string
	articlePattern := regexp.MustCompile(`/hc/en-us/articles/\d+-.+`)

	// Filter for specific patterns that are likely to be real articles
	excludePatterns := []*regexp.Regexp{
		regexp.MustCompile(`/hc/en-us/articles/\d+$`), // Articles without titles
		regexp.MustCompile(`/sections/`),              // Section pages
		regexp.MustCompile(`/categories/`),            // Category pages
		regexp.MustCompile(`/community/`),             // Community pages
	}

	for _, url := range urls {
		if articlePattern.MatchString(url) {
			// Check if URL should be excluded
			shouldExclude := false
			for _, excludePattern := range excludePatterns {
				if excludePattern.MatchString(url) {
					shouldExclude = true
					break
				}
			}

			if !shouldExclude {
				filtered = append(filtered, url)
			}
		}
	}

	return filtered
}

func fetchArticlesConcurrently(articleURLs []string, config Config) <-chan FetchResult {
	results := make(chan FetchResult, len(articleURLs))
	semaphore := make(chan struct{}, config.FetchConcurrency)
	var wg sync.WaitGroup

	for _, url := range articleURLs {
		wg.Add(1)
		go func(articleURL string) {
			defer wg.Done()

			// Acquire semaphore for concurrency control
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Implement retry logic
			var lastErr error
			for attempt := 0; attempt <= config.MaxRetries; attempt++ {
				if attempt > 0 {
					// Exponential backoff for retries
					backoff := time.Duration(attempt) * config.FetchDelay
					time.Sleep(backoff)
					fmt.Printf("Retrying %s (attempt %d/%d)\n", articleURL, attempt, config.MaxRetries)
				}

				// Rate limiting between requests with randomization
				if attempt == 0 {
					// Random delay between 200-500ms to be respectful but fast
					randomDelay := config.FetchDelay + time.Duration(rand.Intn(300))*time.Millisecond
					time.Sleep(randomDelay)
				}

				article, err := scrapeFullArticle(articleURL, config.RequestTimeout)
				if err == nil {
					results <- FetchResult{
						Article: article,
						Error:   nil,
						URL:     articleURL,
						Retries: attempt,
					}
					return
				}
				lastErr = err
			}

			// All retries failed
			results <- FetchResult{
				Article: nil,
				Error:   lastErr,
				URL:     articleURL,
				Retries: config.MaxRetries,
			}
		}(url)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

func scrapeFullArticle(articleURL string, timeout time.Duration) (*Article, error) {
	c := colly.NewCollector(
		colly.Async(true),
	)
	c.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	c.SetRequestTimeout(timeout)

	// Limit concurrent requests per domain
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 2,
		Delay:       100 * time.Millisecond,
	})

	// Add realistic headers
	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.5")
		r.Headers.Set("Accept-Encoding", "gzip, deflate, br")
		r.Headers.Set("DNT", "1")
		r.Headers.Set("Connection", "keep-alive")
		r.Headers.Set("Upgrade-Insecure-Requests", "1")
	})

	var article Article
	var scrapeErr error

	article.URL = articleURL

	// Extract article ID from URL
	re := regexp.MustCompile(`/articles/(\d+)`)
	matches := re.FindStringSubmatch(articleURL)
	if len(matches) >= 2 {
		article.ID = matches[1]
	}

	// Extract title - target the actual article header, get text before metadata
	c.OnHTML("header.article-header h3", func(e *colly.HTMLElement) {
		if article.Title == "" {
			// Get the full text and split by newlines to extract just the title
			fullText := strings.TrimSpace(e.Text)
			if fullText != "" {
				// Split by newlines and take the first non-empty line as the title
				lines := strings.Split(fullText, "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					// Skip empty lines and metadata lines
					if line != "" &&
						!strings.Contains(line, "Published") &&
						!strings.Contains(line, "Last Updated") &&
						!strings.Contains(line, "•") &&
						line != "How can we help?" &&
						line != "Knowledge Base" {
						article.Title = line
						break
					}
				}
			}
		}
	})

	// Fallback title extraction if header method fails
	c.OnHTML("h1, .article-title, [data-testid='article-title']", func(e *colly.HTMLElement) {
		if article.Title == "" {
			style := e.Attr("style")
			if !strings.Contains(style, "display: none") {
				title := strings.TrimSpace(e.Text)
				if title != "" && title != "How can we help?" && title != "Knowledge Base" {
					article.Title = title
				}
			}
		}
	})

	// Extract the main article content with full HTML - try multiple selectors
	c.OnHTML(".article-body", func(e *colly.HTMLElement) {
		if article.Body == "" {
			if html, err := e.DOM.Html(); err == nil {
				article.Body = cleanHTML(html)
			}
		}
	})

	// Fallback body selectors
	c.OnHTML(".article-content, [data-testid='article-body'], .article__body, .article-body-container", func(e *colly.HTMLElement) {
		if article.Body == "" {
			if html, err := e.DOM.Html(); err == nil {
				article.Body = cleanHTML(html)
			}
		}
	})

	// Extract metadata if available
	c.OnHTML("time[datetime], .article-created-at, .article-updated-at", func(e *colly.HTMLElement) {
		datetime := e.Attr("datetime")
		if datetime != "" {
			if article.CreatedAt == "" {
				article.CreatedAt = datetime
			}
			article.UpdatedAt = datetime
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		scrapeErr = err
	})

	// Visit the URL
	if err := c.Visit(articleURL); err != nil {
		return nil, fmt.Errorf("error visiting page: %v", err)
	}

	// Wait for async operations to complete
	c.Wait()

	if scrapeErr != nil {
		return nil, fmt.Errorf("scraping error: %v", scrapeErr)
	}

	if article.Title == "" || article.Title == "How can we help?" || article.Title == "Knowledge Base" {
		return nil, fmt.Errorf("no meaningful title found on page")
	}

	if article.Body == "" {
		return nil, fmt.Errorf("no content found on page")
	}

	// Set timestamps if not found
	if article.CreatedAt == "" {
		article.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if article.UpdatedAt == "" {
		article.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	return &article, nil
}

func createElasticsearchIndex(config ElasticsearchConfig) error {
	if !config.Enabled {
		return nil
	}

	// Check if index exists first
	checkURL := fmt.Sprintf("%s/%s", config.URL, config.Index)
	req, err := http.NewRequest("HEAD", checkURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create HEAD request: %v", err)
	}

	if config.Username != "" && config.Password != "" {
		req.SetBasicAuth(config.Username, config.Password)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to check index existence: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode == 200 {
		fmt.Printf("📋 Elasticsearch index '%s' already exists\n", config.Index)
		return nil
	}

	// Create index with mapping
	indexMapping := `{
		"mappings": {
			"properties": {
				"id": {"type": "keyword"},
				"title": {"type": "text", "analyzer": "standard"},
				"body": {"type": "text", "analyzer": "standard"},
				"url": {"type": "keyword"},
				"created_at": {"type": "date"},
				"updated_at": {"type": "date"},
				"indexed_at": {"type": "date"}
			}
		}
	}`

	createURL := fmt.Sprintf("%s/%s", config.URL, config.Index)
	req, err = http.NewRequest("PUT", createURL, strings.NewReader(indexMapping))
	if err != nil {
		return fmt.Errorf("failed to create PUT request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if config.Username != "" && config.Password != "" {
		req.SetBasicAuth(config.Username, config.Password)
	}

	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create index: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("failed to create index, status: %d", resp.StatusCode)
	}

	fmt.Printf("✅ Created Elasticsearch index '%s'\n", config.Index)
	return nil
}

func createElasticsearchProcessor(config ElasticsearchConfig) ProcessorFunc {
	return func(article *Article) error {
		if !config.Enabled {
			return nil
		}

		// Transform article data for Elasticsearch
		doc := map[string]interface{}{
			"id":         article.ID,
			"title":      article.Title,
			"body":       article.Body,
			"url":        article.URL,
			"created_at": article.CreatedAt,
			"updated_at": article.UpdatedAt,
			"indexed_at": time.Now().UTC().Format(time.RFC3339),
		}

		// Convert to JSON
		jsonData, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("failed to marshal document: %v", err)
		}

		// Create Elasticsearch request
		var esURL string
		if article.ID != "" {
			esURL = fmt.Sprintf("%s/%s/_doc/%s", config.URL, config.Index, article.ID)
		} else {
			// Use a hash of the URL as ID if no article ID available
			esURL = fmt.Sprintf("%s/%s/_doc", config.URL, config.Index)
		}

		req, err := http.NewRequest("PUT", esURL, strings.NewReader(string(jsonData)))
		if err != nil {
			return fmt.Errorf("failed to create ES request: %v", err)
		}

		req.Header.Set("Content-Type", "application/json")
		if config.Username != "" && config.Password != "" {
			req.SetBasicAuth(config.Username, config.Password)
		}

		// Send to Elasticsearch with timeout
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to index to ES: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("ES indexing failed with status %d", resp.StatusCode)
		}

		fmt.Printf("📋 Indexed to Elasticsearch: %s\n", article.Title)
		return nil
	}
}

func cleanHTML(html string) string {
	// Remove excessive div nesting and empty elements
	html = strings.ReplaceAll(html, "<div></div>", "")
	html = strings.ReplaceAll(html, "<div> </div>", "")
	html = strings.ReplaceAll(html, "<p></p>", "")
	html = strings.ReplaceAll(html, "<p> </p>", "")

	// Remove empty spans and other elements
	html = strings.ReplaceAll(html, "<span></span>", "")
	html = strings.ReplaceAll(html, "<span> </span>", "")

	// Clean up multiple consecutive line breaks and spaces
	for strings.Contains(html, "\n\n\n") {
		html = strings.ReplaceAll(html, "\n\n\n", "\n\n")
	}

	for strings.Contains(html, "  ") {
		html = strings.ReplaceAll(html, "  ", " ")
	}

	return strings.TrimSpace(html)
}

// Helper functions for environment variables
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1"
	}
	return defaultValue
}

