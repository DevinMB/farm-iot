package main

import (
	"fmt"
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
	cmdSetPacketType       byte = 0x01
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
)

// IRQ flags
const (
	irqRxDone    uint16 = 0x0002
	irqHeaderErr uint16 = 0x0020
	irqCrcErr    uint16 = 0x0040
	irqTimeout   uint16 = 0x0200
)

// LoRaConfig holds LoRa radio configuration.
type LoRaConfig struct {
	SPIDev    string // e.g. /dev/spidev0.0
	ResetPin  string // BCM pin number as string, e.g. "22"
	BusyPin   string // e.g. "23"
	DIO1Pin   string // e.g. "6"
	FreqHz    uint32 // e.g. 915000000
	SF        byte   // spreading factor 5-12
	BWCode    byte   // 0x04 = 125kHz, 0x05 = 250kHz, 0x06 = 500kHz
	CR        byte   // 0x01 = 4/5
}

// Radio wraps the SX1262 hardware.
type Radio struct {
	bus   spireg.Ref
	conn  spi.Conn
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
	if err := dio1In.In(gpio.PullDown, gpio.RisingEdge); err != nil {
		return nil, fmt.Errorf("lora: dio1 pin input: %w", err)
	}

	busRef, err := spireg.Open(cfg.SPIDev)
	if err != nil {
		return nil, fmt.Errorf("lora: spi open %s: %w", cfg.SPIDev, err)
	}
	conn, err := busRef.Connect(8*physic.MegaHertz, spi.Mode0, 8)
	if err != nil {
		busRef.Close()
		return nil, fmt.Errorf("lora: spi connect: %w", err)
	}

	return &Radio{
		conn:  conn,
		reset: resetOut,
		busy:  busyIn,
		dio1:  dio1In,
		cfg:   cfg,
	}, nil
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
	time.Sleep(20 * time.Millisecond)
	r.waitBusy()

	// Standby (use RC oscillator)
	r.sendCmd(cmdSetStandby, 0x00)
	r.waitBusy()

	// DIO2 controls the RF switch on most HATs
	r.sendCmd(cmdSetDio2RfSwitch, 0x01)
	r.waitBusy()

	// LoRa packet type
	r.sendCmd(cmdSetPacketType, 0x01)
	r.waitBusy()

	// RF frequency: frf = freqHz * 2^25 / 32000000
	frf := uint32(uint64(r.cfg.FreqHz) * (1 << 25) / 32000000)
	r.sendCmd(cmdSetRfFrequency, byte(frf>>16), byte(frf>>8), byte(frf))
	r.waitBusy()

	// Buffer base addresses (TX=0x00, RX=0x00)
	r.sendCmd(cmdSetBufferBaseAddr, 0x00, 0x00)
	r.waitBusy()

	// Modulation: SF, BW code, CR, LDRO=off
	r.sendCmd(cmdSetModulationParams, r.cfg.SF, r.cfg.BWCode, r.cfg.CR, 0x00)
	r.waitBusy()

	// Packet: preamble=12, explicit header, max payload=255, CRC on, IQ standard
	r.sendCmd(cmdSetPacketParams, 0x00, 0x0C, 0x00, 0xFF, 0x01, 0x00)
	r.waitBusy()

	// IRQ mask on DIO1: RxDone | Timeout | HeaderErr | CrcErr
	irqMask := irqRxDone | irqTimeout | irqHeaderErr | irqCrcErr
	r.sendCmd(cmdSetDioIrqParams,
		byte(irqMask>>8), byte(irqMask), // global IRQ mask
		byte(irqMask>>8), byte(irqMask), // DIO1 mask
		0x00, 0x00, // DIO2 mask
		0x00, 0x00) // DIO3 mask
	r.waitBusy()

	return nil
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
	// Enter RX mode (no timeout = 0x000000, stays in RX until packet)
	r.sendCmd(cmdSetRx, 0x00, 0x00, 0x00)
	r.waitBusy()

	// Wait for DIO1 rising edge (IRQ)
	if !r.dio1.WaitForEdge(timeout) {
		return nil, nil // timeout, no packet
	}

	// Read IRQ status
	irq := r.getIRQ()
	r.clearIRQ(irq)

	if irq&(irqHeaderErr|irqCrcErr) != 0 {
		return nil, fmt.Errorf("lora: packet error (irq=0x%04X)", irq)
	}
	if irq&irqTimeout != 0 {
		return nil, nil // radio timeout
	}
	if irq&irqRxDone == 0 {
		return nil, nil // not a receive event
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

func (r *Radio) sendCmd(cmd byte, params ...byte) {
	buf := make([]byte, 1+len(params))
	buf[0] = cmd
	copy(buf[1:], params)
	r.conn.Tx(buf, nil) //nolint:errcheck
}

// readCmd sends a command and returns n response bytes (skipping the leading status byte).
func (r *Radio) readCmd(cmd byte, n int) []byte {
	r.waitBusy()
	tx := make([]byte, 1+1+n) // cmd + status placeholder + data
	tx[0] = cmd
	rx := make([]byte, len(tx))
	r.conn.Tx(tx, rx) //nolint:errcheck
	return rx[2:]     // skip status byte + first NOP
}

func (r *Radio) readBuffer(offset, length byte) []byte {
	r.waitBusy()
	// cmd + offset + NOP + <length bytes>
	tx := make([]byte, 3+int(length))
	tx[0] = cmdReadBuffer
	tx[1] = offset
	rx := make([]byte, len(tx))
	r.conn.Tx(tx, rx) //nolint:errcheck
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
