package iz68000

import "os"

// The 68000 has only 24 address lines. The most significant byte of the
// addresses is ignored, something the Macintosh memory manager relies on to
// store flags on the master pointers.
const addressMask uint32 = 0x00ffffff

// Memory represents the addressable space of the processor. Note that the
// 68000 is big endian and that words and longs are built from the bytes here.
type Memory interface {
	Peek(address uint32) uint8
	Poke(address uint32, value uint8)

	// PeekCode can be used to optimize the memory manager to requests with
	// more locality. It must return the same as a call to Peek()
	PeekCode(address uint32) uint8
}

/*
ResetLine can be implemented by a Memory to be told when the RESET instruction
asserts the reset line of the board. The 68000 asserts the line for 124 clocks
and carries on with the next instruction: the processor itself is not reset,
and what the line does to the rest of the board is the board's business. On a
Macintosh Plus, for one, the machine starts again.

ResetDevices is called from inside ExecuteInstruction, before it returns. A
board that resets the processor as well, with Reset(), should note that it
has to and do it once the instruction has returned.

A Memory that does not implement it is not told, and RESET does nothing but
take its time, as it always has.
*/
type ResetLine interface {
	ResetDevices()
}

func getWord(m Memory, address uint32) uint16 {
	return uint16(m.Peek(address))<<8 | uint16(m.Peek(address+1))
}

func getLong(m Memory, address uint32) uint32 {
	return uint32(getWord(m, address))<<16 | uint32(getWord(m, address+2))
}

func setWord(m Memory, address uint32, value uint16) {
	m.Poke(address, uint8(value>>8))
	m.Poke(address+1, uint8(value))
}

func setLong(m Memory, address uint32, value uint32) {
	setWord(m, address, uint16(value>>16))
	setWord(m, address+2, uint16(value))
}

// FlatMemory puts RAM on the 16Mb addressable by the processor. As it is too
// big to be embedded on the struct, it must be created with NewFlatMemory().
type FlatMemory struct {
	data []uint8
}

// NewFlatMemory returns 16Mb of RAM covering the full address space
func NewFlatMemory() *FlatMemory {
	return &FlatMemory{data: make([]uint8, addressMask+1)}
}

// Peek returns the data on the given address
func (m *FlatMemory) Peek(address uint32) uint8 {
	return m.data[address&addressMask]
}

// PeekCode returns the data on the given address
func (m *FlatMemory) PeekCode(address uint32) uint8 {
	return m.data[address&addressMask]
}

// Poke sets the data at the given address
func (m *FlatMemory) Poke(address uint32, value uint8) {
	m.data[address&addressMask] = value
}

func (m *FlatMemory) loadBinary(filename string, address uint32) error {
	bytes, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	for i, v := range bytes {
		m.Poke(address+uint32(i), v)
	}

	return nil
}
