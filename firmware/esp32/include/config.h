#pragma once

// ── I2C (ADS1115) ─────────────────────────────────────────────────────────
#if defined(BOARD_HELTEC_V3)
  #define PIN_SDA  17
  #define PIN_SCL  18
#elif defined(BOARD_HELTEC_V2)
  #define PIN_SDA   4
  #define PIN_SCL  15
#elif defined(BOARD_TTGO_V1)
  #define PIN_SDA  21
  #define PIN_SCL  22
#else
  #define PIN_SDA  21
  #define PIN_SCL  22
#endif

// ADS1115 I2C address — ADDR pin tied to GND
#define ADS_I2C_ADDR  0x48
// Which ADS channel the potentiometer wiper is on
#define ADS_CHANNEL   0

// ── LoRa pins ─────────────────────────────────────────────────────────────
#if defined(BOARD_HELTEC_V3)
  // SX1262 on Heltec V3 (ESP32-S3) — non-default SPI bus
  #define LORA_CS    8
  #define LORA_RST  12
  #define LORA_DIO1 14
  #define LORA_BUSY 13
  #define LORA_SCK   9
  #define LORA_MOSI 10
  #define LORA_MISO 11
#elif defined(BOARD_HELTEC_V2)
  // SX1276 on Heltec V2 (ESP32)
  #define LORA_CS   18
  #define LORA_RST  14
  #define LORA_DIO0 26
#elif defined(BOARD_TTGO_V1)
  // SX1276 on TTGO V1 (ESP32)
  #define LORA_CS   18
  #define LORA_RST  23
  #define LORA_DIO0 26
#endif

// 915 MHz for US
#define LORA_FREQ_MHZ 915.0f
// Spreading factor 7, BW 125 kHz, CR 4/5 — must match Pi hub config
#define LORA_SF       7
#define LORA_BW       125.0f
#define LORA_CR       5

// ── Sensor defaults ───────────────────────────────────────────────────────
// Physical travel of the potentiometer in mm
#define DEFAULT_CAL_RANGE_MM   75.0f
// ADC count at 0 mm (wiper at GND end) — calibrated via serial at first boot
#define DEFAULT_CAL_ZERO       0
// ADC count at max mm (wiper at VCC end) — calibrated via serial at first boot
#define DEFAULT_CAL_MAX        26000

// How often to take and transmit a reading (ms)
#define DEFAULT_INTERVAL_MS    60000UL

// ── NTP (WiFi mode) ───────────────────────────────────────────────────────
#define NTP_SERVER    "pool.ntp.org"
#define NTP_OFFSET_S  0     // UTC; adjust for local timezone if desired
