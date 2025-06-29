# Documentation Search API

A Gin-based REST API that provides search functionality for documentation with Slack integration.

## Features

✅ **REST API Endpoints**: Clean JSON API for search operations
✅ **Fast Search**: Full-text search across all documentation with smart ranking
✅ **Fuzzy Matching**: Handles typos and partial words
✅ **Phrase Search**: Use quotes for exact phrases
✅ **Real-time Autocomplete**: Intelligent suggestions API
✅ **Slack Integration**: Bot commands and direct messages
✅ **Caching**: In-memory caching for improved performance
✅ **CORS Support**: Cross-origin requests enabled

## API Endpoints

### Search Documentation
```
GET|POST /search?q=search+query&from=0&size=10
```

**Parameters:**
- `q` (required): Search query
- `from` (optional): Offset for pagination (default: 0)
- `size` (optional): Number of results per page (default: 10, max: 50)

**Response:**
```json
{
  "articles": [
    {
      "id": "article-id",
      "title": "Article Title",
      "body": "Article content...",
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z",
      "url": "https://example.com/article",
      "section_id": 123
    }
  ],
  "total": 42,
  "query": "search query",
  "current_page": 1,
  "total_pages": 5,
  "has_prev": false,
  "has_next": true,
  "prev_page": 0,
  "next_page": 2,
  "results_per_page": 10,
  "search_time": "15.23ms"
}
```

### Autocomplete Suggestions
```
GET /autocomplete?q=partial+query
```

**Response:**
```json
{
  "suggestions": [
    "Release Notes",
    "Release Management",
    "Release Process"
  ]
}
```

### Health Check
```
GET /health
```

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2024-01-01T00:00:00Z",
  "service": "documentation-search-api"
}
```

## Slack Integration

### Slash Commands
- `/search <query>` - Search documentation and display results in channel

### Bot Mentions
- `@SearchBot <query>` - Mention the bot in any channel with your search query

### Direct Messages
- Send a direct message to the bot with your search query

## Setup and Configuration

### Environment Variables

Create a `.env` file or set environment variables:

```bash
# API Server Configuration
API_PORT=8080
API_BIND=0.0.0.0

# Elasticsearch Configuration
ELASTICSEARCH_URL=http://localhost:9200
ELASTICSEARCH_INDEX=documentation-articles

# Slack Configuration (optional)
SLACK_BOT_TOKEN=xoxb-your-bot-token-here
SLACK_SIGNING_SECRET=your-signing-secret-here
SLACK_BOT_USER_ID=your-bot-user-id-here
```

### Running the Server

```bash
# Build the server
go build -o api-server api-server.go

# Run the server
./api-server
```

The API will be available at `http://localhost:8080`

### Docker Support

```bash
# Build Docker image
docker build -t doc-search-api .

# Run with Docker
docker run -p 8080:8080 \
  -e ELASTICSEARCH_URL=http://your-es-host:9200 \
  -e ELASTICSEARCH_INDEX=your-index \
  doc-search-api
```

## Slack Bot Setup

1. Create a new Slack app at https://api.slack.com/apps
2. Add Bot Token Scopes:
   - `app_mentions:read`
   - `chat:write`
   - `commands`
   - `im:read`
   - `im:write`
3. Install the app to your workspace
4. Set up Event Subscriptions:
   - Request URL: `https://your-domain.com/slack/events`
   - Subscribe to events: `app_mention`, `message.im`
5. Create Slash Command:
   - Command: `/search`
   - Request URL: `https://your-domain.com/slack/commands`
6. Copy the Bot Token and add to environment variables

## Search Features

### Smart Ranking
- Title matches ranked higher than body matches
- Exact phrase matches get highest priority
- Recent articles get slight boost in ranking

### Query Types
- **Simple search**: `release notes`
- **Phrase search**: `"new feature"`
- **Fuzzy search**: Automatically handles typos
- **Prefix search**: Matches partial words

### Performance
- In-memory caching with 5-minute TTL
- Connection pooling for Elasticsearch
- Configurable timeouts and limits

## API Usage Examples

### cURL Examples

```bash
# Basic search
curl "http://localhost:8080/search?q=release+notes"

# Search with pagination
curl "http://localhost:8080/search?q=bug+fixes&from=10&size=5"

# Autocomplete
curl "http://localhost:8080/autocomplete?q=rel"

# Health check
curl "http://localhost:8080/health"
```

### JavaScript/Fetch Example

```javascript
// Search documentation
const searchResults = await fetch('/search?q=api+documentation')
  .then(res => res.json());

// Get autocomplete suggestions
const suggestions = await fetch('/autocomplete?q=user')
  .then(res => res.json());
```

## Production Deployment

### Performance Tuning
- Set `GIN_MODE=release` environment variable
- Configure appropriate `API_BIND` and `API_PORT`
- Set up load balancing if needed
- Monitor Elasticsearch performance

### Security
- Use HTTPS in production
- Implement rate limiting if needed
- Secure Slack webhook endpoints
- Validate all input parameters

### Monitoring
- Monitor `/health` endpoint
- Track search response times
- Monitor Elasticsearch cluster health
- Set up logging and metrics collection