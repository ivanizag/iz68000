package iz68000

import (
	"strings"
	"testing"
)

// buildTestCPU returns a cpu with the program loaded at $1000 and the reset
// vectors pointing to it
func buildTestCPU(program []uint16) (*State, *sparseMemory) {
	m := &sparseMemory{}
	m.reset()
	s := NewM68000(m)

	setLong(m, vectorResetSSP*4, 0x8000)
	setLong(m, vectorResetPC*4, 0x1000)
	for i, word := range program {
		setWord(m, 0x1000+uint32(2*i), word)
	}

	s.Reset()
	return s, m
}

func runSteps(s *State, steps int) {
	for i := 0; i < steps; i++ {
		s.ExecuteInstruction()
	}
}

func TestReset(t *testing.T) {
	s, _ := buildTestCPU(nil)

	if s.reg.getSP() != 0x8000 {
		t.Errorf("The stack pointer is $%08x, it should be $8000", s.reg.getSP())
	}
	if s.reg.getPC() != 0x1000 {
		t.Errorf("The program counter is $%08x, it should be $1000", s.reg.getPC())
	}
	if !s.IsSupervisor() {
		t.Error("The processor should start on supervisor mode")
	}
}

func TestAddLoop(t *testing.T) {
	s, _ := buildTestCPU([]uint16{
		0x7005, // MOVEQ #5,D0
		0x7200, // MOVEQ #0,D1
		0xd240, // ADD.W D0,D1
		0x5300, // SUBQ.B #1,D0
		0x66fa, // BNE.S -6
	})

	runSteps(s, 2+5*3)

	// The loop adds 5+4+3+2+1
	if s.reg.getD(1) != 15 {
		t.Errorf("D1 is $%08x, it should be $0000000f", s.reg.getD(1))
	}
	if s.reg.getD(0) != 0 {
		t.Errorf("D0 is $%08x, it should be 0", s.reg.getD(0))
	}
}

func TestAddressingModes(t *testing.T) {
	s, m := buildTestCPU([]uint16{
		0x203c, 0x1234, 0x5678, // $1000 MOVE.L #$12345678,D0
		0x23c0, 0x0000, 0x2000, // $1006 MOVE.L D0,($2000).L
		0x327c, 0x2000, //         $100c MOVEA.W #$2000,A1
		0x1029, 0x0003, //         $1010 MOVE.B ($3,A1),D0
	})

	runSteps(s, 2)
	if value := getLong(m, 0x2000); value != 0x12345678 {
		t.Errorf("The memory at $2000 is $%08x, it should be $12345678", value)
	}

	runSteps(s, 2)
	if s.reg.getA(1) != 0x2000 {
		t.Errorf("A1 is $%08x, it should be $2000", s.reg.getA(1))
	}
	// The byte at $2003 is the least significant one of the long just stored
	if s.reg.getD(0)&0xff != 0x78 {
		t.Errorf("D0 is $%08x, the low byte should be $78", s.reg.getD(0))
	}
}

func TestSubroutine(t *testing.T) {
	s, _ := buildTestCPU([]uint16{
		0x6100, 0x0004, // BSR.W +4
		0x4e71, // NOP
		0x7042, // MOVEQ #$42,D0
		0x4e75, // RTS
	})

	s.ExecuteInstruction() // BSR
	if s.reg.getPC() != 0x1006 {
		t.Errorf("The program counter is $%08x, it should be $1006", s.reg.getPC())
	}
	if s.reg.getSP() != 0x7ffc {
		t.Errorf("The stack pointer is $%08x, it should be $7ffc", s.reg.getSP())
	}

	runSteps(s, 2) // MOVEQ and RTS
	if s.reg.getD(0) != 0x42 {
		t.Errorf("D0 is $%08x, it should be $42", s.reg.getD(0))
	}
	if s.reg.getPC() != 0x1004 {
		t.Errorf("The program counter is $%08x, it should be $1004", s.reg.getPC())
	}
	if s.reg.getSP() != 0x8000 {
		t.Errorf("The stack pointer is $%08x, it should be $8000", s.reg.getSP())
	}
}

func TestAddressError(t *testing.T) {
	s, m := buildTestCPU([]uint16{
		0x307c, 0x2001, // MOVEA.W #$2001,A0
		0x3080, // MOVE.W D0,(A0)
	})
	setLong(m, vectorAddressError*4, 0x3000)

	runSteps(s, 2)

	if s.reg.getPC() != 0x3000 {
		t.Errorf("The program counter is $%08x, the address error handler was not called", s.reg.getPC())
	}
	// The group 0 exceptions push 14 bytes
	if s.reg.getSP() != 0x8000-14 {
		t.Errorf("The stack pointer is $%08x, it should be $%08x", s.reg.getSP(), 0x8000-14)
	}
	if address := getLong(m, s.reg.getSP()+2); address != 0x2001 {
		t.Errorf("The address on the frame is $%08x, it should be $2001", address)
	}
}

func TestLineATrap(t *testing.T) {
	s, m := buildTestCPU([]uint16{
		0xa9f4, // _ExitToShell, a Macintosh toolbox trap
	})
	setLong(m, vectorLineA*4, 0x4000)

	s.ExecuteInstruction()

	if s.reg.getPC() != 0x4000 {
		t.Errorf("The program counter is $%08x, the line A handler was not called", s.reg.getPC())
	}
	// The line A traps are faults, they stack the address of the trap itself
	// so that the handler can read the opcode and know which one it was
	if address := getLong(m, s.reg.getSP()+2); address != 0x1000 {
		t.Errorf("The address on the frame is $%08x, it should be $1000", address)
	}
}

func TestPrivilegeViolation(t *testing.T) {
	s, m := buildTestCPU([]uint16{
		0x4e73, // RTE
	})
	setLong(m, vectorPrivilegeViolation*4, 0x5000)

	// Leave the supervisor mode
	s.reg.setSR(0)
	s.ExecuteInstruction()

	if s.reg.getPC() != 0x5000 {
		t.Errorf("The program counter is $%08x, the privilege handler was not called", s.reg.getPC())
	}
	if !s.IsSupervisor() {
		t.Error("The exception should switch to supervisor mode")
	}
}

func TestInterrupt(t *testing.T) {
	s, m := buildTestCPU([]uint16{
		0x4e71, // NOP
		0x4e71, // NOP
	})
	setLong(m, (vectorAutovector+0)*4, 0x6000) // Level 1
	setWord(m, 0x6000, 0x4e71)                 // NOP on the handler

	// The reset masks all the interrupts
	s.SetIRQ(1)
	s.ExecuteInstruction()
	if s.reg.getPC() != 0x1002 {
		t.Errorf("The interrupt of level 1 should be masked")
	}

	// As on iz6502, the pending interrupt is serviced and then the first
	// instruction of the handler is executed on the same call
	s.reg.setInterruptMask(0)
	s.ExecuteInstruction()
	if s.reg.getPC() != 0x6002 {
		t.Errorf("The program counter is $%08x, the interrupt was not serviced", s.reg.getPC())
	}
	if s.reg.getInterruptMask() != 1 {
		t.Errorf("The interrupt mask is %d, it should be 1", s.reg.getInterruptMask())
	}
}

func TestDisasm(t *testing.T) {
	s, _ := buildTestCPU([]uint16{
		0x203c, 0x1234, 0x5678, // $1000 MOVE.L #$12345678,D0
		0x23c0, 0x0000, 0x2000, // $1006 MOVE.L D0,($2000).L
		0x1029, 0x0003, //         $100c MOVE.B ($3,A1),D0
		0x66fa, //                 $1010 BNE.S $100c
		0xe348, //                 $1012 LSL.W #1,D0
		0x4e75, //                 $1014 RTS
	})

	wanted := []string{
		"MOVE.L   #$12345678,D0",
		"MOVE.L   D0,($2000).L",
		"MOVE.B   ($3,A1),D0",
		"BNE      $100c",
		"LSL.W    #1,D0",
		"RTS",
	}

	pc := uint32(0x1000)
	for _, want := range wanted {
		var line string
		line, pc = s.DisasmInstruction(pc)
		if !strings.HasSuffix(strings.TrimSpace(line), want) {
			t.Errorf("Disassembled as %q, it should end with %q", line, want)
		}
	}
}

func TestOpcodeTableCoverage(t *testing.T) {
	implemented := 0
	for i := range opcodes68000 {
		if opcodes68000[i].action != nil {
			implemented++
		}
	}

	// Building the table panics on the patterns claiming the same opcode, so
	// getting here already tells that there are no overlaps
	t.Logf("%d of 65536 opcode words implemented (%.1f%%)",
		implemented, 100*float64(implemented)/65536)

	if implemented < 40000 {
		t.Errorf("Only %d opcode words are implemented", implemented)
	}
}
