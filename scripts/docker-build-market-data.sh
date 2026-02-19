#!/usr/bin/env bash
# Build the market-data Docker image using plain 'docker build'.
# Use this when 'docker compose build market-data' fails with:
#   "compose build requires buildx 0.17.0 or later"
# Then run: docker-compose up -d market-data

set -e
cd "$(dirname "$0")/.."
docker build -f services/market-data/Dockerfile -t microservices-trading-bot-market-data .
echo "Image built. Start with: docker-compose up -d market-data"
