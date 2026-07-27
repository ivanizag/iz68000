package iz68000

import (
	"encoding/binary"
	"fmt"
	"io"
)

// https://www.nxp.com/docs/en/reference-manual/M68000PRM.pdf
// https://www.nxp.com/files-static/archives/doc/ref_manual/M68000UM.pdf
// http://goldencrystal.free.fr/M68kOpcodes-v2.3.pdf
// https://github.com/TomHarte/ProcessorTests

// State represents the state of the simulated device
type State struct {
	opcodes *[65536]opcode
	trace   bool

	reg    registers
	mem    Memory
	cycles uint64

	irqLevel     uint8
	lastIrqLevel uint8
	stopped      bool // Waiting for an interrupt after a STOP instruction

	ir          uint16 // The opcode word being executed
	extraCycles int    // Cycles added by the instruction to the ones on the table

	// The vector of the exception raised by the last instruction executed,
	// or 0 if it completed
	lastExceptionVector int

	lastResolvedAddress uint32
}

type opcode struct {
	name     string
	size     int
	operands int
	cycles   int
	action   opFunc
}

type opFunc func(s *State, ir uint16, op *opcode)

// ExecuteInstruction transforms the state given after a single instruction is
// executed. An instruction aborted by an exception counts as executed.
func (s *State) ExecuteInstruction() {
	defer s.recoverException()

	if level := s.pendingInterrupt(); level != 0 {
		s.processInterrupt(level)
	}
	s.lastIrqLevel = s.irqLevel
	s.lastExceptionVector = 0

	if s.stopped {
		// The STOP instruction waits for an interrupt to resume
		s.cycles += 4
		return
	}

	var traceLine string
	if s.trace {
		line, _ := s.DisasmInstruction(s.reg.getPC())
		traceLine = fmt.Sprintf("%-40s", line)
	}

	pc := s.reg.getPC()
	s.ir = s.fetchWord()
	op := &s.opcodes[s.ir]
	if op.action == nil {
		panic(fmt.Sprintf("Unknown opcode $%04x at $%06x\n", s.ir, pc))
	}

	op.action(s, s.ir, op)
	s.cycles += uint64(op.cycles + s.extraCycles)
	s.extraCycles = 0

	if s.trace {
		fmt.Printf("%s %v\n", traceLine, s.reg)
	}
}

// recoverException catches the instructions aborted by an exception. It is
// deferred by ExecuteInstruction(), the only place where recover() can work.
func (s *State) recoverException() {
	r := recover()
	if r == nil {
		return
	}

	e, ok := r.(exceptionSignal)
	if !ok {
		panic(r)
	}
	s.processException(e)
}

// Reset resets the processor. The supervisor stack pointer and the program
// counter are loaded from the two first vectors.
func (s *State) Reset() {
	s.reg.sr = flagS | flagI // Supervisor mode with the interrupts masked
	s.reg.otherSP = 0
	s.reg.setSP(s.peekLong(vectorResetSSP * 4))
	s.reg.setPC(s.peekLong(vectorResetPC * 4))
	s.stopped = false
	s.cycles += 40
}

/*
Memory access. Note that word and long accesses to an odd address raise an
address error, aborting the instruction. Bytes can be read anywhere.
*/

func (s *State) peekByte(address uint32) uint32 {
	return uint32(s.mem.Peek(address & addressMask))
}

func (s *State) peekWord(address uint32) uint32 {
	if address&1 != 0 {
		s.raiseAddressError(address, false)
	}
	return s.peekByte(address)<<8 | s.peekByte(address+1)
}

func (s *State) peekLong(address uint32) uint32 {
	return s.peekWord(address)<<16 | s.peekWord(address+2)
}

func (s *State) pokeByte(address uint32, value uint32) {
	s.mem.Poke(address&addressMask, uint8(value))
}

func (s *State) pokeWord(address uint32, value uint32) {
	if address&1 != 0 {
		s.raiseAddressError(address, true)
	}
	s.pokeByte(address, value>>8)
	s.pokeByte(address+1, value)
}

func (s *State) pokeLong(address uint32, value uint32) {
	s.pokeWord(address, value>>16)
	s.pokeWord(address+2, value)
}

func (s *State) peekSized(address uint32, size int) uint32 {
	switch size {
	case sizeByte:
		return s.peekByte(address)
	case sizeWord:
		return s.peekWord(address)
	}
	return s.peekLong(address)
}

func (s *State) pokeSized(address uint32, size int, value uint32) {
	switch size {
	case sizeByte:
		s.pokeByte(address, value)
	case sizeWord:
		s.pokeWord(address, value)
	default:
		s.pokeLong(address, value)
	}
}

// pokeWordRaw writes without checking the alignment. Used to build the
// exception frames, a misaligned stack there would be a double bus fault.
func (s *State) pokeWordRaw(address uint32, value uint32) {
	s.pokeByte(address, value>>8)
	s.pokeByte(address+1, value)
}

func (s *State) pokeLongRaw(address uint32, value uint32) {
	s.pokeWordRaw(address, value>>16)
	s.pokeWordRaw(address+2, value)
}

// fetchWord reads the next word of the instruction stream
func (s *State) fetchWord() uint16 {
	pc := s.reg.getPC()
	value := s.peekWord(pc)
	s.reg.setPC(pc + 2)
	return uint16(value)
}

func (s *State) fetchLong() uint32 {
	return uint32(s.fetchWord())<<16 | uint32(s.fetchWord())
}

// checkJumpTarget raises the address error that the prefetch from an odd
// target causes. The instructions that push a return address or write back a
// counter check it before committing anything.
func (s *State) checkJumpTarget(address uint32) {
	if address&1 != 0 {
		s.reg.setPC(address)
		s.raiseAddressError(address, false)
	}
}

// jump changes the program counter after a branch, a jump or a return. The
// processor prefetches from the target as part of the instruction, so an odd
// address raises the address error here and not on the next fetch.
func (s *State) jump(address uint32) {
	s.checkJumpTarget(address)
	s.reg.setPC(address)
}

/*
Stack access
*/

// push writes before moving the stack pointer, an address error must leave
// the stack where it was for the exception frame
func (s *State) push(value uint32, size int) {
	sp := s.reg.getSP() - uint32(size)
	s.pokeSized(sp, size, value)
	s.reg.setSP(sp)
}

func (s *State) pull(size int) uint32 {
	sp := s.reg.getSP()
	value := s.peekSized(sp, size)
	s.reg.setSP(sp + uint32(size))
	return value
}

/*
Disassembly
*/

// DisasmInstruction disassembles the instruction at the given address,
// returning the text and the address of the next instruction
func (s *State) DisasmInstruction(pc uint32) (string, uint32) {
	next := pc + 2
	ir := getWord(s.mem, pc)
	op := &s.opcodes[ir]
	if op.action == nil {
		return fmt.Sprintf("$%06x %-8s $%04x", pc, "???", ir), next
	}

	text := s.operandsString(ir, op, &next)
	return fmt.Sprintf("$%06x %s", pc, text), next
}

// GetCycles returns the count of CPU cycles since the last reset
func (s *State) GetCycles() uint64 {
	return s.cycles
}

// SetTrace activates tracing of the cpu execution
func (s *State) SetTrace(trace bool) {
	s.trace = trace
}

// GetTrace gets the tracing state of the cpu execution
func (s *State) GetTrace() bool {
	return s.trace
}

// SetMemory changes the memory provider
func (s *State) SetMemory(mem Memory) {
	s.mem = mem
}

// GetPC returns the program counter
func (s *State) GetPC() uint32 {
	return s.reg.getPC()
}

// SetPC changes the program counter, as a JMP instruction
func (s *State) SetPC(pc uint32) {
	s.reg.setPC(pc)
}

// IsSupervisor tells if the processor is in supervisor mode
func (s *State) IsSupervisor() bool {
	return s.reg.isSupervisor()
}

// Save saves the CPU state (registers and cycle counter)
func (s *State) Save(w io.Writer) error {
	err := binary.Write(w, binary.BigEndian, s.cycles)
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.BigEndian, s.reg.data)
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.BigEndian, s.reg.pc)
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.BigEndian, s.reg.sr)
	if err != nil {
		return err
	}
	return binary.Write(w, binary.BigEndian, s.reg.otherSP)
}

// Load loads the CPU state (registers and cycle counter)
func (s *State) Load(r io.Reader) error {
	err := binary.Read(r, binary.BigEndian, &s.cycles)
	if err != nil {
		return err
	}
	err = binary.Read(r, binary.BigEndian, &s.reg.data)
	if err != nil {
		return err
	}
	err = binary.Read(r, binary.BigEndian, &s.reg.pc)
	if err != nil {
		return err
	}
	err = binary.Read(r, binary.BigEndian, &s.reg.sr)
	if err != nil {
		return err
	}
	return binary.Read(r, binary.BigEndian, &s.reg.otherSP)
}
