package validation

import (
	"fmt"
	"regexp"
	"strings"
)

var e164Regex = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

func ValidateE164(phone string) error {
	if phone == "" {
		return fmt.Errorf("phone number is required")
	}

	phone = strings.TrimSpace(phone)

	if !e164Regex.MatchString(phone) {
		return fmt.Errorf("phone number must be in E.164 format (e.g., +919876543210)")
	}

	return nil
}

func NormalizeE164(phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")
	phone = strings.ReplaceAll(phone, "(", "")
	phone = strings.ReplaceAll(phone, ")", "")

	if !strings.HasPrefix(phone, "+") {
		if strings.HasPrefix(phone, "91") && len(phone) == 12 {
			phone = "+" + phone
		} else if len(phone) == 10 {
			phone = "+91" + phone
		} else {
			return "", fmt.Errorf("cannot normalize phone number: %s", phone)
		}
	}

	if err := ValidateE164(phone); err != nil {
		return "", err
	}

	return phone, nil
}
