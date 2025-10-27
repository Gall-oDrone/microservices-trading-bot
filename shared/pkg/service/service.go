package service

import (
	"context"
	"fmt"
	"log"
	"time"
)

// ServiceInfo represents service information
type ServiceInfo struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Host        string            `json:"host"`
	Port        int               `json:"port"`
	HealthCheck string            `json:"health_check"`
	Metadata    map[string]string `json:"metadata"`
	Tags        []string          `json:"tags"`
}

// ServiceRegistry defines the interface for service discovery
type ServiceRegistry interface {
	Register(ctx context.Context, service *ServiceInfo) error
	Deregister(ctx context.Context, serviceID string) error
	Discover(ctx context.Context, serviceName string) ([]*ServiceInfo, error)
	Watch(ctx context.Context, serviceName string) (<-chan []*ServiceInfo, error)
	Close() error
}

// ServiceConfig holds service configuration
type ServiceConfig struct {
	Name        string
	Version     string
	Host        string
	Port        int
	HealthCheck string
	Metadata    map[string]string
	Tags        []string
}

// Service represents a service instance
type Service struct {
	config   *ServiceConfig
	registry ServiceRegistry
	logger   *log.Logger
}

// NewService creates a new service instance
func NewService(config *ServiceConfig, registry ServiceRegistry, logger *log.Logger) *Service {
	if logger == nil {
		logger = log.New(log.Writer(), "[SERVICE] ", log.LstdFlags|log.Lshortfile)
	}

	return &Service{
		config:   config,
		registry: registry,
		logger:   logger,
	}
}

// Register registers the service with the registry
func (s *Service) Register(ctx context.Context) error {
	serviceInfo := &ServiceInfo{
		Name:        s.config.Name,
		Version:     s.config.Version,
		Host:        s.config.Host,
		Port:        s.config.Port,
		HealthCheck: s.config.HealthCheck,
		Metadata:    s.config.Metadata,
		Tags:        s.config.Tags,
	}

	if err := s.registry.Register(ctx, serviceInfo); err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}

	s.logger.Printf("Service registered: %s v%s", s.config.Name, s.config.Version)
	return nil
}

// Deregister deregisters the service from the registry
func (s *Service) Deregister(ctx context.Context) error {
	serviceID := fmt.Sprintf("%s-%s-%s:%d", s.config.Name, s.config.Version, s.config.Host, s.config.Port)

	if err := s.registry.Deregister(ctx, serviceID); err != nil {
		return fmt.Errorf("failed to deregister service: %w", err)
	}

	s.logger.Printf("Service deregistered: %s", serviceID)
	return nil
}

// Discover discovers services by name
func (s *Service) Discover(ctx context.Context, serviceName string) ([]*ServiceInfo, error) {
	services, err := s.registry.Discover(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to discover service %s: %w", serviceName, err)
	}

	s.logger.Printf("Discovered %d instances of service %s", len(services), serviceName)
	return services, nil
}

// Watch watches for service changes
func (s *Service) Watch(ctx context.Context, serviceName string) (<-chan []*ServiceInfo, error) {
	ch, err := s.registry.Watch(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to watch service %s: %w", serviceName, err)
	}

	s.logger.Printf("Watching service: %s", serviceName)
	return ch, nil
}

// LoadBalancer defines the interface for load balancing
type LoadBalancer interface {
	Select(services []*ServiceInfo) (*ServiceInfo, error)
}

// RoundRobinLoadBalancer implements round-robin load balancing
type RoundRobinLoadBalancer struct {
	current int
}

// NewRoundRobinLoadBalancer creates a new round-robin load balancer
func NewRoundRobinLoadBalancer() *RoundRobinLoadBalancer {
	return &RoundRobinLoadBalancer{
		current: 0,
	}
}

// Select selects a service using round-robin
func (rr *RoundRobinLoadBalancer) Select(services []*ServiceInfo) (*ServiceInfo, error) {
	if len(services) == 0 {
		return nil, fmt.Errorf("no services available")
	}

	service := services[rr.current]
	rr.current = (rr.current + 1) % len(services)
	return service, nil
}

// RandomLoadBalancer implements random load balancing
type RandomLoadBalancer struct{}

// NewRandomLoadBalancer creates a new random load balancer
func NewRandomLoadBalancer() *RandomLoadBalancer {
	return &RandomLoadBalancer{}
}

// Select selects a service randomly
func (r *RandomLoadBalancer) Select(services []*ServiceInfo) (*ServiceInfo, error) {
	if len(services) == 0 {
		return nil, fmt.Errorf("no services available")
	}

	// Simple random selection (in production, use crypto/rand)
	index := time.Now().UnixNano() % int64(len(services))
	return services[index], nil
}

// ServiceClient represents a service client
type ServiceClient struct {
	registry     ServiceRegistry
	loadBalancer LoadBalancer
	logger       *log.Logger
}

// NewServiceClient creates a new service client
func NewServiceClient(registry ServiceRegistry, loadBalancer LoadBalancer, logger *log.Logger) *ServiceClient {
	if logger == nil {
		logger = log.New(log.Writer(), "[SERVICE-CLIENT] ", log.LstdFlags|log.Lshortfile)
	}

	return &ServiceClient{
		registry:     registry,
		loadBalancer: loadBalancer,
		logger:       logger,
	}
}

// GetService gets a service instance using load balancing
func (sc *ServiceClient) GetService(ctx context.Context, serviceName string) (*ServiceInfo, error) {
	services, err := sc.registry.Discover(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to discover service %s: %w", serviceName, err)
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("no instances of service %s found", serviceName)
	}

	service, err := sc.loadBalancer.Select(services)
	if err != nil {
		return nil, fmt.Errorf("failed to select service %s: %w", serviceName, err)
	}

	return service, nil
}

// GetServiceURL gets the URL for a service
func (sc *ServiceClient) GetServiceURL(ctx context.Context, serviceName string) (string, error) {
	service, err := sc.GetService(ctx, serviceName)
	if err != nil {
		return "", err
	}

	protocol := "http"
	if service.Metadata["protocol"] == "https" {
		protocol = "https"
	}

	return fmt.Sprintf("%s://%s:%d", protocol, service.Host, service.Port), nil
}
