package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL    string
	RedisURL       string
	KafkaBrokers   string
	KafkaUsername  string
	KafkaPassword  string
	KafkaCACert    string
	JWTSecret      string
	ServerPort     string
	Environment    string
	OpenVPNPath    string
	WireGuardPath  string
	// ¦à¦-¦-¦-TÇ¦¬¦¦ TÇ¦-TÁTË ¦¦TÃTÅ¦-¦¬ (¦- UTC, ¦¦¦¬¦¬¦¦¦-TÂ TÁ¦-¦- ¦¦¦-¦-¦-¦¦TÀTÂ¦¬TÀTÃ¦¦TÂ ¦- TÁ¦-¦-¦¦ TÇ¦-TÁ¦-¦-¦-¦¦ ¦¬¦-TÏTÁ)
	BusinessOpenHour  int // ¦ç¦-TÁ ¦-TÂ¦¦TÀTËTÂ¦¬TÏ ¦- UTC (¦¬¦- TÃ¦-¦-¦¬TÇ¦-¦-¦¬TÎ 2 = 9:00 KRAT)
	BusinessCloseHour int // ¦ç¦-TÁ ¦¬¦-¦¦TÀTËTÂ¦¬TÏ ¦- UTC (¦¬¦- TÃ¦-¦-¦¬TÇ¦-¦-¦¬TÎ 16 = 23:00 KRAT)
	BusinessCloseMin  int // ¦Ü¦¬¦-TÃTÂ¦- ¦¬¦-¦¦TÀTËTÂ¦¬TÏ ¦- UTC (¦¬¦- TÃ¦-¦-¦¬TÇ¦-¦-¦¬TÎ 45)
}

func Load() *Config {
	// Railway ¦-¦-¦¦¦¦TÂ ¦¬TÁ¦¬¦-¦¬TÌ¦¬¦-¦-¦-TÂTÌ TÀ¦-¦¬¦-TË¦¦ ¦¬¦-¦¦¦-¦- ¦¬¦¦TÀ¦¦¦-¦¦¦-¦-TËTÅ ¦+¦¬TÏ PostgreSQL
	// ¦ßTÀ¦-¦-¦¦TÀTÏ¦¦¦- ¦- ¦¬¦-TÀTÏ¦+¦¦¦¦ ¦¬TÀ¦¬¦-TÀ¦¬TÂ¦¦TÂ¦-: DATABASE_URL, POSTGRES_URL, PGDATABASE_URL, PGHOST (TÁ¦-¦-TÀ¦¦¦- ¦¬¦¬ TÇ¦-TÁTÂ¦¦¦¦)
	databaseURL := getEnv("DATABASE_URL", "")
	if databaseURL == "" {
		databaseURL = getEnv("POSTGRES_URL", "")
	}
	if databaseURL == "" {
		databaseURL = getEnv("PGDATABASE_URL", "")
	}
	// ¦ÕTÁ¦¬¦¬ ¦-¦¦TÂ ¦¬¦-¦¬¦-¦-¦¦¦- URL, ¦¬TËTÂ¦-¦¦¦-TÁTÏ TÁ¦-¦-TÀ¦-TÂTÌ ¦¬¦¬ ¦-TÂ¦+¦¦¦¬TÌ¦-TËTÅ ¦¬¦¦TÀ¦¦¦-¦¦¦-¦-TËTÅ (Railway ¦¬¦-¦-¦¦¦+¦- TÂ¦-¦¦ ¦+¦¦¦¬¦-¦¦TÂ)
	if databaseURL == "" {
		pgHost := getEnv("PGHOST", "")
		pgPort := getEnv("PGPORT", "5432")
		pgUser := getEnv("PGUSER", "postgres")
		pgPassword := getEnv("PGPASSWORD", "")
		pgDatabase := getEnv("PGDATABASE", "zephyrvpn")
		
		if pgHost != "" {
			if pgPassword != "" {
				databaseURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
					pgUser, pgPassword, pgHost, pgPort, pgDatabase)
			} else {
				databaseURL = fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable",
					pgUser, pgHost, pgPort, pgDatabase)
			}
		}
	}
	if databaseURL == "" {
		databaseURL = "postgres://user:password@localhost/zephyrvpn?sslmode=disable" // Fallback
	}

	// Railway ¦-¦-¦¦¦¦TÂ ¦¬TÁ¦¬¦-¦¬TÌ¦¬¦-¦-¦-TÂTÌ TÀ¦-¦¬¦-TË¦¦ ¦¬¦-¦¦¦-¦- ¦¬¦¦TÀ¦¦¦-¦¦¦-¦-TËTÅ ¦+¦¬TÏ Redis
	// ¦ßTÀ¦-¦-¦¦TÀTÏ¦¦¦- ¦- ¦¬¦-TÀTÏ¦+¦¦¦¦ ¦¬TÀ¦¬¦-TÀ¦¬TÂ¦¦TÂ¦-: REDIS_URL, REDISCLOUD_URL, REDISHOST (TÁ¦-¦-TÀ¦¦¦- ¦¬¦¬ TÇ¦-TÁTÂ¦¦¦¦)
	redisURL := getEnv("REDIS_URL", "")
	if redisURL == "" {
		redisURL = getEnv("REDISCLOUD_URL", "")
	}
	// ¦ÕTÁ¦¬¦¬ ¦-¦¦TÂ ¦¬¦-¦¬¦-¦-¦¦¦- URL, ¦¬TËTÂ¦-¦¦¦-TÁTÏ TÁ¦-¦-TÀ¦-TÂTÌ ¦¬¦¬ ¦-TÂ¦+¦¦¦¬TÌ¦-TËTÅ ¦¬¦¦TÀ¦¦¦-¦¦¦-¦-TËTÅ
	if redisURL == "" {
		redisHost := getEnv("REDISHOST", "")
		redisPort := getEnv("REDISPORT", "6379")
		redisPassword := getEnv("REDISPASSWORD", "")
		redisDB := getEnv("REDISDB", "0")
		
		if redisHost != "" {
			if redisPassword != "" {
				redisURL = fmt.Sprintf("redis://:%s@%s:%s/%s", redisPassword, redisHost, redisPort, redisDB)
			} else {
				redisURL = fmt.Sprintf("redis://%s:%s/%s", redisHost, redisPort, redisDB)
			}
		}
	}
	if redisURL == "" {
		redisURL = "redis://localhost:6379/0" // Fallback
	}

	return &Config{
		DatabaseURL:      databaseURL,
		RedisURL:         redisURL,
		KafkaBrokers:     getEnv("KAFKA_BROKERS", "localhost:9092"),
		KafkaUsername:    getEnv("KAFKA_USERNAME", ""),
		KafkaPassword:    getEnv("KAFKA_PASSWORD", ""),
		KafkaCACert:       getEnv("KAFKA_CA_CERT", "-----BEGIN CERTIFICATE-----
MIIEYTCCAsmgAwIBAgIUBkCaiQM1kL68DV5kQAlg6QlcYNIwDQYJKoZIhvcNAQEM
BQAwOjE4MDYGA1UEAwwvNWZhMTM1OTQtOWExZC00Zjk2LTk0YTQtNmQzYWEyMzM3
ZjBmIFByb2plY3QgQ0EwHhcNMjYwMjAzMDAwODUxWhcNMjgwNTAzMDAwODUxWjA/
MRcwFQYDVQQKDA5rYWZrYS0zMmQ4MTJhOTERMA8GA1UECwwIdTljOWY0YTYxETAP
BgNVBAMMCGF2bmFkbWluMIIBojANBgkqhkiG9w0BAQEFAAOCAY8AMIIBigKCAYEA
2t3PMsP256oD9L4ELffYqGB4pNHjc3qx3DwJlwSfjWtq122bdRSx18yLLr8kUxIE
u8b4VE3WDHqmEIR2Y9HiRtQwHrdONRAIUcjPCJo5M8r3DMqsU1ZEBoraiIPl7zfv
bacIPQ9T87MfcH0eZ+qRqM1n7u0bkwKisF7QY/AxaoQnZCjBsJvkTCfYaX39ocsY
R6c+hATenGQ1fu9ojO73IYFzdgFvHx5FWuShLNxQ2vNbLHCTNOgVzo6Qhc0e+9QI
RpDx2HKE6enCKLTZijlDJN2pUHxv+EXWrp1JUHFfemDCRPnGhJluNW2Ltp3APmNZ
HDEjSY7kscoxuwKlQzCFWO71VLRgfLUtcW+72MHo5QguOQO2OPSISx4rBEj4CCqe
PmdL95O94a0DcyBgbkd47JMgy2yAnGakx+MiDBFrzJRvhMo0hXQhLBYIEWwxsilf
jH08Ls0hfhVpOIfkxMeJqMWKm7snUAvglQWQ7UiVZ/CYWI2ic56oHj9xvt6vN8MJ
AgMBAAGjWjBYMB0GA1UdDgQWBBQh7rYuBxAOd4VTNU9OSkRSiKir4jAJBgNVHRME
AjAAMAsGA1UdDwQEAwIFoDAfBgNVHSMEGDAWgBQlL0cBQ4A9DSouthzQ6FR4ngEA
SDANBgkqhkiG9w0BAQwFAAOCAYEAAXJP6WdnMOJUneRKq96LI9bAleTq9tUnM6h/
tYIeYT1rrANXr1mcvIa8DCaBzmDWUrBGOfceCqIWV1RftLWxrdagmSW8M4/lJ01+
sQKp42zX7iOgN4Hw5VLRhVe3RMl/J/G06Z03dKCGy2Cr+XfIU/NlQlDkMnw/wQva
VnEm3PBNH7Beb04cA0DrQzUxLGrXY7zPehBvrVw61Ao79U2LDzYXjemFCFUPsgha
40RNicWQQ4Pcdaars4jfkbxQ+s41BY2SyPP8tBoh6mS5Z2hWJy22DrvSHwj+NUB9
8mqcjVw7k6N1bNxQD1+eSzC13UzjHcXVmo1tmqZLfnu9y0SdJRVe+8rlDNARsL+s
kzDW0YO5WHYtSntwkhyWDtd85VU4L3rVNGLlTCzeohoDyyzChFJ84AuGONzT++Zh
Lrou/aUcUIAM8MjB5k+T6b2Gz7oj99lrySy9qK/kzxzz5JiDnpGHYZIl4s+JkNB5
YVXr0+prz4PEQ4DQQlBdNMJBp9Bo
-----END CERTIFICATE-----"),
		JWTSecret:        getEnv("JWT_SECRET", "your-secret-key-change-in-production"),
		ServerPort:       getEnv("PORT", "8080"),
		Environment:      getEnv("ENV", "development"),
		OpenVPNPath:      getEnv("OPENVPN_PATH", "/usr/sbin/openvpn"),
		WireGuardPath:    getEnv("WIREGUARD_PATH", "/usr/bin/wg"),
		BusinessOpenHour: getEnvInt("BUSINESS_OPEN_HOUR", 2),   // 2:00 UTC = 9:00 KRAT (UTC+7)
		BusinessCloseHour: getEnvInt("BUSINESS_CLOSE_HOUR", 16), // 16:00 UTC = 23:00 KRAT (UTC+7)
		BusinessCloseMin:  getEnvInt("BUSINESS_CLOSE_MIN", 45),   // 16:45 UTC = 23:45 KRAT (UTC+7)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

