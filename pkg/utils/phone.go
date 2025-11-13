package utils

import (
	"regexp"
	"strings"
)

// MaskPhoneNumber masks a phone number for logging
// Example: +919876543210 -> +91•••••3210
func MaskPhoneNumber(phone string) string {
	if phone == "" {
		return ""
	}

	// Remove any whitespace
	phone = strings.TrimSpace(phone)

	// E.164 format: +[country][number]
	// For India: +91[10 digits]
	// Mask last 7 digits, show country code + first 3 digits

	// Match E.164 format
	re := regexp.MustCompile(`^(\+)(\d{1,3})(\d{3})(\d+)$`)
	matches := re.FindStringSubmatch(phone)

	if len(matches) == 5 {
		countryCode := matches[2]
		first3 := matches[3]
		lastDigits := matches[4]

		// Show first 3 digits, mask middle, show last 4
		if len(lastDigits) >= 4 {
			last4 := lastDigits[len(lastDigits)-4:]
			masked := strings.Repeat("•", len(lastDigits)-4)
			return "+" + countryCode + first3 + masked + last4
		}
	}

	// Fallback: mask all but last 4 characters
	if len(phone) > 4 {
		masked := strings.Repeat("•", len(phone)-4)
		return masked + phone[len(phone)-4:]
	}

	return strings.Repeat("•", len(phone))
}

// ValidateE164 validates E.164 phone number format
func ValidateE164(phone string) bool {
	re := regexp.MustCompile(`^\+[1-9]\d{1,14}$`)
	return re.MatchString(phone)
}

// NormalizePhone normalizes phone number to E.164 format
func NormalizePhone(phone string) string {
	// Remove all non-digit characters except +
	re := regexp.MustCompile(`[^\d+]`)
	cleaned := re.ReplaceAllString(phone, "")

	// If doesn't start with +, assume India (+91)
	if !strings.HasPrefix(cleaned, "+") {
		if strings.HasPrefix(cleaned, "91") {
			cleaned = "+" + cleaned
		} else if strings.HasPrefix(cleaned, "0") {
			// Remove leading 0 and add +91
			cleaned = "+91" + cleaned[1:]
		} else {
			cleaned = "+91" + cleaned
		}
	}

	return cleaned
}
