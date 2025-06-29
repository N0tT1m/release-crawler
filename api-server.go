package main

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

type Article struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	HTMLURL   string `json:"url"`
	SectionID int64  `json:"section_id"`
}

type SearchRequest struct {
	Query string `json:"query" form:"q" binding:"required"`
	From  int    `json:"from" form:"from"`
	Size  int    `json:"size" form:"size"`
}

type SearchAPIResponse struct {
	Articles       []Article `json:"articles"`
	Total          int       `json:"total"`
	Query          string    `json:"query"`
	CurrentPage    int       `json:"current_page"`
	TotalPages     int       `json:"total_pages"`
	HasPrev        bool      `json:"has_prev"`
	HasNext        bool      `json:"has_next"`
	PrevPage       int       `json:"prev_page"`
	NextPage       int       `json:"next_page"`
	ResultsPerPage int       `json:"results_per_page"`
	SearchTime     string    `json:"search_time"`
}

type ElasticsearchResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []struct {
			Source Article `json:"_source"`
			Score  float64 `json:"_score"`
		} `json:"hits"`
	} `json:"hits"`
}

type AutocompleteResponse struct {
	Suggestions []string `json:"suggestions"`
}

// Simple in-memory cache
type SearchCache struct {
	mu    sync.RWMutex
	cache map[string]CacheEntry
}

type CacheEntry struct {
	Result    SearchAPIResponse
	Timestamp time.Time
}

var searchCache = &SearchCache{
	cache: make(map[string]CacheEntry),
}

func main() {
	// Set Gin to release mode for production
	gin.SetMode(gin.ReleaseMode)
	
	r := gin.Default()

	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		
		c.Next()
	})

	// Health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"service":   "documentation-search-api",
		})
	})

	// Search endpoint
	r.GET("/search", searchHandler)
	r.POST("/search", searchHandler)

	// Autocomplete endpoint
	r.GET("/autocomplete", autocompleteHandler)

	// Slack endpoints
	r.POST("/slack/events", slackEventsHandler)
	r.POST("/slack/commands", slackCommandHandler)

	port := getEnv("API_PORT", "8080")
	bind := getEnv("API_BIND", "0.0.0.0")

	fmt.Println("🚀 Starting Documentation Search API...")
	fmt.Println("📍 Server running on:")
	fmt.Printf("   • Local: http://localhost:%s\n", port)
	fmt.Printf("   • Network: http://YOUR_IP:%s\n", port)
	fmt.Println("🔍 API Endpoints:")
	fmt.Printf("   • GET/POST /search - Search documentation\n")
	fmt.Printf("   • GET /autocomplete - Get search suggestions\n")
	fmt.Printf("   • POST /slack/events - Slack events webhook\n")
	fmt.Printf("   • POST /slack/commands - Slack commands webhook\n")
	fmt.Printf("   • GET /health - Health check\n")
	fmt.Println("🛑 Press Ctrl+C to stop")

	log.Fatal(r.Run(bind + ":" + port))
}

func searchHandler(c *gin.Context) {
	startTime := time.Now()
	
	var req SearchRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request parameters", "details": err.Error()})
		return
	}

	if req.Query == "" {
		c.JSON(400, gin.H{"error": "Query parameter is required"})
		return
	}

	// Set defaults
	if req.Size <= 0 || req.Size > 50 {
		req.Size = 10
	}
	if req.From < 0 {
		req.From = 0
	}

	page := (req.From / req.Size) + 1
	if page <= 0 {
		page = 1
	}

	articles, total, err := searchElasticsearch(req.Query, req.From, req.Size)
	if err != nil {
		c.JSON(500, gin.H{"error": "Search failed", "details": err.Error()})
		return
	}

	totalPages := (total + req.Size - 1) / req.Size
	if totalPages == 0 {
		totalPages = 1
	}

	response := SearchAPIResponse{
		Articles:       articles,
		Total:          total,
		Query:          req.Query,
		CurrentPage:    page,
		TotalPages:     totalPages,
		HasPrev:        page > 1,
		HasNext:        page < totalPages,
		PrevPage:       page - 1,
		NextPage:       page + 1,
		ResultsPerPage: req.Size,
		SearchTime:     fmt.Sprintf("%.2fms", float64(time.Since(startTime).Nanoseconds())/1000000),
	}

	c.JSON(200, response)
}

func autocompleteHandler(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if len(query) < 2 {
		c.JSON(200, AutocompleteResponse{Suggestions: []string{}})
		return
	}

	suggestions := getAutocompleteSuggestions(query)
	c.JSON(200, AutocompleteResponse{Suggestions: suggestions})
}

func slackEventsHandler(c *gin.Context) {
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "Failed to read request body"})
		return
	}

	eventsAPIEvent, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		c.JSON(400, gin.H{"error": "Failed to parse event"})
		return
	}

	switch eventsAPIEvent.Type {
	case slackevents.URLVerification:
		var r *slackevents.ChallengeResponse
		err := json.Unmarshal(body, &r)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to unmarshal challenge"})
			return
		}
		c.JSON(200, gin.H{"challenge": r.Challenge})

	case slackevents.CallbackEvent:
		innerEvent := eventsAPIEvent.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			handleAppMention(ev)
		case *slackevents.MessageEvent:
			if ev.BotID == "" { // Ignore bot messages
				handleDirectMessage(ev)
			}
		}
		c.JSON(200, gin.H{"status": "ok"})

	default:
		c.JSON(400, gin.H{"error": "Unknown event type"})
	}
}

func slackCommandHandler(c *gin.Context) {
	command := c.PostForm("command")
	text := c.PostForm("text")
	userID := c.PostForm("user_id")
	channelID := c.PostForm("channel_id")
	responseURL := c.PostForm("response_url")

	if command == "/search" {
		handleSearchCommand(text, userID, channelID, responseURL)
		c.JSON(200, gin.H{
			"response_type": "in_channel",
			"text":         "🔍 Searching documentation...",
		})
	} else {
		c.JSON(400, gin.H{"error": "Unknown command"})
	}
}

func handleAppMention(event *slackevents.AppMentionEvent) {
	query := strings.TrimSpace(strings.Replace(event.Text, fmt.Sprintf("<@%s>", getBotUserID()), "", 1))
	if query == "" {
		sendSlackMessage(event.Channel, "👋 Hi! Ask me to search documentation by mentioning me with your search query. Example: `@SearchBot new release features`")
		return
	}

	performSlackSearch(query, event.Channel, event.User)
}

func handleDirectMessage(event *slackevents.MessageEvent) {
	if event.ChannelType == "im" {
		query := strings.TrimSpace(event.Text)
		if query == "" {
			sendSlackMessage(event.Channel, "👋 Hi! Send me a search query to find documentation. Example: `new release features`")
			return
		}
		
		performSlackSearch(query, event.Channel, event.User)
	}
}

func handleSearchCommand(query, userID, channelID, responseURL string) {
	if query == "" {
		sendSlackResponse(responseURL, "❌ Please provide a search query. Example: `/search new release features`")
		return
	}

	go performSlackSearch(query, channelID, userID)
}

func performSlackSearch(query, channelID, userID string) {
	articles, total, err := searchElasticsearch(query, 0, 5) // Limit to 5 results for Slack
	if err != nil {
		sendSlackMessage(channelID, fmt.Sprintf("❌ Search failed: %v", err))
		return
	}

	if total == 0 {
		sendSlackMessage(channelID, fmt.Sprintf("🔍 No results found for \"%s\"", query))
		return
	}

	// Build Slack message with results
	attachments := []slack.Attachment{}
	
	headerText := fmt.Sprintf("🔍 Found %d results for \"%s\"", total, query)
	if total > 5 {
		headerText += " (showing top 5)"
	}

	for i, article := range articles {
		color := "good"
		if i == 0 {
			color = "#3498db"
		}

		// Truncate body for Slack display
		body := cleanTextForSlack(article.Body)
		if len(body) > 300 {
			body = body[:300] + "..."
		}

		attachment := slack.Attachment{
			Color:     color,
			Title:     article.Title,
			TitleLink: article.HTMLURL,
			Text:      body,
			Fields: []slack.AttachmentField{
				{
					Title: "Created",
					Value: article.CreatedAt,
					Short: true,
				},
				{
					Title: "Updated",
					Value: article.UpdatedAt,
					Short: true,
				},
			},
			Footer: fmt.Sprintf("Article ID: %s", article.ID),
		}
		attachments = append(attachments, attachment)
	}

	api := slack.New(getEnv("SLACK_BOT_TOKEN", ""))
	_, _, err = api.PostMessage(channelID, 
		slack.MsgOptionText(headerText, false),
		slack.MsgOptionAttachments(attachments...),
	)
	
	if err != nil {
		log.Printf("Failed to send Slack message: %v", err)
	}
}

func sendSlackMessage(channelID, text string) {
	api := slack.New(getEnv("SLACK_BOT_TOKEN", ""))
	_, _, err := api.PostMessage(channelID, slack.MsgOptionText(text, false))
	if err != nil {
		log.Printf("Failed to send Slack message: %v", err)
	}
}

func sendSlackResponse(responseURL, text string) {
	payload := map[string]interface{}{
		"text":          text,
		"response_type": "ephemeral",
	}
	
	jsonData, _ := json.Marshal(payload)
	http.Post(responseURL, "application/json", bytes.NewBuffer(jsonData))
}

func getBotUserID() string {
	return getEnv("SLACK_BOT_USER_ID", "")
}

func cleanTextForSlack(text string) string {
	// Remove HTML tags and clean up text for Slack display
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	
	// Remove HTML tags
	text = removeHTMLTags(text)
	
	// Clean up whitespace
	lines := strings.Split(text, "\n")
	var cleanLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}
	text = strings.Join(cleanLines, "\n")
	
	// Remove excessive newlines
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	
	return strings.TrimSpace(text)
}

func removeHTMLTags(text string) string {
	result := ""
	inTag := false
	
	for _, char := range text {
		if char == '<' {
			inTag = true
		} else if char == '>' {
			inTag = false
		} else if !inTag {
			result += string(char)
		}
	}
	
	return result
}

func searchElasticsearch(query string, from, size int) ([]Article, int, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s-%d-%d", query, from, size))))
	if cachedResult, found := getCachedResult(cacheKey); found {
		return cachedResult.Articles, cachedResult.Total, nil
	}

	// Build enhanced query with fuzzy search and phrase detection
	esQuery := buildEnhancedQuery(query, from, size)

	jsonData, err := json.Marshal(esQuery)
	if err != nil {
		return nil, 0, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	esURL := getEnv("ELASTICSEARCH_URL", "http://localhost:9200")
	indexName := getEnv("ELASTICSEARCH_INDEX", "documentation-articles")
	resp, err := client.Post(fmt.Sprintf("%s/%s/_search", esURL, indexName), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var searchResp ElasticsearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, 0, err
	}

	var articles []Article
	for _, hit := range searchResp.Hits.Hits {
		articles = append(articles, hit.Source)
	}

	// Cache the result
	result := SearchAPIResponse{
		Articles: articles,
		Total:    searchResp.Hits.Total.Value,
	}
	cacheResult(cacheKey, result)

	return articles, searchResp.Hits.Total.Value, nil
}

func buildEnhancedQuery(query string, from, size int) map[string]interface{} {
	// Detect if it's a phrase search (quoted)
	isPhrase := strings.HasPrefix(query, "\"") && strings.HasSuffix(query, "\"")
	if isPhrase {
		query = strings.Trim(query, "\"")
		return buildPhraseQuery(query, from, size)
	}

	// Use function scoring with recency boost
	return map[string]interface{}{
		"query": map[string]interface{}{
			"function_score": map[string]interface{}{
				"query": map[string]interface{}{
					"bool": map[string]interface{}{
						"should": []map[string]interface{}{
							// Exact match (highest boost)
							{
								"multi_match": map[string]interface{}{
									"query":  query,
									"fields": []string{"title^5", "body^2"},
									"type":   "phrase",
									"boost":  10,
								},
							},
							// Fuzzy match for typos
							{
								"multi_match": map[string]interface{}{
									"query":     query,
									"fields":    []string{"title^3", "body^1"},
									"fuzziness": "AUTO",
									"boost":     5,
								},
							},
							// Prefix match for partial typing
							{
								"multi_match": map[string]interface{}{
									"query":  query,
									"fields": []string{"title^2", "body^1"},
									"type":   "phrase_prefix",
									"boost":  3,
								},
							},
						},
						"minimum_should_match": 1,
					},
				},
				"boost_mode": "multiply",
				"functions": []map[string]interface{}{
					{
						"gauss": map[string]interface{}{
							"updated_at": map[string]interface{}{
								"scale": "30d",
								"decay": 0.5,
							},
						},
						"weight": 1.2,
					},
				},
			},
		},
		"from": from,
		"size": size,
		"sort": []map[string]interface{}{
			{"_score": map[string]string{"order": "desc"}},
		},
		"highlight": map[string]interface{}{
			"fields": map[string]interface{}{
				"title": map[string]interface{}{"fragment_size": 150},
				"body":  map[string]interface{}{"fragment_size": 300},
			},
			"pre_tags":  []string{"<mark>"},
			"post_tags": []string{"</mark>"},
		},
	}
}

func buildPhraseQuery(query string, from, size int) map[string]interface{} {
	return map[string]interface{}{
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":  query,
				"fields": []string{"title^3", "body^1"},
				"type":   "phrase",
			},
		},
		"from": from,
		"size": size,
		"sort": []map[string]interface{}{
			{"_score": map[string]string{"order": "desc"}},
			{"updated_at": map[string]string{"order": "desc"}},
		},
		"highlight": map[string]interface{}{
			"fields": map[string]interface{}{
				"title": map[string]interface{}{},
				"body":  map[string]interface{}{},
			},
			"pre_tags":  []string{"<mark>"},
			"post_tags": []string{"</mark>"},
		},
	}
}

func getCachedResult(key string) (SearchAPIResponse, bool) {
	searchCache.mu.RLock()
	defer searchCache.mu.RUnlock()
	
	entry, exists := searchCache.cache[key]
	if !exists {
		return SearchAPIResponse{}, false
	}
	
	// Cache expires after 5 minutes
	if time.Since(entry.Timestamp) > 5*time.Minute {
		delete(searchCache.cache, key)
		return SearchAPIResponse{}, false
	}
	
	return entry.Result, true
}

func cacheResult(key string, result SearchAPIResponse) {
	searchCache.mu.Lock()
	defer searchCache.mu.Unlock()
	
	// Clean old entries periodically
	if len(searchCache.cache) > 1000 {
		for k, v := range searchCache.cache {
			if time.Since(v.Timestamp) > 5*time.Minute {
				delete(searchCache.cache, k)
			}
		}
	}
	
	searchCache.cache[key] = CacheEntry{
		Result:    result,
		Timestamp: time.Now(),
	}
}

func getAutocompleteSuggestions(query string) []string {
	// Build a simple prefix query to get suggestions
	esQuery := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"should": []map[string]interface{}{
					{
						"prefix": map[string]interface{}{
							"title": strings.ToLower(query),
						},
					},
					{
						"prefix": map[string]interface{}{
							"body": strings.ToLower(query),
						},
					},
				},
			},
		},
		"size":    5,
		"_source": []string{"title"},
	}

	jsonData, err := json.Marshal(esQuery)
	if err != nil {
		return []string{}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	esURL := getEnv("ELASTICSEARCH_URL", "http://localhost:9200")
	indexName := getEnv("ELASTICSEARCH_INDEX", "documentation-articles")
	resp, err := client.Post(fmt.Sprintf("%s/%s/_search", esURL, indexName), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return []string{}
	}
	defer resp.Body.Close()

	var searchResp ElasticsearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return []string{}
	}

	var suggestions []string
	for _, hit := range searchResp.Hits.Hits {
		title := hit.Source.Title
		if strings.Contains(strings.ToLower(title), strings.ToLower(query)) {
			suggestions = append(suggestions, title)
		}
	}

	return suggestions
}

// Helper functions for environment variables
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}