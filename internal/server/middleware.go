package server

import (
	"log"
	"net/http"
	"strings"
	"time"

	// "nntp-web/internal/ratelimit" // Not directly used in this file; s.rateLimiter type comes from server.go
	"nntp-web/internal/recaptcha"
)

const recaptchaCookieName = "oreo"
const recaptchaCookieDuration = 24 * time.Hour // Valid for 1 day

// Helper function to get client IP address
// It considers X-Forwarded-For header if present, otherwise falls back to RemoteAddr.
func getClientIP(r *http.Request) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// X-Forwarded-For can be a comma-separated list of IPs. The first one is the original client.
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	// Fallback to RemoteAddr, stripping port
	ip := r.RemoteAddr
	if colonPos := strings.LastIndex(ip, ":"); colonPos != -1 {
		ip = ip[:colonPos]
	}
	return ip
}

// BotProtectionMiddleware handles rate limiting and reCAPTCHA challenges.
// It needs access to the server's template rendering functionality to display the CAPTCHA page.
func (s *Server) BotProtectionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If bot protection is not enabled in config, or ratelimiter is nil, pass through
		if !s.config.BotProtectionEnabled || s.rateLimiter == nil {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := getClientIP(r)

		// 1. Check for "oreo" cookie
		cookie, err := r.Cookie(recaptchaCookieName)
		if err == nil && cookie.Value == "verified" { // Basic check, could be more robust (e.g. signed cookie)
			// log.Printf("BotProtection: IP %s allowed via cookie", clientIP)
			next.ServeHTTP(w, r)
			return
		}

		// 2. If no valid cookie, check rate limiter
		if s.rateLimiter.Allow(clientIP) {
			// log.Printf("BotProtection: IP %s allowed by rate limiter", clientIP)
			next.ServeHTTP(w, r)
			return
		}

		// 3. Rate limit exceeded, require CAPTCHA
		// log.Printf("BotProtection: IP %s exceeded rate limit, requiring CAPTCHA for %s", clientIP, r.URL.Path)

		if r.Method == http.MethodPost {
			// Ensure form is parsed before accessing r.FormValue
			if err := r.ParseForm(); err != nil {
				log.Printf("BotProtection: Error parsing form for IP %s: %v", clientIP, err)
				http.Error(w, "Bad Request", http.StatusBadRequest)
				return
			}
			captchaResponse := r.FormValue("g-recaptcha-response")
			if captchaResponse == "" {
				// log.Printf("BotProtection: IP %s - Empty CAPTCHA response", clientIP)
				s.serveCaptchaPage(w, r, "Please complete the CAPTCHA.", http.StatusPaymentRequired) // 402
				return
			}

			// The RecaptchaSecret and SiteKey should be available from s.config
			isValid, err := recaptcha.Verify(s.config.RecaptchaSecret, captchaResponse, clientIP)
			if err != nil {
				log.Printf("BotProtection: Error verifying CAPTCHA for IP %s: %v", clientIP, err)
				s.serveCaptchaPage(w, r, "Error verifying CAPTCHA. Please try again.", http.StatusPaymentRequired) // 402
				return
			}

			if isValid {
				// log.Printf("BotProtection: IP %s passed CAPTCHA", clientIP)
				http.SetCookie(w, &http.Cookie{
					Name:     recaptchaCookieName,
					Value:    "verified",
					Path:     "/",
					Expires:  time.Now().Add(recaptchaCookieDuration),
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode, // Or StrictMode
				})
				// Important: After setting the cookie, redirect or serve the original request.
				// For simplicity, redirecting to the same URL (GET) often works,
				// but the original request might have been a POST.
				// A common pattern is to redirect to GET, or simply allow the current (POST) request to proceed.
				// Let's allow the current request to proceed.
				next.ServeHTTP(w, r)
				return
			} else {
				// log.Printf("BotProtection: IP %s failed CAPTCHA", clientIP)
				s.serveCaptchaPage(w, r, "CAPTCHA verification failed. Please try again.", http.StatusPaymentRequired) // 402
				return
			}
		}

		// For GET requests or POSTs without captcha response, show CAPTCHA page
		s.serveCaptchaPage(w, r, "", http.StatusPaymentRequired) // 402
	})
}

// serveCaptchaPage renders the CAPTCHA page.
// This method needs to be part of the Server struct to access templates and config.
func (s *Server) serveCaptchaPage(w http.ResponseWriter, r *http.Request, errorMessage string, statusCode int) {
	// Ensure SiteKey is available from config
	if s.config.RecaptchaSiteKey == "" {
		log.Println("ERROR: RecaptchaSiteKey is not configured.")
		http.Error(w, "CAPTCHA configuration error.", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode) // Set the 402 status code

	data := struct {
		SiteKey      string
		ErrorMessage string
		CurrentPath  string
	}{
		SiteKey:      s.config.RecaptchaSiteKey,
		ErrorMessage: errorMessage,
		CurrentPath:  r.URL.RequestURI(), // To post back to the same page
	}

	// Assuming "captcha.html.tmpl" will be added and s.renderTemplate can render it.
	// If renderTemplate sets its own status code on error, ensure it doesn't override our 402.
	// The existing renderTemplate logs and writes http.StatusInternalServerError.
	// We need to be careful here. For now, let's assume s.templates.ExecuteTemplate
	// is the core part.

	err := s.templates.ExecuteTemplate(w, "captcha.html.tmpl", data)
	if err != nil {
		// If template execution fails *after* WriteHeader, we can't change the status code.
		// Log the error. The client will receive a partial response or just the 402 header.
		log.Printf("Error executing captcha template: %v", err)
		// Avoid writing another http.Error if headers are already sent.
		// http.Error(w, "Failed to render CAPTCHA page.", http.StatusInternalServerError)
	}
}
