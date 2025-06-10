# Use the latest Go image
FROM golang:latest

# Set working directory
WORKDIR /app

# Install Air for hot reloading
RUN go install github.com/air-verse/air@latest

# Initialize go.mod with the correct module name
RUN go mod init github.com/bitso_trading_bot/golang_server/staging

# Add all necessary dependencies
RUN go get github.com/redis/go-redis/v9 && \
    go get github.com/joho/godotenv && \
    go get github.com/shopspring/decimal && \
    go get github.com/gorilla/websocket && \
    go get github.com/xiam/bitso-go && \
    go mod tidy

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Create a non-root user
RUN useradd -m -u 1000 appuser
RUN chown -R appuser:appuser /app
USER appuser

# Expose port if needed (adjust as necessary)
EXPOSE 8080

# Healthcheck
HEALTHCHECK --interval=30s --timeout=3s \
    CMD curl -f http://localhost:8080/health || exit 1

# Command to run the application with Air
CMD ["air"] 