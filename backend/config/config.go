package config

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	App      AppConfig      `yaml:"app"`
	HTTP     HTTPConfig     `yaml:"http"`
	NDTP     NDTPConfig     `yaml:"ndtp"`
	MLClient MLClientConfig `yaml:"ml_client"`
	Dataset  DatasetConfig  `yaml:"dataset"`
}

type AppConfig struct {
	Name           string `env:"APP_NAME" env-default:"baboteek-regtrans"`
	Environment    string `env:"APP_ENV" env-default:"production"`
	LogLevel       string `env:"LOG_LEVEL" env-default:"info"`
	Timezone       string `env:"TIMEZONE" env-default:"Europe/Moscow"`
	TimeShiftHours int    `env:"TIME_SHIFT_HOURS" env-default:"0"`
}

type HTTPConfig struct {
	Port         int           `env:"HTTP_PORT" env-default:"8080"`
	ReadTimeout  time.Duration `env:"HTTP_READ_TIMEOUT" env-default:"5s"`
	WriteTimeout time.Duration `env:"HTTP_WRITE_TIMEOUT" env-default:"5s"`
}

type NDTPConfig struct {
	TCPPort        int           `env:"NDTP_TCP_PORT" env-default:"9201"`
	ReadTimeout    time.Duration `env:"NDTP_READ_TIMEOUT" env-default:"30s"`
	MaxPacketSize  int           `env:"NDTP_MAX_PACKET_SIZE" env-default:"65535"`
	WorkerPoolSize int           `env:"NDTP_WORKER_POOL" env-default:"100"`
}

type MLClientConfig struct {
	Host           string        `env:"ML_GRPC_HOST" env-required:"true"`
	Port           int           `env:"ML_GRPC_PORT" env-required:"true"`
	Timeout        time.Duration `env:"ML_GRPC_TIMEOUT" env-default:"1s"`
	EnableFallback bool          `env:"ML_ENABLE_FALLBACK" env-default:"true"`
}

type DatasetConfig struct {
	ScheduleCSVPath    string `env:"SCHEDULE_CSV_PATH" env-required:"true"`
	UnitMappingCSVPath string `env:"UNIT_MAPPING_CSV_PATH" env-required:"true"`
}

var (
	cfg  *Config
	once sync.Once
)

// Get загружает и валидирует конфигурацию. При ошибке валидации возвращает понятную ошибку.
func Get() (*Config, error) {
	var err error
	once.Do(func() {
		var instance Config

		// 1. Пытаемся прочитать из .env файла (если есть на диске)
		if _, statErr := os.Stat(".env"); statErr == nil {
			err = cleanenv.ReadConfig(".env", &instance)
			if err == nil {
				// OS/container variables deliberately override local .env values.
				err = cleanenv.ReadEnv(&instance)
			}
		} else {
			// Иначе читаем чистые переменные окружения контейнера/ОС
			err = cleanenv.ReadEnv(&instance)
		}

		if err != nil {
			err = fmt.Errorf("configuration validation failed: %w", err)
			return
		}

		// 2. Дополнительная бизнес-валидация
		if validateErr := instance.Validate(); validateErr != nil {
			err = fmt.Errorf("configuration logic error: %w", validateErr)
			return
		}

		cfg = &instance
	})

	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate проверяет валидность портов и физическое наличие файлов на диске
func (c *Config) Validate() error {
	if c.HTTP.Port < 1 || c.HTTP.Port > 65535 {
		return fmt.Errorf("invalid HTTP_PORT: %d (must be 1..65535)", c.HTTP.Port)
	}
	if c.NDTP.TCPPort < 1 || c.NDTP.TCPPort > 65535 {
		return fmt.Errorf("invalid NDTP_TCP_PORT: %d (must be 1..65535)", c.NDTP.TCPPort)
	}
	if c.MLClient.Port < 1 || c.MLClient.Port > 65535 {
		return fmt.Errorf("invalid ML_GRPC_PORT: %d (must be 1..65535)", c.MLClient.Port)
	}
	if _, err := os.Stat(c.Dataset.UnitMappingCSVPath); os.IsNotExist(err) {
		return fmt.Errorf("UNIT_MAPPING_CSV_PATH file does not exist: %s", c.Dataset.UnitMappingCSVPath)
	}
	if _, err := time.LoadLocation(c.App.Timezone); err != nil {
		return fmt.Errorf("invalid TIMEZONE: %s (%w)", c.App.Timezone, err)
	}

	// Проверяем, существует ли файл расписания на диске прямо при старте!
	if _, err := os.Stat(c.Dataset.ScheduleCSVPath); os.IsNotExist(err) {
		return fmt.Errorf("SCHEDULE_CSV_PATH file does not exist: %s", c.Dataset.ScheduleCSVPath)
	}

	return nil
}
