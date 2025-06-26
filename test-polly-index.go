package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
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

type AzureDocument struct {
	SearchAction string `json:"@search.action"`
	ID           string `json:"id"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	URL          string `json:"url"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	IndexedAt    string `json:"indexed_at"`
}

type AzureBatchRequest struct {
	Value []AzureDocument `json:"value"`
}

func main() {
	// Test with the Polly article
	pollyURL := "https://support.talkdesk.com/hc/en-us/articles/7411707908635-Talkdesk-Studio-Text-to-Speech-Powered-by-Amazon-Polly"
	
	fmt.Printf("🔍 Testing with Polly article: %s\n", pollyURL)
	
	// Scrape the article
	article, err := scrapeFullArticle(pollyURL, 15*time.Second)
	if err != nil {
		fmt.Printf("❌ Error scraping article: %v\n", err)
		return
	}
	
	fmt.Printf("✅ Scraped article: %s\n", article.Title)
	fmt.Printf("📝 Content length: %d characters\n", len(article.Body))
	fmt.Printf("🔗 URL: %s\n", article.URL)
	
	// Index to Azure
	azureConfig := struct {
		ServiceName string
		ApiKey      string
		IndexName   string
	}{
		ServiceName: os.Getenv("AZURE_SEARCH_SERVICE"),
		ApiKey:      os.Getenv("AZURE_SEARCH_KEY"),
		IndexName:   os.Getenv("AZURE_SEARCH_INDEX"),
	}
	
	if err := indexToAzure(article, azureConfig); err != nil {
		fmt.Printf("❌ Error indexing to Azure: %v\n", err)
		return
	}
	
	fmt.Printf("🔍 Successfully indexed to Azure Search!\n")
	fmt.Printf("🌐 Try searching for 'polly' at: http://talkdesk-search-demo.eastus.azurecontainer.io:8080\n")
}

func scrapeFullArticle(articleURL string, timeout time.Duration) (*Article, error) {
	c := colly.NewCollector()
	c.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	c.SetRequestTimeout(timeout)

	var article Article
	var scrapeErr error

	article.URL = articleURL

	// Extract article ID from URL
	re := regexp.MustCompile(`/articles/(\d+)`)
	matches := re.FindStringSubmatch(articleURL)
	if len(matches) >= 2 {
		article.ID = matches[1]
	}

	// Extract title
	c.OnHTML("h1, .article-title, [data-testid='article-title']", func(e *colly.HTMLElement) {
		if article.Title == "" {
			article.Title = strings.TrimSpace(e.Text)
		}
	})

	// Extract the main article content with full HTML
	c.OnHTML(".article-body, .article-content, [data-testid='article-body'], .article__body, .article-body-container, .article-content-body, article, main", func(e *colly.HTMLElement) {
		if article.Body == "" {
			if html, err := e.DOM.Html(); err == nil {
				article.Body = html
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

	if scrapeErr != nil {
		return nil, fmt.Errorf("scraping error: %v", scrapeErr)
	}

	if article.Title == "" || article.Title == "How can we help?" {
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

func indexToAzure(article *Article, config struct{ ServiceName, ApiKey, IndexName string }) error {
	// Transform article data for Azure Cognitive Search
	doc := AzureDocument{
		SearchAction: "mergeOrUpload",
		ID:           article.ID,
		Title:        article.Title,
		Body:         article.Body,
		URL:          article.URL,
		CreatedAt:    article.CreatedAt,
		UpdatedAt:    article.UpdatedAt,
		IndexedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	// Create batch request
	batchRequest := AzureBatchRequest{
		Value: []AzureDocument{doc},
	}

	// Convert to JSON
	jsonData, err := json.Marshal(batchRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal document: %v", err)
	}

	// Create Azure Search request
	azureURL := fmt.Sprintf("https://%s.search.windows.net/indexes/%s/docs/index?api-version=2021-04-30-Preview",
		config.ServiceName, config.IndexName)
	
	req, err := http.NewRequest("POST", azureURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create Azure request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", config.ApiKey)

	// Send to Azure Cognitive Search with timeout
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to index to Azure: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Azure indexing failed with status %d", resp.StatusCode)
	}

	fmt.Printf("🔍 Indexed to Azure Search: %s\n", article.Title)
	return nil
}