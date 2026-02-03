# Настройка Kafka (Aiven) в Railway

## Переменные окружения для Kafka

Для подключения к Kafka в Aiven Cloud нужно добавить следующие переменные в Railway:

### 1. KAFKA_BROKERS
- **Name:** `KAFKA_BROKERS`
- **Value:** `kafka-32d812a9-andrejnikolaevich2016-52c9.k.aivencloud.com:21467`

### 2. KAFKA_USERNAME
- **Name:** `KAFKA_USERNAME`
- **Value:** `avnadmin`

### 3. KAFKA_PASSWORD
- **Name:** `KAFKA_PASSWORD`
- **Value:** `твой_пароль_из_переменных` (замените на реальный пароль из Aiven)

### 4. KAFKA_CA_CERT (опционально, но рекомендуется)
- **Name:** `KAFKA_CA_CERT`
- **Value:** Вставьте весь сертификат, включая `-----BEGIN CERTIFICATE-----` и `-----END CERTIFICATE-----`:

```
-----BEGIN CERTIFICATE-----
MIIERDCCAqygAwIBAgIUC1Xi2MRX2vGZn5A+wvyYzOpKzEEwDQYJKoZIhvcNAQEM
BQAwOjE4MDYGA1UEAwwvNWZhMTM1OTQtOWExZC00Zjk2LTk0YTQtNmQzYWEyMzM3
ZjBmIFByb2plY3QgQ0EwHhcNMjYwMjAzMDAwODE0WhcNMzYwMjAxMDAwODE0WjA6
MTgwNgYDVQQDDC81ZmExMzU5NC05YTFkLTRmOTYtOTRhNC02ZDNhYTIzMzdmMGYg
UHJvamVjdCBDQTCCAaIwDQYJKoZIhvcNAQEBBQADggGPADCCAYoCggGBAI8VtMer
CELYXbKZu0Ru7EmsuhWg6/9SXmn2ALg20H8rnI1kR8twxRuBriE9PrgRrnLA5sVS
vqI8hKD7ZpfReXiRtfFgBKbm618rcVptHGu2Bzi+Hg4qSOdDoA99Jk66O2C7WhAk
HmXLPlEhwFSFex5dSuGAhMiAg9O+W4v+KDT0ks0bDTeLGe+8dLx2H5cFKxDmbIde
AApB5w1SBjs8ZCwiRvKmc8YYyZ78M+hH27u0M0I7t0qXDFZTE9Vif/n8FqyJsMzs
Kr5UEgo07SAvt3QszrB5x+ZQRldEov/CzcNjQELR8WVDpz4KQkdLKn5ln7FYaRzX
Y2wBa/sGRV9McXtpF1P9va8X3l2MaNn+Hh7iEa3AI6UaddPJXa6i/Ya5Emdy/8Wa
e3JMjCYwG4vevRw5D8c9PWhJAbuedRImAaLCXhufSEAUUb+q9+tBheYmR7ec3YwS
J2Y4gSSkzIA+RFjxNrdZ90IC1MaZYfAEzp55ruAT5Ng6xFAR8OUqazbFTwIDAQAB
o0IwQDAdBgNVHQ4EFgQUJS9HAUOAPQ0qLrYc0OhUeJ4BAEgwEgYDVR0TAQH/BAgw
BgEB/wIBADALBgNVHQ8EBAMCAQYwDQYJKoZIhvcNAQEMBQADggGBAG2jm6S+ID4e
2ZSulstZlSf9u4fEtk1pc3l9z8DJyvkuPOKb/dgZDDN87lSeIpTByJktaBMVmgTT
1Y2auHmqj5GJRVHOMLumNdwol8tDxGUHN9r7U22IsHCW3kzIlnALjpA9DK7orfkL
Dc4owEtBAS9J6tY4aUisKgkzAB4CnD0fKZTEYkAbvnGsRFAR1TUwbNzBTNCY5UKv
m2YSgmmh7HraeW78GC+T7vwScEIEguy4Y4OPIdd3qa1zF7apXo0xqbceXX/rJrCu
8MedWrlaomlBp5ffpGVC+Mh7ueT4fhWEpyHTo8VdxuCL3oSRVibV00oDjqWvN7Pf
yAec+p8Fr4QMUnp4py+hFYh2KI4gNr5iwQjZaP+20v9W+JkzpDOXPRcoKUP+rYqK
nbUtd+tHBmtbrU/rsyy9tUB28yZxAhXGBTMy+6Fpc9e3opS0G0nWouc2SN178iwJ
QO2/uM60wdO06wUue6yNQDMtT7HyAvf8eKNO/+9BYZgpt/aOXx0/EQ==
-----END CERTIFICATE-----
```

## Как добавить переменные в Railway

1. Перейдите в ваш **Go сервис** в Railway Dashboard
2. Откройте вкладку **Variables**
3. Нажмите **+ New Variable** для каждой переменной
4. Добавьте все 4 переменные (или минимум 3, если не используете CA сертификат)

## Проверка после настройки

После добавления переменных и перезапуска сервиса в логах должно появиться:

```
📡 KAFKA_BROKERS установлен: kafka-32d812a9-andrejnikolaevich2016-52c9.k.aivencloud.com:21467
🔐 Kafka: SASL/PLAIN аутентификация включена (username: avnadmin)
🔒 Kafka: TLS включен (системные сертификаты)
✅ Kafka producer подключен к kafka-32d812a9-andrejnikolaevich2016-52c9.k.aivencloud.com:21467
📡 Kafka WS Consumer запущен: читает с FirstOffset, GroupID=kitchen-ws-group-v3
```

Вместо ошибок:
```
⚠️ Kafka WS Consumer ошибка чтения: failed to dial: failed to open connection to localhost:9092
```

## Важные замечания

1. **Пароль:** Замените `твой_пароль_из_переменных` на реальный пароль из вашего Aiven проекта
2. **CA сертификат:** Если не указать `KAFKA_CA_CERT`, будет использоваться `InsecureSkipVerify: false` с системными сертификатами. Для production лучше указать CA сертификат.
3. **Безопасность:** Не коммитьте пароли и сертификаты в Git!

## Где найти пароль в Aiven

1. Войдите в Aiven Console
2. Откройте ваш Kafka сервис
3. Перейдите в **Overview** → **Service URI**
4. Скопируйте пароль из строки подключения (после `:` в `avnadmin:password@`)

