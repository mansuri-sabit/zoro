package logger

import (
	"go.uber.org/zap"
	"github.com/troikatech/calling-agent/pkg/utils"
)

// MaskPhone creates a zap field that masks phone numbers
func MaskPhone(key, phone string) zap.Field {
	return zap.String(key, utils.MaskPhoneNumber(phone))
}

// MaskPhoneIfPresent masks phone if not empty
func MaskPhoneIfPresent(key, phone string) zap.Field {
	if phone == "" {
		return zap.String(key, "")
	}
	return MaskPhone(key, phone)
}

