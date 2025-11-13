package logger

import (
	"go.uber.org/zap"
	"github.com/troikatech/calling-agent/pkg/utils"
)

// SafeFields returns zap fields with phone numbers masked
func SafeFields(fields map[string]interface{}) []zap.Field {
	var zapFields []zap.Field
	
	for k, v := range fields {
		switch val := v.(type) {
		case string:
			// Check if it looks like a phone number
			if utils.ValidateE164(val) || len(val) > 10 {
				zapFields = append(zapFields, MaskPhone(k, val))
			} else {
				zapFields = append(zapFields, zap.String(k, val))
			}
		case int, int64, int32:
			zapFields = append(zapFields, zap.Int64(k, int64(val.(int))))
		case bool:
			zapFields = append(zapFields, zap.Bool(k, val))
		default:
			zapFields = append(zapFields, zap.Any(k, val))
		}
	}
	
	return zapFields
}

