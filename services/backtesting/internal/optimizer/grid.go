package optimizer

import (
	"math"
)

// ParameterGrid generates all combinations of parameter values
type ParameterGrid struct {
	parameters map[string]*ParameterRange
}

// NewParameterGrid creates a new parameter grid
func NewParameterGrid(parameters map[string]*ParameterRange) *ParameterGrid {
	return &ParameterGrid{
		parameters: parameters,
	}
}

// Generate generates all parameter combinations
func (g *ParameterGrid) Generate() []map[string]interface{} {
	if len(g.parameters) == 0 {
		return []map[string]interface{}{}
	}
	
	// Build value lists for each parameter
	paramNames := make([]string, 0, len(g.parameters))
	paramValues := make([][]interface{}, 0, len(g.parameters))
	
	for name, param := range g.parameters {
		paramNames = append(paramNames, name)
		
		var values []interface{}
		if param.Values != nil && len(param.Values) > 0 {
			// Use discrete values
			values = param.Values
		} else {
			// Generate values from range
			values = generateRangeValues(param.Min, param.Max, param.Step)
		}
		
		paramValues = append(paramValues, values)
	}
	
	// Generate all combinations
	combinations := g.generateCombinations(paramNames, paramValues, 0, make(map[string]interface{}))
	
	return combinations
}

// generateCombinations recursively generates all parameter combinations
func (g *ParameterGrid) generateCombinations(
	names []string,
	values [][]interface{},
	index int,
	current map[string]interface{},
) []map[string]interface{} {
	if index >= len(names) {
		// Base case: copy current combination
		result := make(map[string]interface{})
		for k, v := range current {
			result[k] = v
		}
		return []map[string]interface{}{result}
	}
	
	// Recursive case: iterate through values for current parameter
	results := make([]map[string]interface{}, 0)
	name := names[index]
	
	for _, value := range values[index] {
		current[name] = value
		subResults := g.generateCombinations(names, values, index+1, current)
		results = append(results, subResults...)
	}
	
	return results
}

// generateRangeValues generates a list of values from min to max with step
func generateRangeValues(min, max, step float64) []interface{} {
	count := int(math.Ceil((max - min) / step)) + 1
	values := make([]interface{}, 0, count)
	
	for i := 0; i < count; i++ {
		value := min + float64(i)*step
		if value > max {
			break
		}
		values = append(values, value)
	}
	
	return values
}

// Count returns the total number of combinations
func (g *ParameterGrid) Count() int {
	if len(g.parameters) == 0 {
		return 0
	}
	
	total := 1
	for _, param := range g.parameters {
		var count int
		if param.Values != nil && len(param.Values) > 0 {
			count = len(param.Values)
		} else {
			count = int(math.Ceil((param.Max - param.Min) / param.Step)) + 1
		}
		total *= count
	}
	
	return total
}

