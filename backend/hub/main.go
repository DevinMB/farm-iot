// FarmSense Hub Daemon
// Runs on Raspberry Pi — authenticates with the FarmSense API, sends
// heartbeats, and forwards LoRa sensor packets to InfluxDB.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"periph.io/x/host/v3"
)

// ── Config ────────────────────────────────────────────────────────────────────

type Config struct {
	HubID         string
	VaultAddr     string
	VaultRoleID   string
	VaultSecretID string
	APIURL        string

	// LoRa (optional — zero values mean LoRa disabled)
	LoRaEnabled bool
	LoRaSPIDev  string
	LoRaSPIHz   uint32
	LoRaReset   string
	LoRaBusy    string
	LoRaDIO1    string
	LoRaCSPin   string  // software CS GPIO; empty = rely on kernel hardware CE
	LoRaRXEnPin string  // board RXEN GPIO for RF switch; empty = not used
	LoRaFreqHz  uint32
	LoRaSF      byte
	LoRaBWCode  byte
	LoRaCR      byte
	LoRaTCXO    float64 // TCXO voltage; 0 = use crystal
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	m := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		m[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}

	cfg := &Config{
		HubID:         m["HUB_ID"],
		VaultAddr:     m["VAULT_ADDR"],
		VaultRoleID:   m["VAULT_ROLE_ID"],
		VaultSecretID: m["VAULT_SECRET_ID"],
		APIURL:        m["API_URL"],
	}
	if cfg.HubID == "" || cfg.VaultRoleID == "" || cfg.VaultSecretID == "" || cfg.APIURL == "" {
		return nil, fmt.Errorf("missing required config values in %s", path)
	}

	// LoRa config (optional)
	cfg.LoRaEnabled = strings.ToLower(m["LORA_ENABLED"]) == "true"
	if cfg.LoRaEnabled {
		cfg.LoRaSPIDev = orDefault(m["LORA_SPI_DEV"], "/dev/spidev0.0")
		cfg.LoRaSPIHz = parseUint32(m["LORA_SPI_HZ"], 2000000) // 2 MHz default
		cfg.LoRaReset = orDefault(m["LORA_RESET_PIN"], "18")
		cfg.LoRaBusy = orDefault(m["LORA_BUSY_PIN"], "20")
		cfg.LoRaDIO1 = orDefault(m["LORA_DIO1_PIN"], "16")
		cfg.LoRaCSPin = orDefault(m["LORA_CS_PIN"], "21")
		// Per Waveshare wiki + working Meshtastic config: don't drive BCM6 by default.
		// SX1262's DIO2 (configured as RF switch) is sufficient for the SPDT RF switch.
		// Setting LORA_RXEN_PIN explicitly (e.g. =6) re-enables manual driving.
		cfg.LoRaRXEnPin = m["LORA_RXEN_PIN"]
		cfg.LoRaSF = parseByte(m["LORA_SF"], 7)
		cfg.LoRaBWCode = parseByte(m["LORA_BW_CODE"], 4)   // SX1262 encoding: 0x04 = 125 kHz (datasheet table 13-47)
		cfg.LoRaCR = parseByte(m["LORA_CR"], 1)             // 0x01 = 4/5
		cfg.LoRaFreqHz = parseUint32(m["LORA_FREQ_HZ"], 915000000)
		cfg.LoRaTCXO = parseFloat64(m["LORA_TCXO_VOLTAGE"], 0) // 0 = crystal, 1.8 = common TCXO
	}

	return cfg, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func parseByte(s string, def byte) byte {
	if s == "" {
		return def
	}
	n, err := strconv.ParseUint(s, 10, 8)
	if err != nil {
		return def
	}
	return byte(n)
}

func parseUint32(s string, def uint32) uint32 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return def
	}
	return uint32(n)
}

func parseFloat64(s string, def float64) float64 {
	if s == "" {
		return def
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}

// ── API client ────────────────────────────────────────────────────────────────

type apiClient struct {
	baseURL    string
	httpClient *http.Client
	mu         sync.RWMutex
	token      string
	tokenExp   time.Time
}

func newAPIClient(baseURL string) *apiClient {
	return &apiClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *apiClient) setToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
	c.tokenExp = time.Now().Add(90 * time.Minute)
}

func (c *apiClient) getToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

func (c *apiClient) tokenExpired() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().After(c.tokenExp)
}

func (c *apiClient) auth(cfg *Config) error {
	body, _ := json.Marshal(map[string]string{
		"hub_id":    cfg.HubID,
		"role_id":   cfg.VaultRoleID,
		"secret_id": cfg.VaultSecretID,
	})

	resp, err := c.httpClient.Post(c.baseURL+"/api/hub/auth", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("auth failed (%d): %s", resp.StatusCode, string(b))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("auth decode: %w", err)
	}
	if result.Token == "" {
		return fmt.Errorf("auth: empty token in response")
	}

	c.setToken(result.Token)
	log.Printf("authenticated with API, token valid for ~90m")
	return nil
}

func (c *apiClient) post(path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.getToken())
	return c.httpClient.Do(req)
}

func (c *apiClient) get(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.getToken())
	return c.httpClient.Do(req)
}

// ── Reading types ─────────────────────────────────────────────────────────────

type reading struct {
	NodeID     string  `json:"node_id"`
	SensorType string  `json:"sensor_type"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	Timestamp  int64   `json:"timestamp"`
}

// loraPacket matches what the ESP32 firmware transmits over LoRa.
type loraPacket struct {
	NodeID     string  `json:"node_id"`
	SensorType string  `json:"sensor_type"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
}

func postReadings(client *apiClient, readings []reading) {
	body, _ := json.Marshal(map[string]interface{}{"readings": readings})
	resp, err := client.post("/api/hub/readings", body)
	if err != nil {
		log.Printf("readings post error: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		log.Printf("readings unexpected status: %d", resp.StatusCode)
	}
}

// ── Heartbeat loop ────────────────────────────────────────────────────────────

func heartbeatLoop(client *apiClient, cfg *Config) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if client.tokenExpired() {
			log.Printf("token expiring, re-authenticating...")
			if err := client.auth(cfg); err != nil {
				log.Printf("re-auth failed: %v", err)
				continue
			}
		}

		resp, err := client.post("/api/hub/heartbeat", nil)
		if err != nil {
			log.Printf("heartbeat error: %v", err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			log.Printf("heartbeat unexpected status: %d", resp.StatusCode)
		}
	}
}

// ── LoRa receive loop ─────────────────────────────────────────────────────────

func loraLoop(client *apiClient, cfg *Config) {
	radio, err := NewRadio(LoRaConfig{
		SPIDev:      cfg.LoRaSPIDev,
		SPIHz:       cfg.LoRaSPIHz,
		ResetPin:    cfg.LoRaReset,
		BusyPin:     cfg.LoRaBusy,
		DIO1Pin:     cfg.LoRaDIO1,
		CSPin:       cfg.LoRaCSPin,
		RXEnPin:     cfg.LoRaRXEnPin,
		FreqHz:      cfg.LoRaFreqHz,
		SF:          cfg.LoRaSF,
		BWCode:      cfg.LoRaBWCode,
		CR:          cfg.LoRaCR,
		TCXOVoltage: cfg.LoRaTCXO,
	})
	if err != nil {
		log.Printf("lora: init radio failed: %v — falling back to idle mode (is SPI enabled? try: sudo reboot)", err)
		idleLoop()
		return
	}
	defer radio.Close()

	if err := radio.Init(); err != nil {
		log.Printf("lora: configure radio failed: %v — falling back to idle mode", err)
		idleLoop()
		return
	}

	log.Printf("LoRa receiver ready — %d MHz SF%d BW%dkHz CR4/%d",
		cfg.LoRaFreqHz/1000000, cfg.LoRaSF, bwCodeToKHz(cfg.LoRaBWCode), cfg.LoRaCR+4)
	log.Printf("Listening for packets on %s (RESET=%s BUSY=%s DIO1=%s CS=%s)",
		cfg.LoRaSPIDev, cfg.LoRaReset, cfg.LoRaBusy, cfg.LoRaDIO1, cfg.LoRaCSPin)

	for {
		if client.tokenExpired() {
			time.Sleep(time.Second)
			continue
		}

		pkt, err := radio.Receive(30 * time.Second)
		if err != nil {
			log.Printf("lora: receive error: %v", err)
			continue
		}
		if pkt == nil {
			// Timeout — just loop back and call Receive again
			continue
		}

		log.Printf("lora: packet %d bytes  RSSI=%ddBm SNR=%ddB",
			len(pkt.Payload), pkt.RSSI, pkt.SNR)
		log.Printf("lora: payload: %s", string(pkt.Payload))

		var lp loraPacket
		if err := json.Unmarshal(pkt.Payload, &lp); err != nil {
			log.Printf("lora: invalid JSON payload: %v", err)
			continue
		}
		if lp.NodeID == "" || lp.SensorType == "" {
			log.Printf("lora: packet missing node_id or sensor_type, dropping")
			continue
		}

		postReadings(client, []reading{{
			NodeID:     lp.NodeID,
			SensorType: lp.SensorType,
			Value:      lp.Value,
			Unit:       lp.Unit,
			Timestamp:  time.Now().Unix(),
		}})
		log.Printf("lora: forwarded %s reading %.4f %s from node %s",
			lp.SensorType, lp.Value, lp.Unit, lp.NodeID[:8])
	}
}

func bwCodeToKHz(code byte) int {
	// SX1262 SetModulationParams BW encoding (datasheet table 13-47).
	// NOTE: This is NOT the same as SX1276/77/78/79 — values are non-contiguous.
	switch code {
	case 0x00:
		return 8 // 7.81 kHz
	case 0x08:
		return 10 // 10.42 kHz
	case 0x01:
		return 16 // 15.63 kHz
	case 0x09:
		return 21 // 20.83 kHz
	case 0x02:
		return 31 // 31.25 kHz
	case 0x0A:
		return 42 // 41.67 kHz
	case 0x03:
		return 63 // 62.50 kHz
	case 0x04:
		return 125 // 125.00 kHz
	case 0x05:
		return 250 // 250.00 kHz
	case 0x06:
		return 500 // 500.00 kHz
	default:
		return 125
	}
}

// ── Idle loop (no LoRa hardware) ──────────────────────────────────────────────
// Blocks forever with periodic reminders about LoRa being disabled.

func idleLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	log.Printf("LoRa disabled — hub is online and sending heartbeats only")
	log.Printf("To enable LoRa: set LORA_ENABLED=true in /etc/farmsense/config.env and restart")
	for range ticker.C {
		log.Printf("waiting — LORA_ENABLED is not set")
	}
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[farmsense-hub] ")

	configPath := "/etc/farmsense/config.env"
	if p := os.Getenv("FARMSENSE_CONFIG"); p != "" {
		configPath = p
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	log.Printf("hub ID: %s", cfg.HubID)
	log.Printf("API:    %s", cfg.APIURL)

	// Initialise periph.io hardware layer (required for SPI/GPIO on Pi)
	if _, err := host.Init(); err != nil {
		log.Printf("periph host init warning: %v (continuing)", err)
	}

	client := newAPIClient(cfg.APIURL)

	// Authenticate with retry
	for {
		if err := client.auth(cfg); err != nil {
			log.Printf("auth failed: %v — retrying in 10s", err)
			time.Sleep(10 * time.Second)
			continue
		}
		break
	}

	// Heartbeat always runs in background
	go heartbeatLoop(client, cfg)

	if cfg.LoRaEnabled {
		loraLoop(client, cfg) // blocks forever
	} else {
		idleLoop() // blocks forever
	}
}
