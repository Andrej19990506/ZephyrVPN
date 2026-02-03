package api

import (
	"crypto/tls"
	"crypto/x509"
	"log"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
)

// CreateKafkaDialer создает dialer для Kafka с поддержкой SASL/PLAIN и TLS (для Aiven)
func CreateKafkaDialer(username, password, caCert string) *kafka.Dialer {
	dialer := &kafka.Dialer{
		Timeout:       10 * time.Second,
		DualStack:     true,
	}

	// Если указаны username и password, используем SASL/PLAIN
	if username != "" && password != "" {
		mechanism := plain.Mechanism{
			Username: username,
			Password: password,
		}
		dialer.SASLMechanism = mechanism
		log.Printf("🔐 Kafka: SASL/PLAIN аутентификация включена (username: %s)", username)
	}

	// Настраиваем TLS
	tlsConfig := &tls.Config{
		InsecureSkipVerify: false, // По умолчанию проверяем сертификат
	}

	// Если указан CA сертификат, добавляем его в pool
	if caCert != "" {
		caCertPool := x509.NewCertPool()
		if ok := caCertPool.AppendCertsFromPEM([]byte(caCert)); ok {
			tlsConfig.RootCAs = caCertPool
			log.Printf("🔒 Kafka: TLS с CA сертификатом включен")
		} else {
			log.Printf("⚠️ Kafka: не удалось распарсить CA сертификат, используем системные сертификаты")
		}
	} else {
		// Если CA сертификат не указан, но нужен TLS (есть username/password), используем системные сертификаты
		if username != "" && password != "" {
			tlsConfig.RootCAs = nil // Используем системные сертификаты
			log.Printf("🔒 Kafka: TLS включен (системные сертификаты)")
		}
	}

	// Если есть SASL, всегда включаем TLS (Aiven требует TLS для SASL)
	// Также включаем TLS если указан CA сертификат
	if dialer.SASLMechanism != nil || caCert != "" {
		dialer.TLS = tlsConfig
		// Если есть SASL, но нет CA сертификата, используем системные сертификаты
		if dialer.SASLMechanism != nil && caCert == "" {
			tlsConfig.RootCAs = nil // Используем системные сертификаты
		}
	}

	return dialer
}

// ParseKafkaBrokers парсит строку с брокерами (может быть через запятую)
func ParseKafkaBrokers(brokers string) []string {
	if brokers == "" {
		return []string{}
	}
	// Убираем пробелы и разбиваем по запятой
	brokerList := strings.Split(strings.ReplaceAll(brokers, " ", ""), ",")
	var result []string
	for _, broker := range brokerList {
		if broker != "" {
			result = append(result, broker)
		}
	}
	return result
}

