package config

// DatabaseConfig holds database-related settings.
// When URL is set, it takes priority over Server/Port and connects directly
// using the full connection string (required for CockroachDB Cloud).
type DatabaseConfig struct {
	// Full connection URL (e.g. postgresql://user:pass@host:26257/db?sslmode=verify-full)
	// When set, Server and Port are ignored.
	URL string `mapstructure:"URL" json:"url" validate:"omitempty"`

	// Connection settings (used when URL is empty)
	Server string `mapstructure:"SERVER"            json:"server"            validate:"omitempty,host"`
	Port   int    `mapstructure:"PORT"             json:"port"             validate:"omitempty,min=1,max=65535"`
}
