package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// VerifyExotelSignature verifies Exotel webhook HMAC signature
// Exotel sends signature in X-Exotel-Signature header
// Signature is HMAC-SHA256 of sorted form values
// If secret is empty, verification is skipped (for development/testing)
func VerifyExotelSignature(secret string, formValues url.Values, signature string) error {
	// Skip verification if secret is not configured (for development/testing)
	if secret == "" {
		return nil
	}

	if signature == "" {
		return fmt.Errorf("signature header missing")
	}

	// Sort form values and create signature string
	var keys []string
	for k := range formValues {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		values := formValues[k]
		for _, v := range values {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
	}

	signatureString := strings.Join(parts, "&")

	// Compute HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signatureString))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	// Compare signatures (constant-time comparison)
	if !hmac.Equal([]byte(expectedSignature), []byte(signature)) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

