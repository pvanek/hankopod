package hanko

import "time"

type User struct {
	ID                  string               `json:"id"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
	Emails              []Email              `json:"emails"`
	Username            *Username            `json:"username"`
	WebauthnCredentials []WebauthnCredential `json:"webauthn_credentials"`
}

type Email struct {
	ID         string `json:"id"`
	Address    string `json:"address"`
	IsVerified bool   `json:"is_verified"`
	IsPrimary  bool   `json:"is_primary"`
}

type Username struct {
	Username string `json:"username"`
}

type WebauthnCredential struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	MFAOnly    bool       `json:"mfa_only"`
}

type Session struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	UserAgent string     `json:"user_agent"`
	IPAddress string     `json:"ip_address"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	LastUsed  time.Time  `json:"last_used"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type AuditLog struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Error         string    `json:"error"`
	SourceIP      string    `json:"meta_source_ip"`
	UserAgent     string    `json:"meta_user_agent"`
	ActorUserID   string    `json:"actor_user_id"`
	ActorEmail    string    `json:"actor_email"`
	CreatedAt     time.Time `json:"created_at"`
	HTTPRequestID string    `json:"meta_http_request_id"`
}
