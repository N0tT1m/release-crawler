package main

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
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
	Query string `json:"query"`
	From  int    `json:"from"`
	Size  int    `json:"size"`
}

type SearchResponse struct {
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

type SearchResult struct {
	Articles       []Article
	Total          int
	Query          string
	CurrentPage    int
	TotalPages     int
	HasPrev        bool
	HasNext        bool
	PrevPage       int
	NextPage       int
	ResultsPerPage int
	Suggestions    []string
	SearchTime     string
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
	Result    SearchResult
	Timestamp time.Time
}

var searchCache = &SearchCache{
	cache: make(map[string]CacheEntry),
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Documentation Search</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            line-height: 1.6;
            color: #333;
            background-color: #f5f5f5;
        }
        
        .container {
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
        }
        
        .header {
            background: white;
            padding: 30px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            margin-bottom: 30px;
            text-align: center;
        }
        
        .header h1 {
            color: #2c3e50;
            margin-bottom: 10px;
            font-size: 2.5rem;
        }
        
        .header p {
            color: #7f8c8d;
            font-size: 1.1rem;
        }
        
        .search-box {
            background: white;
            padding: 25px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            margin-bottom: 30px;
        }
        
        .search-form {
            display: flex;
            gap: 10px;
            align-items: center;
            position: relative;
        }
        
        .search-input {
            flex: 1;
            padding: 12px 16px;
            border: 2px solid #e0e0e0;
            border-radius: 6px;
            font-size: 16px;
            transition: border-color 0.3s;
        }
        
        .search-input:focus {
            outline: none;
            border-color: #3498db;
        }
        
        .autocomplete-container {
            position: relative;
            flex: 1;
        }
        
        .autocomplete-suggestions {
            position: absolute;
            top: 100%;
            left: 0;
            right: 0;
            background: white;
            border: 1px solid #e0e0e0;
            border-top: none;
            border-radius: 0 0 6px 6px;
            box-shadow: 0 4px 8px rgba(0,0,0,0.1);
            z-index: 1000;
            display: none;
        }
        
        .autocomplete-suggestions.show {
            display: block;
        }
        
        .autocomplete-suggestion {
            padding: 10px 16px;
            cursor: pointer;
            border-bottom: 1px solid #f0f0f0;
            transition: background-color 0.2s;
        }
        
        .autocomplete-suggestion:hover,
        .autocomplete-suggestion.selected {
            background-color: #f8f9fa;
        }
        
        .autocomplete-suggestion:last-child {
            border-bottom: none;
        }
        
        .search-stats {
            font-size: 12px;
            color: #7f8c8d;
            margin-left: 10px;
        }
        
        .search-button {
            background: #3498db;
            color: white;
            border: none;
            padding: 12px 24px;
            border-radius: 6px;
            font-size: 16px;
            cursor: pointer;
            transition: background-color 0.3s;
        }
        
        .search-button:hover {
            background: #2980b9;
        }
        
        .results-info {
            background: white;
            padding: 15px 25px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            margin-bottom: 20px;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        
        .results-count {
            color: #7f8c8d;
            font-size: 14px;
        }
        
        .article {
            background: white;
            padding: 25px;
            margin-bottom: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            transition: transform 0.2s, box-shadow 0.2s;
        }
        
        .article:hover {
            transform: translateY(-2px);
            box-shadow: 0 4px 8px rgba(0,0,0,0.15);
        }
        
        .article-title {
            font-size: 1.4rem;
            font-weight: 600;
            color: #2c3e50;
            margin-bottom: 12px;
            line-height: 1.3;
        }
        
        .article-title a {
            color: #3498db;
            text-decoration: none;
            transition: color 0.3s;
        }
        
        .article-title a:hover {
            color: #2980b9;
            text-decoration: underline;
        }
        
        .article-meta {
            display: flex;
            gap: 20px;
            margin-bottom: 15px;
            font-size: 14px;
            color: #7f8c8d;
        }
        
        .article-body {
            color: #555;
            line-height: 1.7;
            margin-bottom: 15px;
        }
        
        .article-body p {
            margin-bottom: 12px;
        }
        
        .article-body ul {
            margin-left: 20px;
            margin-bottom: 12px;
        }
        
        .pagination {
            display: flex;
            justify-content: center;
            align-items: center;
            gap: 10px;
            margin-top: 40px;
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        
        .pagination a, .pagination span {
            padding: 8px 16px;
            text-decoration: none;
            border-radius: 4px;
            transition: all 0.3s;
        }
        
        .pagination a {
            background: #f8f9fa;
            color: #3498db;
            border: 1px solid #e0e0e0;
        }
        
        .pagination a:hover {
            background: #3498db;
            color: white;
        }
        
        .pagination .current {
            background: #3498db;
            color: white;
            border: 1px solid #3498db;
        }
        
        .pagination .disabled {
            background: #f8f9fa;
            color: #bdc3c7;
            border: 1px solid #e0e0e0;
            cursor: not-allowed;
        }
        
        .no-results {
            background: white;
            padding: 40px;
            text-align: center;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            color: #7f8c8d;
        }
        
        .no-results h3 {
            margin-bottom: 10px;
            color: #95a5a6;
        }
        
        @media (max-width: 768px) {
            .container {
                padding: 10px;
            }
            
            .header h1 {
                font-size: 2rem;
            }
            
            .search-form {
                flex-direction: column;
            }
            
            .search-input, .search-button {
                width: 100%;
            }
            
            .results-info {
                flex-direction: column;
                gap: 10px;
                text-align: center;
            }
            
            .article-meta {
                flex-direction: column;
                gap: 5px;
            }
            
            .pagination {
                flex-wrap: wrap;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🔍 Documentation Search</h1>
            <p>Search through all product documentation and release notes</p>
        </div>
        
        <div class="search-box">
            <form class="search-form" method="GET" action="/" id="searchForm">
                <div class="autocomplete-container">
                    <input type="text" name="q" id="searchInput" class="search-input" placeholder="Search release notes... (try quotes for exact phrases)" value="{{.Query}}" autofocus autocomplete="off">
                    <div id="autocompleteSuggestions" class="autocomplete-suggestions"></div>
                </div>
                <button type="submit" class="search-button">Search</button>
                {{if .SearchTime}}<span class="search-stats">{{.SearchTime}}</span>{{end}}
            </form>
        </div>
        
        {{if .Query}}
            {{if .Articles}}
                <div class="results-info">
                    <div class="results-count">
                        Found {{.Total}} results for "{{.Query}}" • Page {{.CurrentPage}} of {{.TotalPages}}
                    </div>
                </div>
                
                {{range .Articles}}
                <div class="article">
                    <h2 class="article-title">
                        <a href="{{.HTMLURL}}" target="_blank">{{.Title}}</a>
                    </h2>
                    <div class="article-meta">
                        <span>📅 Created: {{.CreatedAt}}</span>
                        <span>🔄 Updated: {{.UpdatedAt}}</span>
                        <span>🆔 ID: {{.ID}}</span>
                    </div>
                    <div class="article-body">
                        {{.Body | truncateHTML}}
                    </div>
                </div>
                {{end}}
                
                {{if gt .TotalPages 1}}
                <div class="pagination">
                    {{if .HasPrev}}
                        <a href="?q={{.Query}}&page={{.PrevPage}}">&larr; Previous</a>
                    {{else}}
                        <span class="disabled">&larr; Previous</span>
                    {{end}}
                    
                    <span class="current">{{.CurrentPage}}</span>
                    <span>of {{.TotalPages}}</span>
                    
                    {{if .HasNext}}
                        <a href="?q={{.Query}}&page={{.NextPage}}">Next &rarr;</a>
                    {{else}}
                        <span class="disabled">Next &rarr;</span>
                    {{end}}
                </div>
                {{end}}
            {{else}}
                <div class="no-results">
                    <h3>No results found</h3>
                    <p>Try different keywords or check your spelling</p>
                    {{if .Suggestions}}
                        <div style="margin-top: 20px;">
                            <h4>Did you mean:</h4>
                            {{range .Suggestions}}
                                <div style="margin: 5px 0;">
                                    <a href="?q={{.}}" style="color: #3498db; text-decoration: none;">{{.}}</a>
                                </div>
                            {{end}}
                        </div>
                    {{end}}
                </div>
            {{end}}
        {{else}}
            <div class="no-results">
                <h3>Welcome to Documentation Search</h3>
                <p>Enter a search term above to find relevant release notes and documentation</p>
            </div>
        {{end}}
    </div>
    
    <script>
        document.addEventListener('DOMContentLoaded', function() {
            const searchInput = document.getElementById('searchInput');
            const suggestionsContainer = document.getElementById('autocompleteSuggestions');
            let selectedIndex = -1;
            let debounceTimer;
            
            searchInput.addEventListener('input', function() {
                const query = this.value.trim();
                
                clearTimeout(debounceTimer);
                debounceTimer = setTimeout(() => {
                    if (query.length >= 2) {
                        fetchSuggestions(query);
                    } else {
                        hideSuggestions();
                    }
                }, 300);
            });
            
            searchInput.addEventListener('keydown', function(e) {
                const suggestions = suggestionsContainer.querySelectorAll('.autocomplete-suggestion');
                
                if (e.key === 'ArrowDown') {
                    e.preventDefault();
                    selectedIndex = Math.min(selectedIndex + 1, suggestions.length - 1);
                    updateSelection(suggestions);
                } else if (e.key === 'ArrowUp') {
                    e.preventDefault();
                    selectedIndex = Math.max(selectedIndex - 1, -1);
                    updateSelection(suggestions);
                } else if (e.key === 'Enter') {
                    if (selectedIndex >= 0 && suggestions[selectedIndex]) {
                        e.preventDefault();
                        selectSuggestion(suggestions[selectedIndex].textContent);
                    }
                } else if (e.key === 'Escape') {
                    hideSuggestions();
                    selectedIndex = -1;
                }
            });
            
            // Hide suggestions when clicking outside
            document.addEventListener('click', function(e) {
                if (!searchInput.contains(e.target) && !suggestionsContainer.contains(e.target)) {
                    hideSuggestions();
                }
            });
            
            function fetchSuggestions(query) {
                fetch('/autocomplete?q=' + encodeURIComponent(query))
                    .then(response => response.json())
                    .then(data => {
                        displaySuggestions(data.suggestions || []);
                    })
                    .catch(err => {
                        console.error('Autocomplete error:', err);
                        hideSuggestions();
                    });
            }
            
            function displaySuggestions(suggestions) {
                if (suggestions.length === 0) {
                    hideSuggestions();
                    return;
                }
                
                suggestionsContainer.innerHTML = '';
                suggestions.forEach((suggestion, index) => {
                    const div = document.createElement('div');
                    div.className = 'autocomplete-suggestion';
                    div.textContent = suggestion;
                    div.addEventListener('click', () => selectSuggestion(suggestion));
                    suggestionsContainer.appendChild(div);
                });
                
                suggestionsContainer.classList.add('show');
                selectedIndex = -1;
            }
            
            function updateSelection(suggestions) {
                suggestions.forEach((suggestion, index) => {
                    suggestion.classList.toggle('selected', index === selectedIndex);
                });
            }
            
            function selectSuggestion(suggestion) {
                searchInput.value = suggestion;
                hideSuggestions();
                document.getElementById('searchForm').submit();
            }
            
            function hideSuggestions() {
                suggestionsContainer.classList.remove('show');
                selectedIndex = -1;
            }
            
            // Add search tips
            const tips = [
                'Use quotes for exact phrases: "new feature"',
                'Search works with typos and partial words',
                'Results are sorted by relevance and recency'
            ];
            
            let tipIndex = 0;
            setInterval(() => {
                if (!searchInput.value && document.activeElement !== searchInput) {
                    searchInput.placeholder = tips[tipIndex];
                    tipIndex = (tipIndex + 1) % tips.length;
                }
            }, 4000);
        });
    </script>
</body>
</html>`

func main() {
	http.HandleFunc("/", searchHandler)
	http.HandleFunc("/autocomplete", autocompleteHandler)
	http.HandleFunc("/health", healthHandler)
	
	port := getEnv("SERVER_PORT", "8080")
	bind := getEnv("SERVER_BIND", "0.0.0.0")
	
	fmt.Println("🌐 Starting Documentation Search Server...")
	fmt.Println("📍 Server running on:")
	fmt.Printf("   • Local: http://localhost:%s\n", port)
	fmt.Printf("   • Network: http://YOUR_IP:%s\n", port)
	fmt.Println("🔍 Access the search interface in your browser")
	fmt.Printf("📡 Health check: http://localhost:%s/health\n", port)
	fmt.Println("🛑 Press Ctrl+C to stop")
	
	log.Fatal(http.ListenAndServe(bind+":"+port, nil))
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	pageStr := r.URL.Query().Get("page")
	
	page := 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	
	resultsPerPage := 10
	from := (page - 1) * resultsPerPage
	
	result := SearchResult{
		Query:          query,
		CurrentPage:    page,
		ResultsPerPage: resultsPerPage,
	}
	
	if query != "" {
		articles, total, err := searchElasticsearch(query, from, resultsPerPage)
		if err != nil {
			http.Error(w, fmt.Sprintf("Search error: %v", err), http.StatusInternalServerError)
			return
		}
		
		result.Articles = articles
		result.Total = total
		result.TotalPages = (total + resultsPerPage - 1) / resultsPerPage
		result.HasPrev = page > 1
		result.HasNext = page < result.TotalPages
		result.PrevPage = page - 1
		result.NextPage = page + 1
	}
	
	tmpl := template.Must(template.New("search").Funcs(template.FuncMap{
		"truncateHTML": truncateHTML,
	}).Parse(htmlTemplate))
	
	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, result); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"service":   "talkdesk-search",
	})
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

	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, 0, err
	}

	var articles []Article
	for _, hit := range searchResp.Hits.Hits {
		articles = append(articles, hit.Source)
	}

	// Cache the result
	result := SearchResult{
		Articles: articles,
		Total:    searchResp.Hits.Total.Value,
	}
	cacheResult(cacheKey, result)

	return articles, searchResp.Hits.Total.Value, nil
}

func oldSearchElasticsearch(query string, from, size int) ([]Article, int, error) {
	esQuery := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"should": []map[string]interface{}{
					{
						"match": map[string]interface{}{
							"title": map[string]interface{}{
								"query": query,
								"boost": 3,
							},
						},
					},
					{
						"match": map[string]interface{}{
							"body": map[string]interface{}{
								"query": query,
								"boost": 1,
							},
						},
					},
				},
				"minimum_should_match": 1,
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
		},
	}
	
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
	
	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, 0, err
	}
	
	var articles []Article
	for _, hit := range searchResp.Hits.Hits {
		articles = append(articles, hit.Source)
	}
	
	return articles, searchResp.Hits.Total.Value, nil
}

func truncateHTML(text string) template.HTML {
	// Clean up HTML for better display
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	
	// Remove common structural HTML tags that create whitespace
	text = strings.ReplaceAll(text, "<div>", "")
	text = strings.ReplaceAll(text, "</div>", "\n")
	text = strings.ReplaceAll(text, "<section>", "")
	text = strings.ReplaceAll(text, "</section>", "\n")
	text = strings.ReplaceAll(text, "<nav>", "")
	text = strings.ReplaceAll(text, "</nav>", "")
	text = strings.ReplaceAll(text, "<header>", "")
	text = strings.ReplaceAll(text, "</header>", "\n")
	text = strings.ReplaceAll(text, "<footer>", "")
	text = strings.ReplaceAll(text, "</footer>", "")
	
	// Convert common HTML tags to readable format
	text = strings.ReplaceAll(text, "<p>", "")
	text = strings.ReplaceAll(text, "</p>", "\n\n")
	text = strings.ReplaceAll(text, "<br>", "\n")
	text = strings.ReplaceAll(text, "<br/>", "\n")
	text = strings.ReplaceAll(text, "<br />", "\n")
	
	// Handle headings
	text = strings.ReplaceAll(text, "<h1>", "")
	text = strings.ReplaceAll(text, "</h1>", "\n\n")
	text = strings.ReplaceAll(text, "<h2>", "")
	text = strings.ReplaceAll(text, "</h2>", "\n\n")
	text = strings.ReplaceAll(text, "<h3>", "")
	text = strings.ReplaceAll(text, "</h3>", "\n\n")
	
	// Handle lists
	text = strings.ReplaceAll(text, "<ul>", "")
	text = strings.ReplaceAll(text, "</ul>", "\n")
	text = strings.ReplaceAll(text, "<ol>", "")
	text = strings.ReplaceAll(text, "</ol>", "\n")
	text = strings.ReplaceAll(text, "<li>", "• ")
	text = strings.ReplaceAll(text, "</li>", "\n")
	
	// Handle emphasis
	text = strings.ReplaceAll(text, "<strong>", "**")
	text = strings.ReplaceAll(text, "</strong>", "**")
	text = strings.ReplaceAll(text, "<em>", "*")
	text = strings.ReplaceAll(text, "</em>", "*")
	
	// Remove remaining HTML tags with a simple regex approach
	// Remove style attributes
	text = removeStyleAttributes(text)
	
	// Remove remaining tags
	text = removeHTMLTags(text)
	
	// Aggressive whitespace cleanup
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
	
	text = strings.TrimSpace(text)
	
	// Truncate to reasonable length
	if len(text) > 800 {
		text = text[:800] + "..."
	}
	
	// Convert newlines to HTML breaks for display
	text = strings.ReplaceAll(text, "\n", "<br>")
	
	return template.HTML(text)
}

func removeStyleAttributes(text string) string {
	// Remove style attributes
	for {
		start := strings.Index(text, " style=\"")
		if start == -1 {
			break
		}
		end := strings.Index(text[start+8:], "\"")
		if end == -1 {
			break
		}
		text = text[:start] + text[start+8+end+1:]
	}
	return text
}

func removeHTMLTags(text string) string {
	// Simple HTML tag removal
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

func getCachedResult(key string) (SearchResult, bool) {
	searchCache.mu.RLock()
	defer searchCache.mu.RUnlock()
	
	entry, exists := searchCache.cache[key]
	if !exists {
		return SearchResult{}, false
	}
	
	// Cache expires after 5 minutes
	if time.Since(entry.Timestamp) > 5*time.Minute {
		delete(searchCache.cache, key)
		return SearchResult{}, false
	}
	
	return entry.Result, true
}

func cacheResult(key string, result SearchResult) {
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

func generateSearchSuggestions(query string) []string {
	suggestions := []string{}
	
	// Common typos and alternatives
	typoMap := map[string][]string{
		"teh":      {"the"},
		"adn":      {"and"},
		"accont":   {"account"},
		"suport":   {"support"},
		"contect":  {"contact", "connect"},
		"relase":   {"release"},
		"updat":    {"update"},
		"configur": {"configure", "configuration"},
	}
	
	words := strings.Fields(strings.ToLower(query))
	for _, word := range words {
		if alts, exists := typoMap[word]; exists {
			for _, alt := range alts {
				suggestions = append(suggestions, strings.Replace(query, word, alt, 1))
			}
		}
	}
	
	// Add common search patterns
	commonTerms := []string{"release notes", "update", "new features", "bug fixes", "improvements"}
	for _, term := range commonTerms {
		if strings.Contains(strings.ToLower(term), strings.ToLower(query)) {
			suggestions = append(suggestions, term)
		}
	}
	
	return suggestions
}

func autocompleteHandler(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) < 2 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AutocompleteResponse{Suggestions: []string{}})
		return
	}
	
	// Simple autocomplete using Elasticsearch completion suggester
	suggestions := getAutocompleteSuggestions(query)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AutocompleteResponse{Suggestions: suggestions})
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
		"size": 5,
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
	
	var searchResp SearchResponse
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