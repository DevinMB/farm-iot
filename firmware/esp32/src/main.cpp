#include <Arduino.h>
#include <Preferences.h>
#include <ArduinoJson.h>
#include "config.h"

#ifndef TEST_MODE
#include <Wire.h>
#include <Adafruit_ADS1X15.h>
#endif

#ifdef WIFI_MODE
#include <WiFi.h>
#include <HTTPClient.h>
#include <NTPClient.h>
#include <WiFiUDP.h>
#endif

#ifdef LORA_MODE
#include <RadioLib.h>
#endif

// ── Persistent config (stored in NVS flash) ───────────────────────────────
Preferences prefs;

struct NodeConfig {
    String nodeId;
    int32_t calZero;
    int32_t calMax;
    float   calRangeMm;
    uint32_t intervalMs;
#ifdef WIFI_MODE
    String wifiSsid;
    String wifiPass;
    String apiUrl;
    String hubJwt;
#endif
};

NodeConfig cfg;

void loadConfig() {
    prefs.begin("farmsense", true);
    cfg.nodeId      = prefs.getString("node_id", "");
    cfg.calZero     = prefs.getInt("cal_zero",   DEFAULT_CAL_ZERO);
    cfg.calMax      = prefs.getInt("cal_max",    DEFAULT_CAL_MAX);
    cfg.calRangeMm  = prefs.getFloat("cal_range", DEFAULT_CAL_RANGE_MM);
#ifdef TEST_MODE
    cfg.intervalMs  = prefs.getUInt("interval",  5000UL);  // 5s default in test mode
#else
    cfg.intervalMs  = prefs.getUInt("interval",  DEFAULT_INTERVAL_MS);
#endif
#ifdef WIFI_MODE
    cfg.wifiSsid = prefs.getString("wifi_ssid", "");
    cfg.wifiPass = prefs.getString("wifi_pass", "");
    cfg.apiUrl   = prefs.getString("api_url",   "");
    cfg.hubJwt   = prefs.getString("hub_jwt",   "");
#endif
    prefs.end();
}

void saveConfig() {
    prefs.begin("farmsense", false);
    prefs.putString("node_id",   cfg.nodeId);
    prefs.putInt("cal_zero",     cfg.calZero);
    prefs.putInt("cal_max",      cfg.calMax);
    prefs.putFloat("cal_range",  cfg.calRangeMm);
    prefs.putUInt("interval",    cfg.intervalMs);
#ifdef WIFI_MODE
    prefs.putString("wifi_ssid", cfg.wifiSsid);
    prefs.putString("wifi_pass", cfg.wifiPass);
    prefs.putString("api_url",   cfg.apiUrl);
    prefs.putString("hub_jwt",   cfg.hubJwt);
#endif
    prefs.end();
}

// ── ADS1115 ───────────────────────────────────────────────────────────────
#ifndef TEST_MODE
Adafruit_ADS1115 ads;

bool initADS() {
    Wire.begin(PIN_SDA, PIN_SCL);
    ads.setGain(GAIN_ONE);  // ±4.096V FSR — covers full 3.3V pot range
    return ads.begin(ADS_I2C_ADDR);
}

int32_t readRaw() {
    return (int32_t)ads.readADC_SingleEnded(ADS_CHANNEL);
}

float rawToMm(int32_t raw) {
    int32_t span = cfg.calMax - cfg.calZero;
    if (span == 0) return 0.0f;
    float mm = (float)(raw - cfg.calZero) * (cfg.calRangeMm / (float)span);
    if (mm < 0.0f)             mm = 0.0f;
    if (mm > cfg.calRangeMm)   mm = cfg.calRangeMm;
    return mm;
}
#else
// Fake sensor: slow sine wave 20–40 mm, period ~2 minutes
float fakeReading() {
    float t = millis() / 60000.0f;  // minutes
    return 30.0f + 10.0f * sin(t * 2.0f * PI);
}
#endif

// ── WiFi / HTTP mode ──────────────────────────────────────────────────────
#ifdef WIFI_MODE
WiFiUDP  ntpUDP;
NTPClient ntp(ntpUDP, NTP_SERVER, NTP_OFFSET_S, 60000);

bool wifiConnected = false;

void connectWiFi() {
    if (cfg.wifiSsid.isEmpty()) return;
    Serial.printf("[WiFi] connecting to %s...\n", cfg.wifiSsid.c_str());
    WiFi.begin(cfg.wifiSsid.c_str(), cfg.wifiPass.c_str());
    uint8_t tries = 0;
    while (WiFi.status() != WL_CONNECTED && tries < 20) {
        delay(500);
        Serial.print(".");
        tries++;
    }
    if (WiFi.status() == WL_CONNECTED) {
        Serial.printf("\n[WiFi] connected, IP: %s\n", WiFi.localIP().toString().c_str());
        ntp.begin();
        ntp.update();
        wifiConnected = true;
    } else {
        Serial.println("\n[WiFi] connection failed");
    }
}

void sendReading(float mm) {
    if (!wifiConnected || cfg.apiUrl.isEmpty() || cfg.hubJwt.isEmpty()) return;
    if (WiFi.status() != WL_CONNECTED) {
        connectWiFi();
        if (!wifiConnected) return;
    }

    ntp.update();
    unsigned long ts = ntp.getEpochTime();

    JsonDocument doc;
    JsonArray readings = doc["readings"].to<JsonArray>();
    JsonObject r = readings.add<JsonObject>();
    r["node_id"]     = cfg.nodeId;
    r["sensor_type"] = "dendrometer";
    r["value"]       = mm;
    r["unit"]        = "mm";
    r["timestamp"]   = ts;

    String body;
    serializeJson(doc, body);

    HTTPClient http;
    http.begin(cfg.apiUrl + "/api/hub/readings");
    http.addHeader("Content-Type", "application/json");
    http.addHeader("Authorization", "Bearer " + cfg.hubJwt);

    int code = http.POST(body);
    Serial.printf("[HTTP] POST /api/hub/readings → %d\n", code);
    http.end();
}
#endif  // WIFI_MODE

// ── LoRa mode ─────────────────────────────────────────────────────────────
#ifdef LORA_MODE

#if defined(BOARD_HELTEC_V3)
  // Heltec V3: SX1262, explicit SPI bus (non-default pins on ESP32-S3)
  static SX1262 lora = new Module(LORA_CS, LORA_DIO1, LORA_RST, LORA_BUSY);
#else
  // Heltec V2 / TTGO V1: SX1276, default SPI bus
  static SX1276 lora = new Module(LORA_CS, LORA_DIO0, LORA_RST, RADIOLIB_NC);
#endif

bool initLoRa() {
#if defined(BOARD_HELTEC_V3)
    SPI.begin(LORA_SCK, LORA_MISO, LORA_MOSI, LORA_CS);
    // 1.8V TCXO on DIO3 — required for V3
    int state = lora.begin(LORA_FREQ_MHZ, LORA_BW, LORA_SF, LORA_CR,
                           0x12, 22, 8, 1.8f);  // 22 dBm = max for SX1262
#else
    int state = lora.begin(LORA_FREQ_MHZ, LORA_BW, LORA_SF, LORA_CR);
#endif
    if (state != RADIOLIB_ERR_NONE) {
        Serial.printf("[LoRa] init failed: %d\n", state);
        return false;
    }

    Serial.printf("[LoRa] ready — %.0f MHz SF%d BW%.0fkHz CR4/%d 22dBm preamble=8\n",
        LORA_FREQ_MHZ, LORA_SF, LORA_BW, LORA_CR);
    return true;
}

// Compact JSON payload — hub daemon parses and timestamps on receipt
void sendReading(float mm) {
    if (cfg.nodeId.isEmpty()) return;

    char payload[128];
    snprintf(payload, sizeof(payload),
        "{\"node_id\":\"%s\",\"sensor_type\":\"dendrometer\",\"value\":%.3f,\"unit\":\"mm\"}",
        cfg.nodeId.c_str(), mm);

    int state = lora.transmit((uint8_t*)payload, strlen(payload));
    if (state == RADIOLIB_ERR_NONE) {
        Serial.printf("[LoRa] sent: %s\n", payload);
    } else {
        Serial.printf("[LoRa] send error: %d\n", state);
    }
}
#endif  // LORA_MODE

// ── Serial config shell ───────────────────────────────────────────────────
// Enter by sending any character within 3s of boot, or if node_id is blank.
// Commands:
//   show                      — print current config
//   set node_id <uuid>        — node UUID from the web UI
//   cal zero                  — record current ADC count as 0 mm
//   cal max                   — record current ADC count as 75 mm (or your range)
//   cal range <mm>            — set physical travel distance
//   set interval <seconds>    — reading interval (default 60)
//   set wifi_ssid <ssid>      — (WiFi mode only)
//   set wifi_pass <pass>      — (WiFi mode only)
//   set api_url <url>         — (WiFi mode only, e.g. http://api.farmiot.yoursite.net)
//   set hub_jwt <token>       — (WiFi mode only)
//   start                     — save and reboot into normal operation

bool configMode = false;

void printConfig() {
    Serial.println("──────────────────────────────");
    Serial.printf("node_id   : %s\n", cfg.nodeId.isEmpty() ? "(not set)" : cfg.nodeId.c_str());
    Serial.printf("cal_zero  : %d\n", cfg.calZero);
    Serial.printf("cal_max   : %d\n", cfg.calMax);
    Serial.printf("cal_range : %.1f mm\n", cfg.calRangeMm);
    Serial.printf("interval  : %lu s\n", cfg.intervalMs / 1000UL);
#ifdef WIFI_MODE
    Serial.printf("wifi_ssid : %s\n", cfg.wifiSsid.isEmpty() ? "(not set)" : cfg.wifiSsid.c_str());
    Serial.printf("wifi_pass : %s\n", cfg.wifiPass.isEmpty() ? "(not set)" : "***");
    Serial.printf("api_url   : %s\n", cfg.apiUrl.isEmpty() ? "(not set)" : cfg.apiUrl.c_str());
    Serial.printf("hub_jwt   : %s\n", cfg.hubJwt.isEmpty() ? "(not set)" : "<set>");
#endif
#ifdef LORA_MODE
    Serial.println("mode      : LoRa 915MHz");
#else
    Serial.println("mode      : WiFi HTTP");
#endif
    Serial.println("──────────────────────────────");
}

void handleSerialLine(String line) {
    line.trim();
    if (line.isEmpty()) return;

    if (line == "show") {
        printConfig();
        return;
    }

    if (line == "cal zero") {
#ifdef TEST_MODE
        Serial.println("[cal] not available in TEST_MODE");
#else
        int32_t raw = readRaw();
        cfg.calZero = raw;
        Serial.printf("[cal] zero = %d (slide fully compressed)\n", raw);
#endif
        return;
    }

    if (line == "cal max") {
#ifdef TEST_MODE
        Serial.println("[cal] not available in TEST_MODE");
#else
        int32_t raw = readRaw();
        cfg.calMax = raw;
        Serial.printf("[cal] max = %d (slide fully extended)\n", raw);
#endif
        return;
    }

    if (line.startsWith("cal range ")) {
        cfg.calRangeMm = line.substring(10).toFloat();
        Serial.printf("[cal] range = %.1f mm\n", cfg.calRangeMm);
        return;
    }

    if (line.startsWith("set ")) {
        String rest = line.substring(4);
        int sp = rest.indexOf(' ');
        if (sp < 0) { Serial.println("usage: set <key> <value>"); return; }
        String key = rest.substring(0, sp);
        String val = rest.substring(sp + 1);

        if (key == "node_id")    { cfg.nodeId = val; Serial.printf("[set] node_id = %s\n", val.c_str()); }
        else if (key == "interval") { cfg.intervalMs = val.toInt() * 1000UL; Serial.printf("[set] interval = %s s\n", val.c_str()); }
#ifdef WIFI_MODE
        else if (key == "wifi_ssid") { cfg.wifiSsid = val; Serial.println("[set] wifi_ssid ok"); }
        else if (key == "wifi_pass") { cfg.wifiPass = val; Serial.println("[set] wifi_pass ok"); }
        else if (key == "api_url")   { cfg.apiUrl   = val; Serial.printf("[set] api_url = %s\n", val.c_str()); }
        else if (key == "hub_jwt")   { cfg.hubJwt   = val; Serial.println("[set] hub_jwt ok"); }
#endif
        else { Serial.printf("unknown key: %s\n", key.c_str()); }
        return;
    }

    if (line == "start") {
        saveConfig();
        Serial.println("[config] saved — rebooting...");
        delay(500);
        ESP.restart();
        return;
    }

    if (line == "read") {
#ifdef TEST_MODE
        Serial.printf("[TEST] fake reading: %.3f mm\n", fakeReading());
#else
        int32_t raw = readRaw();
        float mm = rawToMm(raw);
        Serial.printf("[sensor] raw=%d  mm=%.3f\n", raw, mm);
#endif
        return;
    }

    Serial.printf("unknown command: %s\n", line.c_str());
}

// ── setup / loop ──────────────────────────────────────────────────────────
void setup() {
    Serial.begin(115200);
    delay(200);

    loadConfig();

#ifdef TEST_MODE
    Serial.println("[TEST MODE] fake sensor data — no ADS1115 needed");
#else
    if (!initADS()) {
        Serial.println("[ERROR] ADS1115 not found — check wiring (SDA/SCL, 3.3V, ADDR pin)");
    }
#endif

    // Enter config mode if node_id is blank or user sends a character within 3s
    if (cfg.nodeId.isEmpty()) {
        configMode = true;
        Serial.println("\n=== FarmSense ESP32 — First Boot Config ===");
        Serial.println("Type 'show' to see current config, 'set node_id <uuid>' to configure.");
        Serial.println("Get your Node UUID from the hub detail page in the web UI.");
        printConfig();
    } else {
        Serial.println("\n=== FarmSense ESP32 ===");
        Serial.println("Send any character within 3 seconds to enter config mode...");
        uint32_t deadline = millis() + 3000;
        while (millis() < deadline) {
            if (Serial.available()) {
                Serial.read();
                configMode = true;
                Serial.println("\n[config] entering config mode");
                printConfig();
                break;
            }
        }
    }


    if (!configMode) {
#ifdef WIFI_MODE
        connectWiFi();
#endif
#ifdef LORA_MODE
        if (!initLoRa()) {
            Serial.println("[ERROR] LoRa init failed — check wiring");
        }
#endif
        Serial.printf("[run] node_id=%s  interval=%lus\n",
            cfg.nodeId.c_str(), cfg.intervalMs / 1000UL);
    }
}

static String serialBuf;
static uint32_t lastReadAt = 0;

void loop() {
    // Serial input (always active)
    while (Serial.available()) {
        char c = (char)Serial.read();
        if (c == '\n' || c == '\r') {
            if (serialBuf.length() > 0) {
                handleSerialLine(serialBuf);
                serialBuf = "";
            }
        } else {
            serialBuf += c;
        }
    }

    if (configMode) return;

    uint32_t now = millis();
    if (now - lastReadAt >= cfg.intervalMs) {
        lastReadAt = now;
#ifdef TEST_MODE
        float mm = fakeReading();
        Serial.printf("[TEST] fake reading: %.3f mm\n", mm);
#else
        int32_t raw = readRaw();
        float mm    = rawToMm(raw);
        Serial.printf("[sensor] raw=%d  mm=%.3f\n", raw, mm);
#endif
        sendReading(mm);
    }
}
