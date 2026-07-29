# iz68000 - Motorola MC68000 emulator in Go

Simple Motorola MC68000 emulator library for Go, with instruction level timing.
Extensive usage of the test suite [SingleStepTests/m68000](https://github.com/SingleStepTests/680x0) by David Harte.

The library is being used in:

- Example [Motorola MC68000 Educational Computer Board](examples/tinyBasic)
  running Tiny BASIC
- Macintosh Plus emulator [izMac](https://github.com/ivanizag/izmac)

## Example

```go
package main

import (
	"github.com/ivanizag/iz68000"
)

func main() {
	// Prepare cpu and memory
	memory := iz68000.NewFlatMemory()
	cpu := iz68000.NewM68000(memory)

	// Load the reset vectors and a program
	memory.Poke(0x0003, 0x00) // Supervisor stack pointer at $8000
	memory.Poke(0x0002, 0x80)
	memory.Poke(0x0007, 0x00) // Start at $1000
	memory.Poke(0x0006, 0x10)

	cpu.Reset()
	cpu.SetTrace(true)
	for i := 0; i < 10; i++ {
		cpu.ExecuteInstruction()
	}
}
```

## Status

All the MC68000 instructions are implemented, 82% of the 65536 opcode words:

MOVE, MOVEA, MOVEQ, MOVEM, MOVEP, LEA, PEA, EXT, SWAP, EXG, LINK, UNLK, ORI,
ANDI, SUBI, ADDI, EORI, CMPI, ADDQ, SUBQ, OR, AND, SUB, ADD, CMP, EOR, SUBA,
ADDA, CMPA, ADDX, SUBX, CMPM, MULU, MULS, DIVU, DIVS, ABCD, SBCD, NBCD, NEGX,
CLR, NEG, NOT, TST, TAS, CHK, BTST, BCHG, BCLR, BSET, ASL, ASR, LSL, LSR, ROL,
ROR, ROXL, ROXR, Bcc, BRA, BSR, DBcc, Scc, JMP, JSR, RTS, RTR, RTE, TRAP,
TRAPV, NOP, RESET, STOP, ILLEGAL, MOVE to and from SR, CCR and USP, the ORI,
ANDI and EORI variants that reach CCR and SR, and the line A and line F traps
that the Macintosh uses for the toolbox calls.

The remaining opcode words are the encodings that are illegal on the 68000.

The cycle counts are verified against the test suite. 

## Test suite resuls

Against the whole test suite, with the cycle counts checked, 311278 of 315000
scenarios pass. The 3722 that do not are all documented on
`harteSuite_test.go`:

- The V flag of ABCD, SBCD and NBCD, that the manual leaves undefined.
- MOVE.L, ADDX.L, SUBX.L and CMPM.L aborted by an address error on their
  second operand, where which half of the long was transferred first decides
  what has already been committed.
- Three of the seven words of the group 0 exception frame.
- The cycles of the CHK trap and of the bit operations on the high half of a
  data register, where the times are bimodal and the condition that picks
  between them is not always reproduced.

The cycles of an instruction aborted by an address error are not compared. It
stops part way through, after a number of cycles that depends on the
instruction and the addressing mode, so only a fixed approximation is charged.
Every instruction that completes is cycle exact.

Both that and the exception frame details are the prefetch queue and the bus
cycles of the real processor showing through, out of reach of an emulator with
instruction level timing. The tests of STOP are skipped, its final states
can't be reached by executing a single instruction.

## Example machine

`examples/tinyBasic` emulates the Motorola MC68000 Educational Computer Board
of 1981, the most minimal 68000 computer there is: a processor, RAM and two
serial ports on 6850 ACIAs. It runs Gordon Brandly's Tiny BASIC of 1985, that
was written for that board and whose whole interface with the hardware is the
status and data registers of the console port:

- $10040  status, receiver ready on the bit 0, transmitter ready on the bit 1
- $10042  data


To exeute the example, run:
```sh
cd examples/tinyBasic
go run .
```

## Tests

The unit tests run with `go test`. To also run the
[SingleStepTests/m68000](https://github.com/SingleStepTests/m68000) suite,
clone it next to this repo and enable it in `harteSuite_test.go`:

```sh
git clone https://github.com/SingleStepTests/m68000 ../m68000-tests
```

