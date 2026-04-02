package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	ServerPort  string
	Environment string

	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	PostgresMaxConns int

	MongoURI string
	MongoDB  string

	RedisHost     string
	RedisPort     string
	RedisPassword string

	JWTSecret      string
	JWTExpiryHours int

	ExecutorPort string
	ExecutorURL  string

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	KafkaBroker string
	JaegerEndpoint string
	MetricsPort string
}

func Load() (*Config, error) {
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		fmt.Printf("No .env file found, reading from environment: %v\n", err)
	}

	cfg := &Config{
		ServerPort:  viper.GetString("SERVER_PORT"),
		Environment: viper.GetString("ENVIRONMENT"),

		PostgresHost:     viper.GetString("POSTGRES_HOST"),
		PostgresPort:     viper.GetString("POSTGRES_PORT"),
		PostgresUser:     viper.GetString("POSTGRES_USER"),
		PostgresPassword: viper.GetString("POSTGRES_PASSWORD"),
		PostgresDB:       viper.GetString("POSTGRES_DB"),
		PostgresMaxConns: viper.GetInt("POSTGRES_MAX_CONNS"),

		MongoURI: viper.GetString("MONGO_URI"),
		MongoDB:  viper.GetString("MONGO_DB"),

		RedisHost:     viper.GetString("REDIS_HOST"),
		RedisPort:     viper.GetString("REDIS_PORT"),
		RedisPassword: viper.GetString("REDIS_PASSWORD"),

		JWTSecret:      viper.GetString("JWT_SECRET"),
		JWTExpiryHours: viper.GetInt("JWT_EXPIRY_HOURS"),

		ExecutorPort: viper.GetString("EXECUTOR_PORT"),
		ExecutorURL:  viper.GetString("EXECUTOR_URL"),

		GoogleClientID:     viper.GetString("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: viper.GetString("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  viper.GetString("GOOGLE_REDIRECT_URL"),
		KafkaBroker: viper.GetString("KAFKA_BROKER"),
		JaegerEndpoint: viper.GetString("JAEGER_ENDPOINT"),
		MetricsPort: viper.GetString("METRICS_PORT"),
	}

	if cfg.ServerPort == "" {
		return nil, fmt.Errorf("SERVER_PORT is required")
	}
	if cfg.PostgresHost == "" {
		return nil, fmt.Errorf("POSTGRES_HOST is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	return cfg, nil
}

func LoadExecutor() (*Config, error) {
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err != nil {
		fmt.Printf("no .env file found, reading from environment: %v\n", err)
	}

	cfg := &Config{
		PostgresHost:     viper.GetString("POSTGRES_HOST"),
		PostgresPort:     viper.GetString("POSTGRES_PORT"),
		PostgresUser:     viper.GetString("POSTGRES_USER"),
		PostgresPassword: viper.GetString("POSTGRES_PASSWORD"),
		PostgresDB:       viper.GetString("POSTGRES_DB"),
		PostgresMaxConns: viper.GetInt("POSTGRES_MAX_CONNS"),
		MongoURI:         viper.GetString("MONGO_URI"),
		MongoDB:          viper.GetString("MONGO_DB"),
		RedisHost:     viper.GetString("REDIS_HOST"),
    	RedisPort:     viper.GetString("REDIS_PORT"),
    	RedisPassword: viper.GetString("REDIS_PASSWORD"),
		KafkaBroker: viper.GetString("KAFKA_BROKER"),
		JaegerEndpoint: viper.GetString("JAEGER_ENDPOINT"),
		MetricsPort: viper.GetString("METRICS_PORT"),
	}

	if cfg.PostgresHost == "" {
		return nil, fmt.Errorf("POSTGRES_HOST is required")
	}
	if cfg.MongoURI == "" {
		return nil, fmt.Errorf("MONGO_URI is required")
	}

	return cfg, nil
}
