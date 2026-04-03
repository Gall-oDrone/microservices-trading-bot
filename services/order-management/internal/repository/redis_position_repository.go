package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
)

const positionKeyPrefix = "om:position:"

// RedisPositionRepository implements PositionRepository using Redis.
// Positions are keyed by book (e.g., "btc_mxn") for quick lookups.
type RedisPositionRepository struct {
	client  *redis.Client
	logger  *logger.Logger
	metrics *metrics.MetricsCollector
}

// NewRedisPositionRepository creates a new Redis position repository.
func NewRedisPositionRepository(client *redis.Client, logger *logger.Logger, metrics *metrics.MetricsCollector) *RedisPositionRepository {
	return &RedisPositionRepository{
		client:  client,
		logger:  logger,
		metrics: metrics,
	}
}

func positionKey(book string) string { return positionKeyPrefix + book }
func positionBooksKey() string       { return positionKeyPrefix + "books" }

// Create creates a new position.
func (r *RedisPositionRepository) Create(ctx context.Context, position *models.Position) error {
	if position == nil {
		return fmt.Errorf("position cannot be nil")
	}
	if err := position.Validate(); err != nil {
		return fmt.Errorf("invalid position: %w", err)
	}

	exists, err := r.client.Exists(ctx, positionKey(position.Book)).Result()
	if err != nil {
		return fmt.Errorf("redis check exists: %w", err)
	}
	if exists > 0 {
		return fmt.Errorf("position already exists for book: %s", position.Book)
	}

	data, err := json.Marshal(position)
	if err != nil {
		return fmt.Errorf("marshal position: %w", err)
	}

	pipe := r.client.Pipeline()
	pipe.Set(ctx, positionKey(position.Book), data, 0)
	pipe.SAdd(ctx, positionBooksKey(), position.Book)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis create position: %w", err)
	}

	r.logger.Debug("Position created", map[string]interface{}{
		"book": position.Book,
		"side": position.Side,
		"size": position.Size,
	})
	return nil
}

// Update updates an existing position.
func (r *RedisPositionRepository) Update(ctx context.Context, position *models.Position) error {
	if position == nil {
		return fmt.Errorf("position cannot be nil")
	}
	if err := position.Validate(); err != nil {
		return fmt.Errorf("invalid position: %w", err)
	}

	exists, err := r.client.Exists(ctx, positionKey(position.Book)).Result()
	if err != nil {
		return fmt.Errorf("redis check exists: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("position not found for book: %s", position.Book)
	}

	data, err := json.Marshal(position)
	if err != nil {
		return fmt.Errorf("marshal position: %w", err)
	}

	err = r.client.Set(ctx, positionKey(position.Book), data, 0).Err()
	if err != nil {
		return fmt.Errorf("redis update position: %w", err)
	}

	r.logger.Debug("Position updated", map[string]interface{}{
		"book":         position.Book,
		"side":         position.Side,
		"size":         position.Size,
		"realized_pnl": position.RealizedPnL,
	})
	return nil
}

// Get retrieves a position by book.
func (r *RedisPositionRepository) Get(ctx context.Context, book string) (*models.Position, error) {
	data, err := r.client.Get(ctx, positionKey(book)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("position not found for book: %s", book)
		}
		return nil, fmt.Errorf("redis get position: %w", err)
	}

	var position models.Position
	if err := json.Unmarshal(data, &position); err != nil {
		return nil, fmt.Errorf("unmarshal position: %w", err)
	}

	if position.Metadata == nil {
		position.Metadata = make(map[string]interface{})
	}

	return position.Clone(), nil
}

// GetAll retrieves all positions.
func (r *RedisPositionRepository) GetAll(ctx context.Context) ([]*models.Position, error) {
	books, err := r.client.SMembers(ctx, positionBooksKey()).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers: %w", err)
	}

	positions := make([]*models.Position, 0, len(books))
	for _, book := range books {
		pos, err := r.Get(ctx, book)
		if err != nil {
			continue
		}
		positions = append(positions, pos)
	}

	return positions, nil
}

// List retrieves positions with optional filters.
func (r *RedisPositionRepository) List(ctx context.Context, filters *models.PositionFilters) ([]*models.Position, error) {
	if filters == nil {
		filters = models.NewPositionFilters()
	}
	if err := filters.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filters: %w", err)
	}

	positions, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	return filters.Apply(positions), nil
}

// Delete deletes a position.
func (r *RedisPositionRepository) Delete(ctx context.Context, book string) error {
	exists, err := r.client.Exists(ctx, positionKey(book)).Result()
	if err != nil {
		return fmt.Errorf("redis check exists: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("position not found for book: %s", book)
	}

	pipe := r.client.Pipeline()
	pipe.Del(ctx, positionKey(book))
	pipe.SRem(ctx, positionBooksKey(), book)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis delete position: %w", err)
	}

	r.logger.Debug("Position deleted", map[string]interface{}{"book": book})
	return nil
}

// GetOpenPositions retrieves all open positions.
func (r *RedisPositionRepository) GetOpenPositions(ctx context.Context) ([]*models.Position, error) {
	positions, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	openPositions := make([]*models.Position, 0)
	for _, pos := range positions {
		if pos.IsOpen() {
			openPositions = append(openPositions, pos)
		}
	}

	return openPositions, nil
}

// GetSummary retrieves a summary of all positions.
func (r *RedisPositionRepository) GetSummary(ctx context.Context) (*models.PositionSummary, error) {
	positions, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	return models.NewPositionSummary(positions), nil
}

// Exists checks if a position exists.
func (r *RedisPositionRepository) Exists(ctx context.Context, book string) (bool, error) {
	n, err := r.client.Exists(ctx, positionKey(book)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Close closes the repository and releases resources.
func (r *RedisPositionRepository) Close() error {
	r.logger.Info("Position repository closed", nil)
	return nil
}

// Ping verifies Redis connectivity (for health checks).
func (r *RedisPositionRepository) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return r.client.Ping(ctx).Err()
}
