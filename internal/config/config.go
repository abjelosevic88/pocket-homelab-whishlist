// Package config loads runtime configuration from environment variables.
//
// Every option has a sensible default so that `docker run` with a single
// volume works out of the box. See docs/configuration.md for the full list.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	Host           string
	Port           int
	BaseURL        string
	DataDir        string
	DBPath         string
	FilesDir       string
	LogLevel       string
	LogFormat      string // text | json
	TrustProxy     bool
	MetricsEnabled bool

	MaxUploadBytes int64

	AuthMode             string // local | proxy | none
	AuthProxyHeader      string
	AuthProxyEmailHeader string
	SessionTTL           time.Duration
	SecureCookies        bool
	AdminEmail           string
	AdminPassword        string
	AdminName            string

	// Sign in with an OpenID Connect provider. On a box with Pocket Core none
	// of these are needed: the app uses the client Core issues on registration.
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCName         string // button label
	OIDCAutoCreate   bool   // unknown emails get a member account

	DefaultCurrency string

	// The owner's integrations, copied once into the owner's own settings;
	// the values saved under Settings → Connected apps win.
	NotifyURL      string
	NotifyFormat   string // apprise | ntfy | json
	InventoryURL   string
	InventoryToken string
	MoneyURL       string
	MoneyToken     string
	SplitURL       string
	SplitToken     string
	SplitPublicURL string
	SplitGroupID   int64
	ContactsURL    string
	ContactsToken  string

	// Price checks: how many pages at most per sweep, and whether the
	// fetcher may reach private addresses (a shop on your own network).
	PriceChecksPerRun int
	FetchPrivate      bool

	// Deal hunting: the SearXNG that searches the web, the owner's OpenAI
	// settings (copied once), where people shop by default, rates.
	SearchURL     string
	OpenAIKey     string
	OpenAIModel   string
	OpenAIBaseURL string
	DealMarkets   string
	DeliverTo     string // ISO country code
	HuntsPerRun   int
	FXProvider    string // auto | frankfurter | erapi | none

	// Outbound lookups identify as this.
	UserAgent string

	// Pocket Core: when CORE_URL is set the app registers itself on start,
	// publishes events and accepts the tokens Core hands to paired phones.
	CoreURL             string
	CoreRegistrationKey string
	CoreAppURL          string // how Core reaches this app; defaults to BASE_URL

	DemoData bool
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(env(key, "")) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func envInt(key string, def int) int {
	if v := env(key, ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := env(key, ""); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := env(key, ""); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	dataDir := env("DATA_DIR", "./data")
	c := &Config{
		Host:           env("HOST", "0.0.0.0"),
		Port:           envInt("PORT", 8080),
		BaseURL:        strings.TrimRight(env("BASE_URL", ""), "/"),
		DataDir:        dataDir,
		DBPath:         env("DB_PATH", dataDir+"/pocket-wishlist.db"),
		FilesDir:       env("FILES_DIR", dataDir+"/files"),
		LogLevel:       strings.ToLower(env("LOG_LEVEL", "info")),
		LogFormat:      strings.ToLower(env("LOG_FORMAT", "text")),
		TrustProxy:     envBool("TRUST_PROXY", true),
		MetricsEnabled: envBool("METRICS_ENABLED", true),

		MaxUploadBytes: envInt64("MAX_UPLOAD_BYTES", 20<<20),

		AuthMode:             strings.ToLower(env("AUTH_MODE", "local")),
		AuthProxyHeader:      env("AUTH_PROXY_HEADER", "Remote-User"),
		AuthProxyEmailHeader: env("AUTH_PROXY_EMAIL_HEADER", "Remote-Email"),
		SessionTTL:           envDuration("SESSION_TTL", 720*time.Hour),
		SecureCookies:        envBool("SECURE_COOKIES", false),
		AdminEmail:           strings.ToLower(env("ADMIN_EMAIL", "")),
		AdminPassword:        env("ADMIN_PASSWORD", ""),
		AdminName:            env("ADMIN_NAME", "Admin"),
		OIDCIssuer:           strings.TrimRight(env("OIDC_ISSUER", ""), "/"),
		OIDCClientID:         env("OIDC_CLIENT_ID", ""),
		OIDCClientSecret:     env("OIDC_CLIENT_SECRET", ""),
		OIDCName:             env("OIDC_NAME", "single sign-on"),
		OIDCAutoCreate:       envBool("OIDC_AUTO_CREATE", true),

		DefaultCurrency: strings.ToUpper(env("DEFAULT_CURRENCY", "EUR")),

		NotifyURL:         env("NOTIFY_URL", ""),
		NotifyFormat:      strings.ToLower(env("NOTIFY_FORMAT", "")),
		InventoryURL:      strings.TrimRight(env("INVENTORY_URL", ""), "/"),
		InventoryToken:    env("INVENTORY_TOKEN", ""),
		MoneyURL:          strings.TrimRight(env("MONEY_URL", ""), "/"),
		MoneyToken:        env("MONEY_TOKEN", ""),
		SplitURL:          strings.TrimRight(env("SPLIT_URL", ""), "/"),
		SplitToken:        env("SPLIT_TOKEN", ""),
		SplitPublicURL:    strings.TrimRight(env("SPLIT_PUBLIC_URL", ""), "/"),
		SplitGroupID:      envInt64("SPLIT_GROUP_ID", 0),
		ContactsURL:       strings.TrimRight(env("CONTACTS_URL", ""), "/"),
		ContactsToken:     env("CONTACTS_TOKEN", ""),
		PriceChecksPerRun: envInt("PRICE_CHECKS_PER_RUN", 40),
		FetchPrivate:      envBool("FETCH_PRIVATE", false),
		SearchURL:         strings.TrimRight(env("SEARXNG_URL", ""), "/"),
		OpenAIKey:         env("OPENAI_API_KEY", ""),
		OpenAIModel:       env("OPENAI_MODEL", ""),
		OpenAIBaseURL:     strings.TrimRight(env("OPENAI_BASE_URL", ""), "/"),
		DealMarkets:       env("DEAL_MARKETS", ""),
		DeliverTo:         strings.ToUpper(env("DELIVER_TO", "")),
		HuntsPerRun:       envInt("HUNTS_PER_RUN", 3),
		FXProvider:        strings.ToLower(env("FX_PROVIDER", "auto")),

		UserAgent: env("USER_AGENT", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36 PocketWishlist/1.0"),

		CoreURL:             strings.TrimRight(env("CORE_URL", ""), "/"),
		CoreRegistrationKey: env("CORE_REGISTRATION_KEY", ""),
		CoreAppURL:          strings.TrimRight(env("CORE_APP_URL", ""), "/"),

		DemoData: envBool("DEMO_DATA", false),
	}
	if c.CoreAppURL == "" {
		c.CoreAppURL = c.BaseURL
	}
	switch c.AuthMode {
	case "local", "proxy", "none":
	default:
		return nil, fmt.Errorf("AUTH_MODE must be one of local, proxy, none (got %q)", c.AuthMode)
	}
	if c.Port <= 0 || c.Port > 65535 {
		return nil, fmt.Errorf("PORT out of range: %d", c.Port)
	}
	if c.OIDCIssuer != "" && (c.OIDCClientID == "" || c.BaseURL == "") {
		return nil, fmt.Errorf("OIDC_ISSUER needs OIDC_CLIENT_ID and BASE_URL (the callback is BASE_URL/api/v1/auth/oidc/callback)")
	}
	if c.PriceChecksPerRun <= 0 {
		c.PriceChecksPerRun = 40
	}
	return c, nil
}

// Addr returns host:port.
func (c *Config) Addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }
