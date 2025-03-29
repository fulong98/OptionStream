package config

import (
	"github.com/spf13/viper"
)

type Config struct {
	Deribit DeribitConfig
	Kafka   KafkaConfig
}

type DeribitConfig struct {
	WebSocketURL string   `mapstructure:"websocket_url"`
	Testnet      bool     `mapstructure:"testnet"`
	Instruments  []string `mapstructure:"instruments"`
}

type KafkaConfig struct {
	Brokers           []string `mapstructure:"brokers"`
	Topic             string   `mapstructure:"topic"`
	BatchSize         int      `mapstructure:"batch_size"`
	BatchTimeoutMs    int      `mapstructure:"batch_timeout_ms"`
	MaxRetries        int      `mapstructure:"max_retries"`
	RetryBackoffMs    int      `mapstructure:"retry_backoff_ms"`
	RequiredAcks      int      `mapstructure:"required_acks"`
	EnableIdempotency bool     `mapstructure:"enable_idempotency"`
	CompressionType   string   `mapstructure:"compression_type"`
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	// Set defaults
	viper.SetDefault("deribit.websocket_url", "wss://www.deribit.com/ws/api/v2")
	viper.SetDefault("deribit.testnet", true)
	viper.SetDefault("kafka.brokers", []string{"localhost:9092"})
	viper.SetDefault("kafka.topic", "orderbook.deribit.options")
	viper.SetDefault("kafka.batch_size", 100)
	viper.SetDefault("kafka.batch_timeout_ms", 100)
	viper.SetDefault("kafka.max_retries", 3)
	viper.SetDefault("kafka.retry_backoff_ms", 100)
	viper.SetDefault("kafka.required_acks", -1) // -1 means all replicas must acknowledge
	viper.SetDefault("kafka.enable_idempotency", true)
	viper.SetDefault("kafka.compression_type", "snappy")

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
