package main

import (
	"fmt"
	"log"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
)

// SX1262 command opcodes
const (
	cmdSetStandby          byte = 0x80
	cmdSetRx               byte = 0x82
	cmdSetPacketType       byte = 0x8A // SX1262 datasheet table 11-2 (was 0x01 — wrong)
	cmdSetRfFrequency      byte = 0x86
	cmdSetModulationParams byte = 0x8B
	cmdSetPacketParams     byte = 0x8C
	cmdSetBufferBaseAddr   byte = 0x8F
	cmdSetDioIrqParams     byte = 0x08
	cmdGetIrqStatus        byte = 0x12
	cmdClearIrqStatus      byte = 0x02
	cmdGetRxBufferStatus   byte = 0x13
	cmdReadBuffer          byte = 0x1E
	cmdGetPacketStatus     byte = 0x14
	cmdSetDio2RfSwitch     byte = 0x9D
	cmdSetDio3AsTcxo       byte = 0x97
	cmdCalibrate           byte = 0x89
	cmdCalibrateImage      byte = 0x98
	cmdGetStatus           byte = 0xC0
	cmdReadRegister        byte = 0x1D
	cmdGetDeviceErrors     byte = 0x17
	cmdClearDeviceErrors   byte = 0x07
	cmdGetPacketType       byte = 0x11
)

// IRQ flags (SX1262 datasheet table 13-29). RX-only daemon — TX/CAD flags omitted.
const (
	irqRxDone      uint16 = 0x0002
	irqPreambleDet uint16 = 0x0004
	irqHeaderValid uint16 = 0x0010
	irqHeaderErr   uint16 = 0x0020
	irqCrcErr      uint16 = 0x0040
	irqTimeout     uint16 = 0x0200
)

// LoRaConfig holds LoRa radio configuration.
type LoRaConfig struct {
	SPIDev       string  // e.g. /dev/spidev0.0
	SPIHz        uint32  // SPI clock speed in Hz, e.g. 2000000 for 2 MHz
	ResetPin     string  // BCM pin number as string, e.g. "22"
	BusyPin      string  // e.g. "23"
	DIO1Pin      string  // e.g. "6"
	FreqHz       uint32  // e.g. 915000000
	SF           byte    // spreading factor 5-12
	BWCode       byte    // 0x04 = 125kHz, 0x05 = 250kHz, 0x06 = 500kHz
	CR           byte    // 0x01 = 4/5
	TCXOVoltage  float64 // TCXO voltage on DIO3 (0 = disabled, use crystal; 1.6/1.7/1.8/2.2/2.4/2.7/3.0/3.3)
	CSPin        string  // software chip-select GPIO (e.g. "21" for Waveshare); empty = use hardware CE
	RXEnPin      string  // board RXEN GPIO (e.g. "6" for Waveshare); empty = not used
}

// Radio wraps the SX1262 hardware.
type Radio struct {
	bus   spireg.Ref
	conn  spi.Conn
	cs    gpio.PinOut // software chip-select (nil = rely on kernel hardware CE)
	rxen  gpio.PinOut // board RXEN for RF switch (nil = not used)
	reset gpio.PinOut
	busy  gpio.PinIn
	dio1  gpio.PinIn
	cfg   LoRaConfig
}

// NewRadio opens the SPI device and GPIO pins.
func NewRadio(cfg LoRaConfig) (*Radio, error) {
	resetPin := gpioreg.ByName(cfg.ResetPin)
	if resetPin == nil {
		return nil, fmt.Errorf("lora: reset pin %q not found", cfg.ResetPin)
	}
	busyPin := gpioreg.ByName(cfg.BusyPin)
	if busyPin == nil {
		return nil, fmt.Errorf("lora: busy pin %q not found", cfg.BusyPin)
	}
	dio1Pin := gpioreg.ByName(cfg.DIO1Pin)
	if dio1Pin == nil {
		return nil, fmt.Errorf("lora: dio1 pin %q not found", cfg.DIO1Pin)
	}

	resetOut, ok := resetPin.(gpio.PinOut)
	if !ok {
		return nil, fmt.Errorf("lora: reset pin is not an output")
	}
	busyIn, ok := busyPin.(gpio.PinIn)
	if !ok {
		return nil, fmt.Errorf("lora: busy pin is not an input")
	}
	dio1In, ok := dio1Pin.(gpio.PinIn)
	if !ok {
		return nil, fmt.Errorf("lora: dio1 pin is not an input")
	}

	if err := busyIn.In(gpio.PullNoChange, gpio.NoEdge); err != nil {
		return nil, fmt.Errorf("lora: busy pin input: %w", err)
	}
	// NoEdge: sysfs edge-detection is deprecated on Bookworm (kernel 6.x).
	// We poll the pin in a tight loop instead of using WaitForEdge.
	if err := dio1In.In(gpio.PullDown, gpio.NoEdge); err != nil {
		return nil, fmt.Errorf("lora: dio1 pin input: %w", err)
	}

	busRef, err := spireg.Open(cfg.SPIDev)
	if err != nil {
		return nil, fmt.Errorf("lora: spi open %s: %w", cfg.SPIDev, err)
	}
	spiHz := physic.Frequency(cfg.SPIHz) * physic.Hertz
	if spiHz == 0 {
		spiHz = 2 * physic.MegaHertz
	}
	conn, err := busRef.Connect(spiHz, spi.Mode0, 8)
	if err != nil {
		busRef.Close()
		return nil, fmt.Errorf("lora: spi connect: %w", err)
	}

	r := &Radio{
		conn:  conn,
		reset: resetOut,
		busy:  busyIn,
		dio1:  dio1In,
		cfg:   cfg,
	}

	if cfg.CSPin != "" {
		csPin := gpioreg.ByName(cfg.CSPin)
		if csPin == nil {
			conn.(interface{ Close() error }).Close() //nolint:errcheck
			busRef.Close()
			return nil, fmt.Errorf("lora: cs pin %q not found", cfg.CSPin)
		}
		csOut, ok := csPin.(gpio.PinOut)
		if !ok {
			return nil, fmt.Errorf("lora: cs pin is not an output")
		}
		csOut.Out(gpio.High) // deassert CS initially
		r.cs = csOut
		log.Printf("lora: software CS on GPIO%s", cfg.CSPin)
	}

	if cfg.RXEnPin != "" {
		rxenPin := gpioreg.ByName(cfg.RXEnPin)
		if rxenPin == nil {
			return nil, fmt.Errorf("lora: rxen pin %q not found", cfg.RXEnPin)
		}
		rxenOut, ok := rxenPin.(gpio.PinOut)
		if !ok {
			return nil, fmt.Errorf("lora: rxen pin is not an output")
		}
		rxenOut.Out(gpio.Low) // RF switch idle until we enter RX
		r.rxen = rxenOut
		log.Printf("lora: RXEN on GPIO%s", cfg.RXEnPin)
	}

	return r, nil
}

// Close releases hardware resources.
func (r *Radio) Close() {
	if c, ok := r.conn.(interface{ Close() error }); ok {
		c.Close()
	}
}

// Init resets the chip and configures it for LoRa receive.
func (r *Radio) Init() error {
	// Hardware reset
	if err := r.reset.Out(gpio.Low); err != nil {
		return fmt.Errorf("lora: reset low: %w", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := r.reset.Out(gpio.High); err != nil {
		return fmt.Errorf("lora: reset high: %w", err)
	}
	// BUSY should go HIGH within ~1ms after reset and stay HIGH for ~10ms.
	// If it never goes HIGH, the BUSY pin is likely not connected.
	time.Sleep(1 * time.Millisecond)
	busyHigh := r.busy.Read() == gpio.High
	log.Printf("lora: 1ms post-reset BUSY=%v (expect High — if Low, BUSY pin may be unwired)", r.busy.Read())
	time.Sleep(19 * time.Millisecond) // total 20ms post-reset before sending commands
	r.waitBusy()
	if !busyHigh {
		log.Printf("lora: WARNING — BUSY never went High; all SPI reads may return garbage")
	}

	// Standby in RC oscillator mode
	r.sendCmd(cmdSetStandby, 0x00)
	r.waitBusy()
	log.Printf("lora: standby, status=0x%02X BUSY=%v", r.readStatus(), r.busy.Read())

	if r.cfg.TCXOVoltage > 0 {
		// HAT has a TCXO on DIO3. Enable it, wait for it to stabilise, then
		// recalibrate all blocks so the PLL uses the TCXO as its reference.
		// Without this the chip stays in FS mode (PLL can't lock) and never RXs.
		voltCode := tcxoVoltageCode(r.cfg.TCXOVoltage)
		// timeout = 320 × 15.625µs = 5ms
		r.sendCmd(cmdSetDio3AsTcxo, voltCode, 0x00, 0x01, 0x40)
		time.Sleep(5 * time.Millisecond)
		r.waitBusy()
		log.Printf("lora: TCXO (DIO3 %.1fV code=0x%02X) enabled", r.cfg.TCXOVoltage, voltCode)

		r.sendCmd(cmdCalibrate, 0x7F)
		time.Sleep(10 * time.Millisecond)
		r.waitBusy()
		log.Printf("lora: calibration complete")

		r.sendCmd(cmdSetStandby, 0x00)
		r.waitBusy()
	} else {
		log.Printf("lora: TCXO disabled — using crystal reference")
	}

	// DIO2 controls the on-chip TXEN signal — RadioLib enables this on every begin()
	// regardless of board, and at least some Waveshare modules need it to route the
	// antenna correctly even in pure RX use.
	r.sendCmd(cmdSetDio2RfSwitch, 0x01)
	r.waitBusy()

	// LoRa packet type
	r.sendCmd(cmdSetPacketType, 0x01)
	r.waitBusy()

	// Explicitly set standard IQ polarity (RadioLib does this; without it the
	// chip can be left in inverted-IQ mode from a prior run and silently drop packets).
	r.writeRegister(0x0736, 0x0D)

	// Boosted RX gain (~6 dB more sensitivity). Default after reset is 0x94 (power-saving).
	r.writeRegister(0x08AC, 0x96)

	// RF frequency: frf = freqHz * 2^25 / 32000000 (32-bit value, MSB first).
	// SX1262 SetRfFrequency takes 4 FRF bytes — dropping the MSB leaves the chip
	// tuned to the wrong band. For 915 MHz the correct FRF is 0x39300000;
	// without the MSB the chip ends up at ~768 MHz and never sees any signal.
	frf := uint32(uint64(r.cfg.FreqHz) * (1 << 25) / 32000000)
	r.sendCmd(cmdSetRfFrequency, byte(frf>>24), byte(frf>>16), byte(frf>>8), byte(frf))
	r.waitBusy()

	// Image calibration for the operating band (SX1262 datasheet table 9-2).
	// REQUIRED after each power-on reset for proper RX image rejection — without
	// this the receiver is functional in name only and detects nothing.
	imgF1, imgF2 := imageCalParams(r.cfg.FreqHz)
	r.sendCmd(cmdCalibrateImage, imgF1, imgF2)
	time.Sleep(10 * time.Millisecond)
	r.waitBusy()
	log.Printf("lora: image calibrated for %d MHz band (params=0x%02X 0x%02X)", r.cfg.FreqHz/1000000, imgF1, imgF2)

	// Buffer base addresses (TX=0x00, RX=0x00)
	r.sendCmd(cmdSetBufferBaseAddr, 0x00, 0x00)
	r.waitBusy()

	// Modulation: SF, BW code, CR, LDRO (auto-enable when symbol time > 16ms,
	// per Semtech recommendation. Without this, SF11+ at BW125 or SF12 at BW250
	// will not decode even when both sides claim matching parameters).
	ldro := byte(0)
	if symbolTimeMs(r.cfg.SF, r.cfg.BWCode) > 16.0 {
		ldro = 1
	}
	r.sendCmd(cmdSetModulationParams, r.cfg.SF, r.cfg.BWCode, r.cfg.CR, ldro)
	r.waitBusy()
	log.Printf("lora: modulation SF%d BW%dkHz CR4/%d LDRO=%d (symbol=%.2fms)",
		r.cfg.SF, bwCodeToKHz(r.cfg.BWCode), r.cfg.CR+4, ldro, symbolTimeMs(r.cfg.SF, r.cfg.BWCode))

	// Packet: preamble=8 (matches ESP32 RadioLib default), explicit header,
	// max payload=255, CRC on, IQ standard
	r.sendCmd(cmdSetPacketParams, 0x00, 0x08, 0x00, 0xFF, 0x01, 0x00)
	r.waitBusy()

	// IRQ mask on DIO1: enable EVERY relevant flag so the IRQ status register
	// reflects all stages of the demodulation pipeline. This lets us see whether
	// a preamble was detected, whether the header validated, etc.
	irqMask := irqRxDone | irqPreambleDet | irqHeaderValid | irqHeaderErr |
		irqCrcErr | irqTimeout
	r.sendCmd(cmdSetDioIrqParams,
		byte(irqMask>>8), byte(irqMask), // global IRQ mask
		byte(irqMask>>8), byte(irqMask), // DIO1 mask
		0x00, 0x00, // DIO2 mask
		0x00, 0x00) // DIO3 mask
	r.waitBusy()

	// Full register dump for diagnostic comparison vs RadioLib expected values.
	r.dumpRegisters()

	// Check device errors — any non-zero bits indicate a config issue.
	devErr := r.getDeviceErrors()
	log.Printf("lora: device errors=0x%04X (0=clean; bit2=PLL, bit3=XOSC, bit4=IMG_CAL, bit8=PLL_LOCK)", devErr)
	if devErr != 0 {
		r.sendCmd(cmdClearDeviceErrors, 0x00, 0x00)
		r.waitBusy()
	}

	return nil
}

// dumpRegisters prints all SX1262 registers we care about for diagnosis.
func (r *Radio) dumpRegisters() {
	// Packet type is queried via the GetPacketType COMMAND (0x11), not a register read.
	// Earlier dump labeled 0x0889 as "PACKET_TYPE" — that was wrong; that address is
	// some other internal register. This is the authoritative way to check.
	pktType := r.getPacketType()
	pktName := "unknown"
	switch pktType {
	case 0x00:
		pktName = "GFSK"
	case 0x01:
		pktName = "LoRa"
	case 0x02:
		pktName = "LR-FHSS"
	}

	// Read back the FRF register so we can see the chip's actual tuned frequency.
	// The SX1262 stores FRF at register 0x088B (4 bytes, MSB first). Decoded
	// frequency = FRF * 32MHz / 2^25.
	frfBytes := r.readRegister(0x088B, 4)
	frfReg := uint32(frfBytes[0])<<24 | uint32(frfBytes[1])<<16 | uint32(frfBytes[2])<<8 | uint32(frfBytes[3])
	freqHz := uint64(frfReg) * 32000000 / (1 << 25)
	log.Printf("lora: FRF readback=0x%08X → %d Hz (%.3f MHz)  [expect %d MHz]",
		frfReg, freqHz, float64(freqHz)/1e6, r.cfg.FreqHz/1000000)
	log.Printf("lora: GetPacketType cmd → 0x%02X (%s, expect 0x01 LoRa)", pktType, pktName)

	type regCheck struct {
		addr   uint16
		nBytes int
		name   string
		expect string
	}
	regs := []regCheck{
		{0x0736, 1, "IQ_POLARITY    ", "0x0D (standard)"},
		{0x0740, 2, "LORA_SYNC_WORD ", "0x14 0x24 (private 0x12)"},
		{0x08AC, 1, "RX_GAIN        ", "0x96 (boosted)"},
		{0x08D8, 1, "TX_CLAMP_CFG   ", "(workaround reg)"},
		{0x08E7, 1, "OCP            ", "0x38 (140mA default)"},
	}
	sb := r.readStatusBoth()
	log.Printf("lora: ── register dump ─── status[rx0=0x%02X rx1=0x%02X]", sb[0], sb[1])
	for _, rc := range regs {
		bytes := r.readRegister(rc.addr, rc.nBytes)
		hex := ""
		for _, b := range bytes {
			hex += fmt.Sprintf("0x%02X ", b)
		}
		log.Printf("lora:   reg 0x%04X %s = %s(expect %s)", rc.addr, rc.name, hex, rc.expect)
	}
	log.Printf("lora: ────────────────────────────────────────────")
}

// getDeviceErrors returns the SX1262 OpError mask via cmdGetDeviceErrors.
func (r *Radio) getDeviceErrors() uint16 {
	r.waitBusy()
	tx := []byte{cmdGetDeviceErrors, 0x00, 0x00, 0x00}
	rx := make([]byte, len(tx))
	r.spiTx(tx, rx)
	return uint16(rx[2])<<8 | uint16(rx[3])
}

// getPacketType returns the active modem (0x00=GFSK, 0x01=LoRa, 0x02=LR-FHSS).
// Authoritative way to verify the chip is in LoRa mode.
func (r *Radio) getPacketType() byte {
	r.waitBusy()
	tx := []byte{cmdGetPacketType, 0x00, 0x00}
	rx := make([]byte, len(tx))
	r.spiTx(tx, rx)
	return rx[2]
}

// Packet is a received LoRa packet.
type Packet struct {
	Payload []byte
	RSSI    int // dBm
	SNR     int // dB
}

// Receive blocks until a LoRa packet arrives or timeout elapses.
// Call Init() and then StartRX() before calling Receive.
// Returns nil, nil on timeout — caller should restart RX.
func (r *Radio) Receive(timeout time.Duration) (*Packet, error) {
	// Assert RXEN so the board's RF switch connects the antenna to the RX path.
	if r.rxen != nil {
		r.rxen.Out(gpio.High)
	}

	// Clear any stale IRQ flags before re-entering RX (RadioLib does this on every startReceive).
	r.clearIRQ(0xFFFF)

	// Enter RX continuous mode (0xFFFFFF). 0x000000 is "single shot" — chip
	// auto-returns to STDBY after one packet, ending the session if a packet
	// is missed. Continuous keeps us listening across drops.
	r.sendCmd(cmdSetRx, 0xFF, 0xFF, 0xFF)
	r.waitBusy()

	deadline := time.Now().Add(timeout)
	var rxIrq uint16
	for {
		if time.Now().After(deadline) {
			return nil, nil // timeout, no packet
		}
		irq := r.getIRQ()
		if irq&irqRxDone != 0 {
			rxIrq = irq // capture before clearing
			break
		}
		if irq != 0 {
			// Clear preamble/header-only flags and keep listening.
			r.clearIRQ(irq)
		}
		time.Sleep(time.Millisecond)
	}
	r.clearIRQ(0xFFFF)

	if rxIrq&(irqHeaderErr|irqCrcErr) != 0 {
		return nil, fmt.Errorf("lora: packet error (irq=0x%04X)", rxIrq)
	}

	// Get payload length and buffer start pointer
	bufStatus := r.readCmd(cmdGetRxBufferStatus, 2)
	payloadLen := bufStatus[0]
	bufPtr := bufStatus[1]

	if payloadLen == 0 {
		return nil, fmt.Errorf("lora: empty payload")
	}

	// Read the payload from the buffer
	payload := r.readBuffer(bufPtr, payloadLen)

	// Get RSSI and SNR
	pktStatus := r.readCmd(cmdGetPacketStatus, 3)
	rssi := -int(pktStatus[0]) / 2
	snrRaw := int8(pktStatus[1])
	snr := int(snrRaw) / 4

	return &Packet{
		Payload: payload,
		RSSI:    rssi,
		SNR:     snr,
	}, nil
}

// ── Low-level SPI helpers ─────────────────────────────────────────────────────

func (r *Radio) waitBusy() {
	deadline := time.Now().Add(5 * time.Second)
	for r.busy.Read() == gpio.High {
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// spiTx performs a full-duplex SPI transaction, asserting the software CS if configured.
func (r *Radio) spiTx(tx, rx []byte) {
	if r.cs != nil {
		r.cs.Out(gpio.Low)
	}
	r.conn.Tx(tx, rx) //nolint:errcheck
	if r.cs != nil {
		r.cs.Out(gpio.High)
	}
}

func (r *Radio) sendCmd(cmd byte, params ...byte) {
	buf := make([]byte, 1+len(params))
	buf[0] = cmd
	copy(buf[1:], params)
	r.spiTx(buf, nil)
}

// readCmd sends a command and returns n response bytes (skipping the leading status byte).
func (r *Radio) readCmd(cmd byte, n int) []byte {
	r.waitBusy()
	tx := make([]byte, 1+1+n) // cmd + status placeholder + data
	tx[0] = cmd
	rx := make([]byte, len(tx))
	r.spiTx(tx, rx)
	return rx[2:] // skip status byte + first NOP
}

func (r *Radio) readBuffer(offset, length byte) []byte {
	r.waitBusy()
	// cmd + offset + NOP + <length bytes>
	tx := make([]byte, 3+int(length))
	tx[0] = cmdReadBuffer
	tx[1] = offset
	rx := make([]byte, len(tx))
	r.spiTx(tx, rx)
	return rx[3:] // skip status, offset echo, NOP
}

func (r *Radio) getIRQ() uint16 {
	b := r.readCmd(cmdGetIrqStatus, 2)
	return uint16(b[0])<<8 | uint16(b[1])
}

func (r *Radio) clearIRQ(mask uint16) {
	r.sendCmd(cmdClearIrqStatus, byte(mask>>8), byte(mask))
	r.waitBusy()
}

// writeRegister writes a single byte to an SX1262 register.
// WriteRegister (0x0D): [cmd][addr_hi][addr_lo][data]
func (r *Radio) writeRegister(addr uint16, data byte) {
	r.waitBusy()
	tx := []byte{0x0D, byte(addr >> 8), byte(addr), data}
	r.spiTx(tx, nil)
}

// readRegister reads n bytes from the SX1262 register at addr.
// ReadRegister (0x1D): [cmd][addr_hi][addr_lo][NOP][data...]
func (r *Radio) readRegister(addr uint16, n int) []byte {
	r.waitBusy()
	tx := make([]byte, 4+n) // cmd + addr_hi + addr_lo + NOP + data
	tx[0] = cmdReadRegister
	tx[1] = byte(addr >> 8)
	tx[2] = byte(addr)
	rx := make([]byte, len(tx))
	r.spiTx(tx, rx)
	return rx[4:]
}

// readStatusBoth returns [rx0, rx1] of the GetStatus SPI transaction.
func (r *Radio) readStatusBoth() [2]byte {
	r.waitBusy()
	tx := []byte{cmdGetStatus, 0x00}
	rx := make([]byte, 2)
	r.spiTx(tx, rx)
	return [2]byte{rx[0], rx[1]}
}

// readStatus returns the SX1262 NStatus byte.
// Bit layout (SX1262 DS v2.1): bits[7:5]=ChipMode bits[4:3]=CmdStatus
//   ChipMode: 2=STDBY_RC 3=STDBY_XOSC 4=FS 5=RX 6=TX
//   Expected: STDBY_RC→0x40  FS→0x80  RX→0xA0  TX→0xC0
func (r *Radio) readStatus() byte {
	r.waitBusy()
	tx := []byte{cmdGetStatus, 0x00}
	rx := make([]byte, 2)
	r.spiTx(tx, rx)
	return rx[1]
}

// decodeIRQ returns a human-readable string of which IRQ bits are set.
func decodeIRQ(irq uint16) string {
	if irq == 0 {
		return "none"
	}
	flags := []struct {
		bit  uint16
		name string
	}{
		{irqRxDone, "RxDone"},
		{irqPreambleDet, "PreambleDetected"},
		{irqHeaderValid, "HeaderValid"},
		{irqHeaderErr, "HeaderErr"},
		{irqCrcErr, "CrcErr"},
		{irqTimeout, "Timeout"},
	}
	out := ""
	for _, f := range flags {
		if irq&f.bit != 0 {
			if out != "" {
				out += "|"
			}
			out += f.name
		}
	}
	return out
}

// symbolTimeMs returns the LoRa symbol time in ms for given SF/BW code.
// Used to decide whether LDRO (Low Data Rate Optimize) is required.
func symbolTimeMs(sf byte, bwCode byte) float64 {
	bwHz := float64(bwCodeToKHz(bwCode)) * 1000.0
	if bwHz == 0 {
		return 0
	}
	return float64(uint32(1)<<sf) * 1000.0 / bwHz
}

// imageCalParams returns the (freq1, freq2) bytes for CalibrateImage given the
// operating frequency in Hz. From SX1262 datasheet table 9-2.
func imageCalParams(freqHz uint32) (byte, byte) {
	mhz := freqHz / 1000000
	switch {
	case mhz >= 902 && mhz <= 928:
		return 0xE1, 0xE9 // US ISM
	case mhz >= 863 && mhz <= 870:
		return 0xD7, 0xDB // EU ISM
	case mhz >= 779 && mhz <= 787:
		return 0xC1, 0xC5
	case mhz >= 470 && mhz <= 510:
		return 0x75, 0x81
	case mhz >= 430 && mhz <= 440:
		return 0x6B, 0x6F
	default:
		return 0xE1, 0xE9 // fall back to US 915
	}
}

// tcxoVoltageCode converts a float voltage to the SX1262 DIO3 TCXO voltage code.
func tcxoVoltageCode(v float64) byte {
	switch {
	case v <= 1.65:
		return 0x00 // 1.6V
	case v <= 1.75:
		return 0x01 // 1.7V
	case v <= 1.9:
		return 0x02 // 1.8V
	case v <= 2.3:
		return 0x03 // 2.2V
	case v <= 2.55:
		return 0x04 // 2.4V
	case v <= 2.85:
		return 0x05 // 2.7V
	case v <= 3.15:
		return 0x06 // 3.0V
	default:
		return 0x07 // 3.3V
	}
}
