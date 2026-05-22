package router

import "context"

// Coordinator runs regime routing for one or more books.
type Coordinator interface {
	RunOnce(ctx context.Context) ([]Decision, error)
	Run(ctx context.Context)
	LastDecisions() []Decision
	RecentDecisions() []Decision
}

// BookCoordinator owns one Engine per configured book.
type BookCoordinator struct {
	engines []*Engine
}

// NewBookCoordinator wraps per-book engines.
func NewBookCoordinator(engines []*Engine) *BookCoordinator {
	return &BookCoordinator{engines: engines}
}

func (c *BookCoordinator) RunOnce(ctx context.Context) ([]Decision, error) {
	out := make([]Decision, 0, len(c.engines))
	for _, e := range c.engines {
		d, err := e.RunOnce(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (c *BookCoordinator) Run(ctx context.Context) {
	RunAll(ctx, c.engines)
}

func (c *BookCoordinator) LastDecisions() []Decision {
	out := make([]Decision, 0, len(c.engines))
	for _, e := range c.engines {
		out = append(out, e.LastDecision())
	}
	return out
}

func (c *BookCoordinator) RecentDecisions() []Decision {
	var out []Decision
	for _, e := range c.engines {
		out = append(out, e.RecentDecisions()...)
	}
	if len(out) > 50 {
		out = out[len(out)-50:]
	}
	return out
}
