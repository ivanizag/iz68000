package iz68000

/*
	Tests from https://github.com/SingleStepTests/m68000, the TomHarte style
	tests for the 68000 generated with the microcoded core of MAME.

	The tests are disabled by default because they require a huge download.
	To enable them, clone the repo and change ProcessorTestsEnable and
	ProcessorTestsPath:

		git clone https://github.com/SingleStepTests/m68000 m68000-tests

	The .json.bin files are read as they are, there is no need to run the
	decode.py script of the repo.

	Know issues:
		- The cycle counts are not verified yet (Note 1)
		- Part of the frame pushed by the group 0 exceptions (Note 2)
		- MOVE.L, ADDX.L, SUBX.L and CMPM.L aborted by an address error on
		  their second operand (Note 3)
		- The tests of STOP are skipped (Note 4)
		- The N and V flags of ABCD, SBCD and NBCD (Note 5)

	Notes 2 and 3 are both the prefetch queue and the bus cycles of the real
	processor showing through when an address error aborts an instruction. An
	emulator with instruction level timing can't reproduce them.

	Note 5: the manual leaves N and V undefined for the decimal instructions,
	the V here does not always agree with the microcode of MAME. A handful of
	SBCD scenarios also differ on the result itself, all of them with operands
	holding digits above 9, which is not valid decimal input.

	Each test is dispatched using the opcode on the prefetch queue, the tests
	of the instructions not implemented yet are skipped.
*/

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

var ProcessorTestsEnable = false
var ProcessorTestsPath = "../m68000-tests/v1/"
var ProcessorTestsCheckCycles = false      // Note 1
var ProcessorTestsCheckExceptionPC = false // Note 2

/*
The tests set the program counter with the m_au register of MAME, the address
of the next prefetch. It is 4 bytes ahead of the instruction being executed.
*/
const prefetchAhead = 4

// Note 4: the final states of STOP can't be reached by executing a single
// instruction, the processor is halted there and the tests carry on to
// whatever woke it up.
var skippedFiles = map[string]bool{
	"STOP.json.bin": true,
}

type scenarioState struct {
	d        [8]uint32
	a        [7]uint32
	usp      uint32
	ssp      uint32
	sr       uint32
	pc       uint32
	prefetch [2]uint32
	ram      [][2]uint32
}

type scenario struct {
	name    string
	initial scenarioState
	final   scenarioState
	cycles  int
}

func TestHarteM68000(t *testing.T) {
	if !ProcessorTestsEnable {
		t.Skip("SingleStepTests/m68000 are not enabled")
	}

	files, err := filepath.Glob(ProcessorTestsPath + "*.json.bin")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("No tests found on %s", ProcessorTestsPath)
	}
	sort.Strings(files)

	for _, file := range files {
		file := file
		name := filepath.Base(file)
		if skippedFiles[name] {
			continue // Note 4
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testFile(t, file)
		})
	}
}

func testFile(t *testing.T, filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	scenarios, err := decodeScenarios(data)
	if err != nil {
		t.Fatal(err)
	}

	m := &sparseMemory{}
	s := NewM68000(m)

	run, skipped := 0, 0
	for i := range scenarios {
		sc := &scenarios[i]
		if s.opcodes[uint16(sc.initial.prefetch[0])].action == nil {
			// The instruction is not implemented yet
			skipped++
			continue
		}
		run++
		t.Run(sc.name, func(t *testing.T) {
			m.reset()
			testScenario(t, s, sc)
		})
	}
	t.Logf("%d scenarios run, %d skipped", run, skipped)
}

func testScenario(t *testing.T, s *State, sc *scenario) {
	// Setup the CPU. The stack pointers are assigned without going through
	// setSR() to avoid its swapping logic.
	for i := 0; i < 8; i++ {
		s.reg.data[regD0+i] = sc.initial.d[i]
	}
	for i := 0; i < 7; i++ {
		s.reg.data[regA0+i] = sc.initial.a[i]
	}
	s.reg.sr = uint16(sc.initial.sr) & srMask
	if s.reg.isSupervisor() {
		s.reg.setSP(sc.initial.ssp)
		s.reg.otherSP = sc.initial.usp
	} else {
		s.reg.setSP(sc.initial.usp)
		s.reg.otherSP = sc.initial.ssp
	}
	s.reg.setPC(sc.initial.pc - prefetchAhead)

	// The prefetched words are written first, the RAM of the test wins if it
	// also defines them
	setWord(s.mem, s.reg.getPC(), uint16(sc.initial.prefetch[0]))
	setWord(s.mem, s.reg.getPC()+2, uint16(sc.initial.prefetch[1]))
	for _, e := range sc.initial.ram {
		s.mem.Poke(e[0], uint8(e[1]))
	}

	// Execute the instruction
	start := s.GetCycles()
	s.ExecuteInstruction()

	// Check the result
	for i := 0; i < 8; i++ {
		assertReg(t, sc, fmt.Sprintf("D%d", i), s.reg.getD(i), sc.final.d[i])
	}
	for i := 0; i < 7; i++ {
		assertReg(t, sc, fmt.Sprintf("A%d", i), s.reg.getA(i), sc.final.a[i])
	}
	assertReg(t, sc, "USP", s.reg.getUSP(), sc.final.usp)
	assertReg(t, sc, "SSP", supervisorSP(s), sc.final.ssp)
	assertReg(t, sc, "PC", s.reg.getPC()+prefetchAhead, sc.final.pc)
	assertFlags(t, sc, s.reg.getSR(), uint16(sc.final.sr)&srMask)

	for _, e := range sc.final.ram {
		if isPrefetchDependent(s, e[0]) {
			continue // Note 2
		}
		if value := uint32(s.mem.Peek(e[0])); value != e[1] {
			t.Errorf("Memory at $%06x is $%02x and should be $%02x for %s",
				e[0], value, e[1], sc)
		}
	}

	if ProcessorTestsCheckCycles {
		cycles := int(s.GetCycles() - start)
		if cycles != sc.cycles {
			t.Errorf("Took %v cycles, it should be %v for %s", cycles, sc.cycles, sc)
		}
	}
}

/*
isPrefetchDependent tells if an address belongs to the part of a group 0
exception frame that an instruction level emulator can't reproduce.

Three of the seven words of the frame depend on how far the prefetch had got
when the access failed:

  - The program counter, from 2 to 10 bytes past the start of the instruction
    depending on the addressing mode and on the direction of the access.
  - The instruction register, that on the failed writes already has the first
    word of the next instruction.
  - The status word, that keeps the unused bits of that same register.

The address of the failed access, the status register and the function code
bits of the status word are compared, they are well defined.
*/
func isPrefetchDependent(s *State, address uint32) bool {
	if ProcessorTestsCheckExceptionPC {
		return false
	}
	if s.lastExceptionVector != vectorAddressError &&
		s.lastExceptionVector != vectorBusError {
		return false
	}

	offset := address - s.reg.getSP()
	return offset < 2 || (offset >= 6 && offset < 8) || (offset >= 10 && offset < 14)
}

// supervisorSP returns the supervisor stack pointer, wherever it is stored
func supervisorSP(s *State) uint32 {
	if s.reg.isSupervisor() {
		return s.reg.getSP()
	}
	return s.reg.otherSP
}

func assertReg(t *testing.T, sc *scenario, name string, actual uint32, wanted uint32) {
	if actual != wanted {
		t.Errorf("Register %s is $%08x and should be $%08x for %s", name, actual, wanted, sc)
	}
}

func assertFlags(t *testing.T, sc *scenario, actual uint16, wanted uint16) {
	if actual != wanted {
		t.Errorf("%016b flag diffs, they are %04x and should be %04x for %s",
			actual^wanted, actual, wanted, sc)
	}
}

func (sc *scenario) String() string {
	return fmt.Sprintf("%s (opcode $%04x)", sc.name, sc.initial.prefetch[0])
}

/*
Decoding of the .json.bin files. The layout is the one implemented by the
decode.py script of the test repo, little endian and without padding.
*/

const (
	magicFile        = 0x1A3F5D71
	magicTest        = 0xABC12367
	magicName        = 0x89ABCDEF
	magicState       = 0x01234567
	magicTransaction = 0x456789AB
)

type binReader struct {
	data []byte
	pos  int
	err  error
}

func (r *binReader) u8() uint8 {
	if r.err != nil || r.pos+1 > len(r.data) {
		r.fail()
		return 0
	}
	value := r.data[r.pos]
	r.pos++
	return value
}

func (r *binReader) u16() uint16 {
	if r.err != nil || r.pos+2 > len(r.data) {
		r.fail()
		return 0
	}
	value := binary.LittleEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return value
}

func (r *binReader) u32() uint32 {
	if r.err != nil || r.pos+4 > len(r.data) {
		r.fail()
		return 0
	}
	value := binary.LittleEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return value
}

func (r *binReader) fail() {
	if r.err == nil {
		r.err = fmt.Errorf("unexpected end of file at %d", r.pos)
	}
}

// block reads the size and magic number that prefix each section
func (r *binReader) block(magic uint32) {
	r.u32() // The size of the block, not needed to walk it
	if found := r.u32(); found != magic && r.err == nil {
		r.err = fmt.Errorf("bad magic number $%08x, expected $%08x at %d", found, magic, r.pos-4)
	}
}

func decodeScenarios(data []byte) ([]scenario, error) {
	r := &binReader{data: data}

	if magic := r.u32(); magic != magicFile {
		return nil, fmt.Errorf("bad file magic number $%08x", magic)
	}
	count := r.u32()

	scenarios := make([]scenario, 0, count)
	for i := uint32(0); i < count && r.err == nil; i++ {
		var sc scenario
		r.block(magicTest)
		sc.name = r.name()
		sc.initial = r.state()
		sc.final = r.state()
		sc.cycles = r.transactions()
		scenarios = append(scenarios, sc)
	}
	return scenarios, r.err
}

func (r *binReader) name() string {
	r.block(magicName)
	length := int(r.u32())
	if r.err != nil || r.pos+length > len(r.data) {
		r.fail()
		return ""
	}
	value := string(r.data[r.pos : r.pos+length])
	r.pos += length
	return value
}

func (r *binReader) state() scenarioState {
	var st scenarioState
	r.block(magicState)

	for i := 0; i < 8; i++ {
		st.d[i] = r.u32()
	}
	for i := 0; i < 7; i++ {
		st.a[i] = r.u32()
	}
	st.usp = r.u32()
	st.ssp = r.u32()
	st.sr = r.u32()
	st.pc = r.u32()
	st.prefetch[0] = r.u32()
	st.prefetch[1] = r.u32()

	// The RAM is stored as words, they are split in bytes as the tests of the
	// 8 bit processors do
	count := r.u32()
	if r.err != nil {
		return st
	}
	st.ram = make([][2]uint32, 0, 2*count)
	for i := uint32(0); i < count; i++ {
		address := r.u32()
		value := r.u16()
		st.ram = append(st.ram,
			[2]uint32{address, uint32(value >> 8)},
			[2]uint32{address | 1, uint32(value & 0xff)})
	}
	return st
}

// transactions walks the bus activity, only the cycle count is used
func (r *binReader) transactions() int {
	r.block(magicTransaction)
	cycles := int(r.u32())
	count := r.u32()

	for i := uint32(0); i < count && r.err == nil; i++ {
		kind := r.u8()
		r.u32() // Cycles of this transaction
		if kind != 0 {
			// Function code, address bus, data bus, UDS and LDS
			for j := 0; j < 5; j++ {
				r.u32()
			}
		}
	}
	return cycles
}

/*
sparseMemory is used instead of FlatMemory to avoid allocating 16Mb per test
*/

type sparseMemory struct {
	data map[uint32]uint8
}

func (m *sparseMemory) reset() {
	m.data = make(map[uint32]uint8)
}

func (m *sparseMemory) Peek(address uint32) uint8 {
	return m.data[address&addressMask]
}

func (m *sparseMemory) PeekCode(address uint32) uint8 {
	return m.data[address&addressMask]
}

func (m *sparseMemory) Poke(address uint32, value uint8) {
	m.data[address&addressMask] = value
}
