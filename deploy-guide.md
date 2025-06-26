# Azure Deployment Guide - Cheapest Possible

## 🎯 **Total Monthly Cost: ~$1-5**

### **Option 1: Azure Container Instances (Recommended)**
- **Cost**: ~$1-3/month
- **Setup**: 5 minutes
- **Perfect for**: 3 users testing

### **Option 2: Azure App Service Free Tier**
- **Cost**: $0
- **Limitation**: App sleeps after 20 minutes of inactivity
- **Good for**: Intermittent testing

---

## 🚀 **Quick Setup (Container Instances)**

### **1. Build and Push Docker Image**
```bash
# Build the image
docker build -t talkdesk-search .

# Tag for Azure Container Registry (if using)
docker tag talkdesk-search your-registry.azurecr.io/talkdesk-search:latest

# Push to registry
docker push your-registry.azurecr.io/talkdesk-search:latest
```

### **2. Deploy to Azure**
```bash
# Login to Azure
az login

# Create resource group
az group create --name talkdesk-rg --location eastus

# Deploy container
az container create \
  --resource-group talkdesk-rg \
  --name talkdesk-search \
  --image your-registry.azurecr.io/talkdesk-search:latest \
  --cpu 0.1 \
  --memory 0.5 \
  --restart-policy Always \
  --ports 8080 \
  --dns-name-label talkdesk-search-unique \
  --environment-variables \
    AZURE_SEARCH_SERVICE=your-search-service \
    AZURE_SEARCH_INDEX=talkdesk-docs \
  --secure-environment-variables \
    AZURE_SEARCH_KEY=your-api-key
```

### **3. Access Your Application**
```
URL: http://talkdesk-search-unique.eastus.azurecontainer.io:8080
```

---

## 💰 **FREE Option: Azure App Service**

### **1. Create App Service (Free Tier)**
```bash
# Create App Service plan (Free tier)
az appservice plan create \
  --name talkdesk-plan \
  --resource-group talkdesk-rg \
  --sku F1 \
  --is-linux

# Create web app
az webapp create \
  --resource-group talkdesk-rg \
  --plan talkdesk-plan \
  --name talkdesk-search-app \
  --deployment-container-image-name your-registry/talkdesk-search:latest
```

### **2. Configure Environment Variables**
```bash
az webapp config appsettings set \
  --resource-group talkdesk-rg \
  --name talkdesk-search-app \
  --settings \
    AZURE_SEARCH_SERVICE=your-search-service \
    AZURE_SEARCH_KEY=your-api-key \
    AZURE_SEARCH_INDEX=talkdesk-docs
```

---

## 📋 **Setup Azure Cognitive Search**

### **1. Create Search Service (Free Tier)**
```bash
az search service create \
  --name your-search-service \
  --resource-group talkdesk-rg \
  --sku free \
  --location eastus
```

### **2. Get API Key**
```bash
az search admin-key show \
  --service-name your-search-service \
  --resource-group talkdesk-rg
```

### **3. Run Crawler to Index Data**
```bash
# Set environment variables
export AZURE_SEARCH_SERVICE=your-search-service
export AZURE_SEARCH_KEY=your-api-key
export AZURE_SEARCH_INDEX=talkdesk-docs

# Build and run crawler
go build -o azure-crawler azure-crawler.go
./azure-crawler
```

---

## 🔧 **Environment Variables Needed**

```bash
AZURE_SEARCH_SERVICE=your-search-service-name
AZURE_SEARCH_KEY=your-admin-api-key
AZURE_SEARCH_INDEX=talkdesk-docs
PORT=8080
```

---

## 📊 **Cost Breakdown**

### **Container Instances**
- CPU: 0.1 vCPU × $0.0000012/second × 2.6M seconds/month = ~$3.12
- Memory: 0.5GB × $0.0000001875/second × 2.6M seconds/month = ~$0.49
- **Total**: ~$3.61/month

### **Azure Cognitive Search (Free Tier)**
- **Cost**: $0
- **Limitations**: 50MB storage, 10,000 documents
- **Perfect for**: Testing with 47 articles

### **App Service (Free Tier)**
- **Cost**: $0
- **Limitations**: Sleeps after 20 minutes inactivity
- **Good for**: Demo/testing only

---

## 🚀 **One-Click Deploy Commands**

```bash
# Complete setup in one go
az group create --name talkdesk-rg --location eastus

# Create search service
az search service create \
  --name talkdesk-search-svc \
  --resource-group talkdesk-rg \
  --sku free

# Get API key
SEARCH_KEY=$(az search admin-key show \
  --service-name talkdesk-search-svc \
  --resource-group talkdesk-rg \
  --query primaryKey -o tsv)

# Deploy container
az container create \
  --resource-group talkdesk-rg \
  --name talkdesk-search \
  --image ghcr.io/your-username/talkdesk-search:latest \
  --cpu 0.1 \
  --memory 0.5 \
  --restart-policy Always \
  --ports 8080 \
  --dns-name-label talkdesk-search-demo \
  --environment-variables \
    AZURE_SEARCH_SERVICE=talkdesk-search-svc \
    AZURE_SEARCH_INDEX=talkdesk-docs \
  --secure-environment-variables \
    AZURE_SEARCH_KEY=$SEARCH_KEY

echo "🎉 Deployed! Access at: http://talkdesk-search-demo.eastus.azurecontainer.io:8080"
```

## 📝 **Next Steps**

1. **Run the crawler** to index your 47 articles
2. **Test the search** with your 3 users
3. **Monitor costs** in Azure portal
4. **Scale up** if needed (upgrade to Standard search tier)

The free/cheap tier should handle 3 users testing easily!