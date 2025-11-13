package utils

import (
	"encoding/json"
)

// GetTotalCount gets the total count for a query (without pagination)
// DEPRECATED: This function is not used. Use MongoDB query builder's Count() method directly.
// Example: client.NewQuery("table").Eq("field", value).Count(ctx)
func GetTotalCount(client interface{}, table string, filters map[string]interface{}) (int64, error) {
	// This function is deprecated and not used in the codebase
	// Use MongoDB query builder's Count() method directly instead
	return 0, nil
}

// GetTotalCountWithQuery gets total count using a query builder
// DEPRECATED: This function is not used. Use MongoDB query builder's Count() method directly.
func GetTotalCountWithQuery(query interface{}) (int64, error) {
	// This function is deprecated and not used in the codebase
	// Use MongoDB query builder's Count() method directly instead
	return 0, nil
}

// CountQueryResult counts results from a query response
func CountQueryResult(data []byte) (int64, error) {
	var results []map[string]interface{}
	if err := json.Unmarshal(data, &results); err != nil {
		return 0, err
	}
	return int64(len(results)), nil
}

