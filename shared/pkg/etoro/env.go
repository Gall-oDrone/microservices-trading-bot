package etoro

import "fmt"

// Environment selects demo (paper) or real trading API paths.
type Environment string

const (
	EnvDemo Environment = "demo"
	EnvReal Environment = "real"
)

// ParseEnvironment normalizes ETORO_ENV values (demo|real).
func ParseEnvironment(raw string) (Environment, error) {
	switch Environment(raw) {
	case EnvDemo, EnvReal:
		return Environment(raw), nil
	case "":
		return EnvDemo, nil
	default:
		return "", fmt.Errorf("invalid ETORO_ENV %q: use demo or real", raw)
	}
}

func (e Environment) String() string {
	return string(e)
}
