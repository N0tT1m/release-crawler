package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type ElasticsearchHit struct {
	Source map[string]interface{} `json:"_source"`
}

type ElasticsearchResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []ElasticsearchHit `json:"hits"`
	} `json:"hits"`
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
	azureConfig := struct {
		ServiceName string
		ApiKey      string
		IndexName   string
	}{
		ServiceName: os.Getenv("AZURE_SEARCH_SERVICE"),
		ApiKey:      os.Getenv("AZURE_SEARCH_KEY"),
		IndexName:   os.Getenv("AZURE_SEARCH_INDEX"),
	}

	if azureConfig.ApiKey == "" {
		fmt.Printf("❌ AZURE_SEARCH_KEY not set\n")
		return
	}

	fmt.Printf("🔄 Transferring data from Elasticsearch (talkdesk-docs) to Azure...\n")

	// Get all articles from the correct Elasticsearch index
	totalPages := 8 // 753 articles / 100 per page
	var allArticles []ElasticsearchHit

	for page := 0; page < totalPages; page++ {
		from := page * 100
		esURL := fmt.Sprintf("http://localhost:9200/talkdesk-docs/_search?size=100&from=%d", from)
		
		resp, err := http.Get(esURL)
		if err != nil {
			fmt.Printf("❌ Error fetching page %d from Elasticsearch: %v\n", page+1, err)
			continue
		}

		var esResponse ElasticsearchResponse
		if err := json.NewDecoder(resp.Body).Decode(&esResponse); err != nil {
			fmt.Printf("❌ Error decoding page %d: %v\n", page+1, err)
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		allArticles = append(allArticles, esResponse.Hits.Hits...)
		fmt.Printf("📄 Fetched page %d: %d articles (total so far: %d)\n", page+1, len(esResponse.Hits.Hits), len(allArticles))

		if len(esResponse.Hits.Hits) < 100 {
			break // Last page
		}
	}

	fmt.Printf("📚 Total articles to transfer: %d\n", len(allArticles))

	// Process articles in batches
	batchSize := 50
	var successCount int
	var errorCount int

	for i := 0; i < len(allArticles); i += batchSize {
		end := i + batchSize
		if end > len(allArticles) {
			end = len(allArticles)
		}

		batch := allArticles[i:end]
		fmt.Printf("📦 Processing batch %d-%d...\n", i+1, end)

		if err := processBatch(batch, azureConfig); err != nil {
			fmt.Printf("⚠ Error processing batch %d-%d: %v\n", i+1, end, err)
			errorCount++
		} else {
			successCount++
			fmt.Printf("✅ Batch %d-%d successfully indexed\n", i+1, end)
		}

		// Small delay between batches
		time.Sleep(500 * time.Millisecond)
	}

	fmt.Printf("\n=== Transfer Summary ===\n")
	fmt.Printf("Total articles: %d\n", len(allArticles))
	fmt.Printf("Successful batches: %d\n", successCount)
	fmt.Printf("Failed batches: %d\n", errorCount)
	fmt.Printf("🌐 Test search for 'polly' at: http://talkdesk-search-demo.eastus.azurecontainer.io:8080\n")
}

func processBatch(articles []ElasticsearchHit, config struct{ ServiceName, ApiKey, IndexName string }) error {
	var azureDocs []AzureDocument

	for _, hit := range articles {
		source := hit.Source

		// Convert Elasticsearch document to Azure format
		doc := AzureDocument{
			SearchAction: "mergeOrUpload",
			IndexedAt:    time.Now().UTC().Format(time.RFC3339),
		}

		// Map fields from Elasticsearch to Azure format
		if id, ok := source["id"]; ok {
			doc.ID = fmt.Sprintf("%v", id)
		}
		if title, ok := source["title"]; ok {
			doc.Title = fmt.Sprintf("%v", title)
		}
		if body, ok := source["body"]; ok {
			doc.Body = fmt.Sprintf("%v", body)
		}
		if url, ok := source["url"]; ok {
			doc.URL = fmt.Sprintf("%v", url)
		}
		if createdAt, ok := source["created_at"]; ok {
			doc.CreatedAt = fmt.Sprintf("%v", createdAt)
		}
		if updatedAt, ok := source["updated_at"]; ok {
			doc.UpdatedAt = fmt.Sprintf("%v", updatedAt)
		}

		// Ensure we have required fields
		if doc.ID == "" || doc.ID == "<nil>" {
			doc.ID = fmt.Sprintf("doc-%d", time.Now().UnixNano())
		}
		if doc.Title == "" {
			doc.Title = "Untitled"
		}

		azureDocs = append(azureDocs, doc)
	}

	// Create batch request
	batchRequest := AzureBatchRequest{
		Value: azureDocs,
	}

	// Convert to JSON
	jsonData, err := json.Marshal(batchRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal batch: %v", err)
	}

	// Send to Azure
	azureURL := fmt.Sprintf("https://%s.search.windows.net/indexes/%s/docs/index?api-version=2021-04-30-Preview",
		config.ServiceName, config.IndexName)
	
	req, err := http.NewRequest("POST", azureURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", config.ApiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send to Azure: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Azure returned status %d", resp.StatusCode)
	}

	return nil
}