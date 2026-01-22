package service

import (
	"context"
	"log"
	"sync"
)

// ServiceConfig contains service configuration
type ServiceConfig struct {
	Name        string
	Version     string
	Host        string
	Port        int
	HealthCheck string
	Metadata    map[string]string
	Tags        []string
}

// Registry is an interface for service discovery registries
type Registry interface {
	Register(ctx context.Context, service *Service) error
	Deregister(ctx context.Context, service *Service) error
}

// Service represents a microservice
type Service struct {
	config   *ServiceConfig
	registry Registry
	logger   *log.Logger
	running  bool
	mu       sync.RWMutex
}

// NewService creates a new service instance
func NewService(config *ServiceConfig, registry Registry, logger *log.Logger) *Service {
	return &Service{
		config:   config,
		registry: registry,
		logger:   logger,
		running:  false,
	}
}

// Start starts the service and registers with the registry
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	if s.registry != nil {
		if err := s.registry.Register(ctx, s); err != nil {
			if s.logger != nil {
				s.logger.Printf("Failed to register service: %v", err)
			}
			return err
		}
	}

	s.running = true
	if s.logger != nil {
		s.logger.Printf("Service %s started on %s:%d", s.config.Name, s.config.Host, s.config.Port)
	}
	return nil
}

// Stop stops the service and deregisters from the registry
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	if s.registry != nil {
		if err := s.registry.Deregister(ctx, s); err != nil {
			if s.logger != nil {
				s.logger.Printf("Failed to deregister service: %v", err)
			}
			return err
		}
	}

	s.running = false
	if s.logger != nil {
		s.logger.Printf("Service %s stopped", s.config.Name)
	}
	return nil
}

// IsRunning returns whether the service is running
func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Config returns the service configuration
func (s *Service) Config() *ServiceConfig {
	return s.config
}

// Name returns the service name
func (s *Service) Name() string {
	return s.config.Name
}

// Version returns the service version
func (s *Service) Version() string {
	return s.config.Version
}

// Host returns the service host
func (s *Service) Host() string {
	return s.config.Host
}

// Port returns the service port
func (s *Service) Port() int {
	return s.config.Port
}

// HealthCheckPath returns the health check endpoint path
func (s *Service) HealthCheckPath() string {
	return s.config.HealthCheck
}

// Metadata returns the service metadata
func (s *Service) Metadata() map[string]string {
	return s.config.Metadata
}

// Tags returns the service tags
func (s *Service) Tags() []string {
	return s.config.Tags
}
