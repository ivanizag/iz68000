package iz68000

import "fmt"

// The exception vectors. The vector table is at address 0 and has a long with
// the handler address for each vector.
const (
	vectorResetSSP           = 0
	vectorResetPC            = 1
	vectorBusError           = 2
	vectorAddressError       = 3
	vectorIllegalInstruction = 4
	vectorZeroDivide         = 5
	vectorChk                = 6
	vectorTrapv              = 7
	vectorPrivilegeViolation = 8
	vectorTrace              = 9
	vectorLineA              = 10 // The Macintosh toolbox traps
	vectorLineF              = 11
	vectorSpuriousInterrupt  = 24
	vectorAutovector         = 25 // 25 to 31 for the interrupt levels 1 to 7
	vectorTrap               = 32 // 32 to 47 for TRAP #0 to TRAP #15
)

/*
An exception can be raised in the middle of an instruction, when the effective
addresses are already resolved or even after some memory was written. To abort
the instruction we panic with an exceptionSignal, recovered by
ExecuteInstruction(). Note that this is not used for interrupts, they are
always processed between instructions.
*/
type exceptionSignal struct {
	vector  int
	address uint32 // The address that failed on the address and bus errors
	write   bool
	ir      uint16

	// True when the instruction had already done its work and it was the
	// prefetch from a bad jump target that failed
	afterInstruction bool
}

func (s *State) raiseException(vector int) {
	panic(exceptionSignal{vector: vector})
}

// raiseAddressError aborts the instruction that tried to access a word or a
// long on an odd address
func (s *State) raiseAddressError(address uint32, write bool) {
	panic(exceptionSignal{
		vector:  vectorAddressError,
		address: address,
		write:   write,
		ir:      s.ir,
	})
}

// raiseJumpError is the address error of a prefetch from an odd target, that
// happens once the instruction has already been executed
func (s *State) raiseJumpError(address uint32) {
	panic(exceptionSignal{
		vector:           vectorAddressError,
		address:          address,
		ir:               s.ir,
		afterInstruction: true,
	})
}

// The bits of the status word pushed by the group 0 exceptions. The unused
// ones keep the value they had on the instruction register.
const (
	statusRead             = 0x10 // Clear on the accesses that were writing
	statusInstruction      = 0x08
	functionCodeSupervisor = 0x04
	functionCodeData       = 0x01
)

// statusWord builds the first word of the group 0 exception frame. The
// supervisor bit comes from the status register as it was before the
// exception switched to supervisor mode.
func statusWord(e exceptionSignal, sr uint16) uint32 {
	status := uint32(e.ir)&0xffe0 | functionCodeData
	if sr&flagS != 0 {
		status |= functionCodeSupervisor
	}
	if !e.write {
		status |= statusRead
	}
	return status
}

// The cost of the exception processing itself
func exceptionCycles(vector int) int {
	switch vector {
	case vectorBusError, vectorAddressError:
		return 50
	case vectorZeroDivide:
		return 38
	case vectorChk:
		return 38
	}
	return 34
}

/*
The cycles charged to an instruction aborted by an address error. The real
value is however far the instruction had got when the access failed, from 8 to
20 cycles depending on the instruction and the addressing mode. Reproducing it
needs the bus cycle detail that this emulator does not model.
*/
const abortedInstructionCycles = 8

/*
chargeExceptionCycles fixes the cycle count of the instruction that raised the
exception. The manual gives a single total for each one:

  - The traps and the faults that don't reach the bus, like the privilege
    violations, cost only the exception processing.
  - An address error on an operand aborts the instruction on the bus cycle
    that failed, only that much of it is charged.
  - An address error on a jump target happens once the instruction is done,
    so it is charged in full.
*/
func (s *State) chargeExceptionCycles(e exceptionSignal) {
	group0 := e.vector == vectorBusError || e.vector == vectorAddressError
	switch {
	case e.vector == vectorChk:
		// The bound was read and compared before the trap was decided
		s.cycles = s.instructionStartCycles +
			uint64(s.opcodes[s.ir].eaCycles+s.extraCycles)
	case !group0:
		s.cycles = s.instructionStartCycles
	case !e.afterInstruction:
		s.lastExceptionAborted = true
		s.cycles = s.instructionStartCycles + abortedInstructionCycles
	default:
		s.cycles += uint64(s.extraCycles)
	}
	s.cycles += uint64(exceptionCycles(e.vector))
}

/*
exceptionPC returns the program counter to save on the frame. The exceptions
that report a fault stack the instruction that caused it, so that the handler
can look at it. The ones requested by an instruction, the traps, stack the
next instruction instead.
*/
func (s *State) exceptionPC(vector int) uint32 {
	switch vector {
	case vectorIllegalInstruction, vectorPrivilegeViolation, vectorLineA, vectorLineF:
		return s.instructionPC
	}
	return s.reg.getPC()
}

// processException saves the state on the supervisor stack and jumps to the
// handler on the vector table
func (s *State) processException(e exceptionSignal) {
	if s.trace {
		fmt.Printf("Exception %d at PC $%06x\n", e.vector, s.reg.getPC())
	}

	s.lastExceptionVector = e.vector

	// The status register is saved as it was before entering supervisor mode
	sr := s.reg.getSR()
	s.reg.setSR(sr&^flagT | flagS)

	sp := s.reg.getSP()
	sp -= 4
	s.pokeLongRaw(sp, s.exceptionPC(e.vector))
	sp -= 2
	s.pokeWordRaw(sp, uint32(sr))

	if e.vector == vectorAddressError || e.vector == vectorBusError {
		// The group 0 exceptions push a bigger frame with enough information
		// to identify the failed access
		sp -= 2
		s.pokeWordRaw(sp, uint32(e.ir))
		sp -= 4
		s.pokeLongRaw(sp, e.address)
		sp -= 2
		s.pokeWordRaw(sp, statusWord(e, sr))
	}

	s.reg.setSP(sp)
	s.reg.setPC(s.peekLong(uint32(e.vector) * 4))
}

// SetIRQ sets the level of the interrupt request lines, from 0 for no
// interrupt to 7 for the non maskable one. While it is asserted, an interrupt
// is serviced before each instruction if the mask on the status register
// allows it.
func (s *State) SetIRQ(level uint8) {
	s.irqLevel = level & 7
}

// pendingInterrupt returns the level to service, or 0 if there is none. Level
// 7 is edge triggered and can't be masked, the rest are level triggered.
func (s *State) pendingInterrupt() uint8 {
	if s.irqLevel == 0 {
		return 0
	}
	if s.irqLevel == 7 {
		if s.irqLevel == s.lastIrqLevel {
			return 0
		}
		return 7
	}
	if s.irqLevel > s.reg.getInterruptMask() {
		return s.irqLevel
	}
	return 0
}

// processInterrupt services an interrupt. Only the autovectored interrupts,
// the ones used by the Macintosh, are supported.
func (s *State) processInterrupt(level uint8) {
	s.stopped = false
	s.processException(exceptionSignal{vector: vectorAutovector + int(level) - 1})
	s.reg.setInterruptMask(level)
	s.cycles += 44 // The exception processing and the interrupt acknowledge
}
