package config

const (
	defaultConfigPath = "/etc/vyos-nats-agent/config.yaml"
	envConfigPathKey  = "VYOS_NATS_AGENT_CONFIG"
)

func DefaultAppConfig() AppConfig {
	return AppConfig{
		Agent: AgentConfig{
			Name:      "vyos-nats-agent",
			Version:   "0.1.0",
			Target:    "vyos",
			StateFile: "/tmp/vyos-nats-agent/state.json",
			Logging: LoggingConfig{
				Enabled: true,
				Level:   "info",
				Format:  "text",
			},
			Configure: ConfigureConfig{
				Mode: "placeholder",
			},
			Debug: DebugConfig{
				LogPayloads:  false,
				LogRendered:  false,
				LogApplyPlan: false,
			},
			Apply: ApplyConfig{
				SaveAfterCommit: false,
			},
			Actions: ActionsConfig{
				Mode:    "placeholder",
				Enabled: []string{"trace"},
			},
		},
		AgentCore: AgentCoreConfig{
			NATS: NATSConfig{
				Servers:              []string{"nats://127.0.0.1:4222"},
				ClientName:           "vyos-nats-agent",
				ConnectTimeout:       "5s",
				RetryOnFailedConnect: false,
				MaxReconnects:        -1,
				ReconnectWait:        "2s",
			},
			JetStream: JetStreamConfig{
				DefaultTimeout: "5s",
			},
			Subjects: SubjectConfig{
				ConfigurePattern: "cmd.configure.%s",
				ActionPattern:    "cmd.action.%s.%s",
				ResultPattern:    "result.%s",
				StatusPattern:    "status.%s",
				HealthPattern:    "health.%s",
			},
			KV: KVConfig{
				Bucket:           "cfg_desired",
				KeyPattern:       "desired.%s",
				AutoCreateBucket: true,
				History:          1,
				Storage:          "file",
				Replicas:         1,
			},
			Timeouts: TimeoutConfig{
				PublishTimeout:   "5s",
				SubscribeTimeout: "5s",
				KVTimeout:        "5s",
				ShutdownTimeout:  "10s",
				HandlerWarnAfter: "2s",
			},
			Retry: RetryConfig{
				PublishAttempts: 1,
				PublishBackoff:  "250ms",
			},
			Execution: ExecutionConfig{
				HandlerMode: "sync",
			},
		},
	}
}
