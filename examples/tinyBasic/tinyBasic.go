package main

/*
The Motorola MC68000 Educational Computer Board of 1981, the most minimal
68000 computer there is: a processor, RAM, the Tutor monitor on ROM and two
serial ports on 6850 ACIAs.

Tiny BASIC runs from RAM and only uses the console port, so the ROM and the
second port are not needed here. Tutor would load it and start it with its GO
command, the reset vectors do the same job.

https://en.wikipedia.org/wiki/Motorola_68000_Educational_Computer_Board
https://en.wikipedia.org/wiki/Tiny_BASIC
*/

import (
	"bufio"
	"fmt"
	"os"

	"github.com/ivanizag/iz68000"
)

const (
	// The board has 32Kb of RAM, the address Tiny BASIC uses as the top of
	// its stack
	ramSize = 0x8000

	// The console ACIA. The status has the receiver ready on the bit 0 and
	// the transmitter ready on the bit 1.
	aciaStatus = 0x10040
	aciaData   = 0x10042

	statusReceiverReady    = 1 << 0
	statusTransmitterReady = 1 << 1

	// Tiny BASIC is assembled to run from the first address Tutor leaves free
	tinyBasicAddress = 0x900

	// The entries used of the vector table, that has a long per vector
	// starting at address zero
	resetStackVector = 4 * 0
	resetPCVector    = 4 * 1
	trap14Vector     = 4 * (32 + 14)

	// The BYE command returns to the monitor with a TRAP #14. There is no
	// monitor here, so its vector points to an address that ends the run.
	monitorReturn = 0x400
)

type machine struct {
	cpu   *iz68000.State
	input chan uint8
	ram   [ramSize]uint8
}

// Peek returns the data on the given address
func (m *machine) Peek(address uint32) uint8 {
	switch address {
	case aciaStatus:
		// There is always room to send, the terminal never makes us wait
		status := uint8(statusTransmitterReady)
		if len(m.input) != 0 {
			status |= statusReceiverReady
		}
		return status

	case aciaData:
		select {
		case char := <-m.input:
			return char
		default:
			return 0
		}
	}

	if address < ramSize {
		return m.ram[address]
	}
	return 0
}

// PeekCode returns the data on the given address, without the side effects of
// reading the serial port
func (m *machine) PeekCode(address uint32) uint8 {
	if address < ramSize {
		return m.ram[address]
	}
	return 0
}

// Poke sets the data at the given address
func (m *machine) Poke(address uint32, value uint8) {
	if address == aciaData {
		os.Stdout.Write([]uint8{value})
		return
	}

	if address < ramSize {
		m.ram[address] = value
	}
}

func (m *machine) pokeLong(address uint32, value uint32) {
	m.Poke(address, uint8(value>>24))
	m.Poke(address+1, uint8(value>>16))
	m.Poke(address+2, uint8(value>>8))
	m.Poke(address+3, uint8(value))
}

func newMachine() *machine {
	var m machine
	m.cpu = iz68000.NewM68000(&m)
	m.input = make(chan uint8, 256)

	program, err := os.ReadFile("tinybasic.bin")
	if err != nil {
		panic(err)
	}
	copy(m.ram[tinyBasicAddress:], program)

	// Tiny BASIC loads the stack pointer itself, the reset only has to get to
	// its first instruction
	m.pokeLong(resetStackVector, ramSize)
	m.pokeLong(resetPCVector, tinyBasicAddress)
	m.pokeLong(trap14Vector, monitorReturn)

	m.cpu.Reset()
	return &m
}

func (m *machine) run() {
	for m.cpu.GetPC() != monitorReturn {
		m.cpu.ExecuteInstruction()
	}

	fmt.Println("\nBack to the monitor, that isn't there. Bye.")
	os.Exit(0)
}

// readTerminal feeds the console port with what is typed, a line at a time.
// The board expects a carriage return to end them.
func (m *machine) readTerminal() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		for _, char := range scanner.Bytes() {
			m.input <- char
		}
		m.input <- '\r'
	}
}

func main() {
	m := newMachine()
	fmt.Println("Motorola MC68000 Educational Computer Board")
	fmt.Println("Type BYE to exit")

	// The processor runs here, the terminal is the one on the side. Reaching
	// the end of the input must not stop the machine.
	go m.readTerminal()
	m.run()
}
