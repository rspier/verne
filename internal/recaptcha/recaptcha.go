package recaptcha

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"
)

const googleRecaptchaVerifyURL = "https://www.google.com/recaptcha/api/siteverify"

// Response is the structure of the JSON response from Google's reCAPTCHA verification server.
type Response struct {
	Success     bool      `json:"success"`
	ChallengeTS time.Time `json:"challenge_ts"` // Timestamp of the challenge load (ISO format yyyy-MM-dd'T'HH:mm:ssZZ)
	Hostname    string    `json:"hostname"`     // The hostname of the site where the reCAPTCHA was solved
	ErrorCodes  []string  `json:"error-codes"`  // Optional
}

// Verify sends the reCAPTCHA response token to Google for verification.
// It takes the shared secret key and the user's response token as input.
// Returns true if the token is valid, false otherwise, along with any error encountered.
func Verify(secret string, responseToken string, remoteIP string) (bool, error) {
	if secret == "" {
		return false, errors.New("recaptcha secret is not configured")
	}
	if responseToken == "" {
		return false, errors.New("recaptcha response token is empty")
	}

	formData := url.Values{}
	formData.Set("secret", secret)
	formData.Set("response", responseToken)
	if remoteIP != "" { // remoteip is an optional parameter
		formData.Set("remoteip", remoteIP)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.PostForm(googleRecaptchaVerifyURL, formData)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var recaptchaResponse Response
	if err := json.Unmarshal(body, &recaptchaResponse); err != nil {
		return false, err
	}

	if !recaptchaResponse.Success {
		// Optionally log error codes: log.Printf("reCAPTCHA verification failed: %v", recaptchaResponse.ErrorCodes)
		return false, nil // Not an error in communication, but CAPTCHA failed
	}

	return true, nil
}
