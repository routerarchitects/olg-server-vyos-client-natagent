package config

type AppConfig struct {
	Agent     AgentConfig     `yaml:"agent"`
	AgentCore AgentCoreConfig `yaml:"agentcore"`
}

type AgentConfig struct {
	Name      string          `yaml:"name"`
	Version   string          `yaml:"version"`
	Target    string          `yaml:"target"`
	StateFile string          `yaml:"state_file"`
	Logging   LoggingConfig   `yaml:"logging"`
	Configure ConfigureConfig `yaml:"configure"`
	Debug     DebugConfig     `yaml:"debug"`
	Apply     ApplyConfig     `yaml:"apply"`
	Actions   ActionsConfig   `yaml:"actions"`
}

type LoggingConfig struct {
	Enabled bool   `yaml:"enabled"`
	Level   string `yaml:"level"`
	Format  string `yaml:"format"`
}

type ConfigureConfig struct {
	Mode string `yaml:"mode"`
}

type DebugConfig struct {
	LogPayloads  bool `yaml:"log_payloads"`
	LogRendered  bool `yaml:"log_rendered"`
	LogApplyPlan bool `yaml:"log_apply_plan"`
}

type ApplyConfig struct {
	SaveAfterCommit bool `yaml:"save_after_commit"`
}

type ActionsConfig struct {
	Mode    string   `yaml:"mode"`
	Enabled []string `yaml:"enabled"`
}

type AgentCoreConfig struct {
	NATS      NATSConfig      `yaml:"nats"`
	JetStream JetStreamConfig `yaml:"jetstream"`
	Subjects  SubjectConfig   `yaml:"subjects"`
	KV        KVConfig        `yaml:"kv"`
	Timeouts  TimeoutConfig   `yaml:"timeouts"`
	Retry     RetryConfig     `yaml:"retry"`
	Execution ExecutionConfig `yaml:"execution"`
}

type NATSConfig struct {
	Servers              []string  `yaml:"servers"`
	ClientName           string    `yaml:"client_name"`
	CredentialsFile      string    `yaml:"credentials_file"`
	NKeySeedFile         string    `yaml:"nkey_seed_file"`
	UserJWTFile          string    `yaml:"user_jwt_file"`
	Username             string    `yaml:"username"`
	Password             string    `yaml:"password"`
	Token                string    `yaml:"token"`
	ConnectTimeout       string    `yaml:"connect_timeout"`
	RetryOnFailedConnect bool      `yaml:"retry_on_failed_connect"`
	MaxReconnects        int       `yaml:"max_reconnects"`
	ReconnectWait        string    `yaml:"reconnect_wait"`
	ReconnectBufSize     int       `yaml:"reconnect_buf_size"`
	TLS                  TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Enabled            bool   `yaml:"enabled"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
	CAFile             string `yaml:"ca_file"`
	CertFile           string `yaml:"cert_file"`
	KeyFile            string `yaml:"key_file"`
	ServerName         string `yaml:"server_name"`
}

type JetStreamConfig struct {
	Domain         string `yaml:"domain"`
	APIPrefix      string `yaml:"api_prefix"`
	DefaultTimeout string `yaml:"default_timeout"`
}

type SubjectConfig struct {
	ConfigurePattern string `yaml:"configure_pattern"`
	ActionPattern    string `yaml:"action_pattern"`
	ResultPattern    string `yaml:"result_pattern"`
	StatusPattern    string `yaml:"status_pattern"`
	HealthPattern    string `yaml:"health_pattern"`
}

type KVConfig struct {
	Bucket           string `yaml:"bucket"`
	KeyPattern       string `yaml:"key_pattern"`
	AutoCreateBucket bool   `yaml:"auto_create_bucket"`
	History          uint8  `yaml:"history"`
	TTL              string `yaml:"ttl"`
	MaxValueSize     int32  `yaml:"max_value_size"`
	Storage          string `yaml:"storage"`
	Replicas         int    `yaml:"replicas"`
}

type TimeoutConfig struct {
	PublishTimeout   string `yaml:"publish_timeout"`
	SubscribeTimeout string `yaml:"subscribe_timeout"`
	KVTimeout        string `yaml:"kv_timeout"`
	ShutdownTimeout  string `yaml:"shutdown_timeout"`
	HandlerWarnAfter string `yaml:"handler_warn_after"`
}

type RetryConfig struct {
	PublishAttempts int    `yaml:"publish_attempts"`
	PublishBackoff  string `yaml:"publish_backoff"`
}

type ExecutionConfig struct {
	HandlerMode string `yaml:"handler_mode"`
}
