package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type KeycloakConfig struct {
	URL      string `mapstructure:"url"`
	Realm    string `mapstructure:"realm"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Provider string `mapstructure:"provider"`
}

type GithubConfig struct {
	Token        string   `mapstructure:"token"`
	Org          string   `mapstructure:"org"`
	IgnoredUsers []string `mapstructure:"ignored_users"`
}

type DiscordConfig struct {
	Token         string `mapstructure:"token"`
	GuildID       string `mapstructure:"guildid"`
	LogChannelID  string `mapstructure:"logchannelid"`
	DebugThreadID string `mapstructure:"debugthreadid"`
	BaseRoleID    string `mapstructure:"baseroleid"`
}

type SyncConfig struct {
	Interval    time.Duration `mapstructure:"interval"`
	MappingFile string        `mapstructure:"mappingfile"`
	Parallelism int           `mapstructure:"parallelism"`
}

type Config struct {
	Keycloak KeycloakConfig `mapstructure:"keycloak"`
	Github   GithubConfig   `mapstructure:"github"`
	Discord  DiscordConfig  `mapstructure:"discord"`
	Sync     SyncConfig     `mapstructure:"sync"`
	Metrics  struct {
		Addr string `mapstructure:"addr"`
	} `mapstructure:"metrics"`
	Mappings map[string]LDAPGroupMapping
}

type LDAPGroupMapping struct {
	DiscordRoleID  int64  `mapstructure:"discord_role_id"`
	GithubTeamSlug string `mapstructure:"github_team_slug"`
}

// BindFlags defines all CLI flags and binds them to Viper.
// This is the single source of truth for configuration keys and flags.
func BindFlags(flags *pflag.FlagSet, v *viper.Viper) {
	flags.String("keycloak-url", "", "Keycloak server URL")
	flags.String("keycloak-realm", "", "Keycloak realm")
	flags.String("keycloak-username", "", "Keycloak admin username")
	flags.String("keycloak-password", "", "Keycloak admin password")
	flags.String("keycloak-provider", "github", "Keycloak identity provider name")
	flags.String("github-token", "", "GitHub Personal Access Token")
	flags.String("github-org", "", "GitHub organization name")
	flags.String("discord-token", "", "Discord bot token")
	flags.String("discord-guildid", "", "Discord server ID")
	flags.String("discord-logchannel", "", "Discord channel ID for logs")
	flags.String("discord-debugthread", "", "Discord thread ID for sync reports")
	flags.String("discord-baserole", "", "Discord base role ID")
	flags.Duration("sync-interval", 5*time.Minute, "Interval between sync runs")
	flags.String("sync-mappingfile", "mappings.toml", "Path to role mapping TOML file")
	flags.Int("sync-parallelism", 20, "Number of concurrent API calls")
	flags.String("metrics-addr", ":9090", "Prometheus metrics address")
	flags.StringSlice("github-ignored-users", []string{"pydis-bot"}, "GitHub users to ignore")

	mappings := map[string]string{
		"keycloak-url":         "keycloak.url",
		"keycloak-realm":       "keycloak.realm",
		"keycloak-username":    "keycloak.username",
		"keycloak-password":    "keycloak.password",
		"keycloak-provider":    "keycloak.provider",
		"github-token":         "github.token",
		"github-org":           "github.org",
		"discord-token":        "discord.token",
		"discord-guildid":      "discord.guildid",
		"discord-logchannel":   "discord.logchannelid",
		"discord-debugthread":  "discord.debugthreadid",
		"discord-baserole":     "discord.baseroleid",
		"sync-interval":        "sync.interval",
		"sync-mappingfile":     "sync.mappingfile",
		"sync-parallelism":     "sync.parallelism",
		"metrics-addr":         "metrics.addr",
		"github-ignored-users": "github.ignored_users",
	}

	for flagName, configKey := range mappings {
		_ = v.BindPFlag(configKey, flags.Lookup(flagName))
		_ = v.BindEnv(configKey)
	}
}

func LoadConfig(v *viper.Viper) (*Config, error) {
	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Load mappings from TOML
	mv := viper.New()
	mv.SetConfigFile(cfg.Sync.MappingFile)
	if err := mv.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read mapping file %s: %w", cfg.Sync.MappingFile, err)
	}

	mappings := make(map[string]LDAPGroupMapping)
	if err := mv.UnmarshalKey("mappings", &mappings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mappings: %w", err)
	}
	cfg.Mappings = mappings

	return cfg, nil
}

func (c *Config) Validate() error {
	var missing []string

	fields := map[string]string{
		"HAFNIUM_KEYCLOAK_URL":          c.Keycloak.URL,
		"HAFNIUM_KEYCLOAK_REALM":        c.Keycloak.Realm,
		"HAFNIUM_KEYCLOAK_USERNAME":     c.Keycloak.Username,
		"HAFNIUM_KEYCLOAK_PASSWORD":     c.Keycloak.Password,
		"HAFNIUM_GITHUB_TOKEN":          c.Github.Token,
		"HAFNIUM_GITHUB_ORG":            c.Github.Org,
		"HAFNIUM_DISCORD_TOKEN":         c.Discord.Token,
		"HAFNIUM_DISCORD_GUILDID":       c.Discord.GuildID,
		"HAFNIUM_DISCORD_LOGCHANNELID":  c.Discord.LogChannelID,
		"HAFNIUM_DISCORD_DEBUGTHREADID": c.Discord.DebugThreadID,
		"HAFNIUM_DISCORD_BASEROLEID":    c.Discord.BaseRoleID,
	}

	for env, val := range fields {
		if val == "" {
			missing = append(missing, env)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration values: %s", strings.Join(missing, ", "))
	}

	return nil
}
