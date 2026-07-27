# iz68000 - Motorola 68000 emulator in Go

[![Go Reference](https://pkg.go.dev/badge/github.com/ivanizag/iz68000.svg)](https://pkg.go.dev/github.com/ivanizag/iz68000)

Motorola 68000 emulator library for Go, with instruction level timing.

It is being used in:

- Macintosh Plus emulator [izmac](https://github.com/ivanizag/izmac)

See the library documentation in [pkg.go.dev](https://pkg.go.dev/github.com/ivanizag/iz68000#section-documentation)

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

## Design

The opcodes are 16 bit words with the operands encoded on the free bits, so a
table with an entry per opcode word is needed. Instead of writing the 65536
entries, `m68000.go` describes each family of opcodes with a bit pattern that
`decoder.go` expands on init:

```go
{name: "ADD", pattern: "1101nnn0ssmmmrrr", ea: eaAll, timing: timingEA,
    operands: operandsEAToDataReg, cycles: 4, cyclesL: 6,
    build: buildAluToRegister(aluAdd, true)},
```

The size, the addressing mode category and the time taken by the effective
address calculation are resolved when expanding, the actions only have to
extract the register numbers. Expanding two patterns onto the same opcode word
panics, so the table can't have silent overlaps.

An exception can be raised in the middle of an instruction, when the effective
addresses are already resolved or even after some memory was written. To abort
the instruction the emulator panics with an `exceptionSignal`, recovered by
`ExecuteInstruction()`.

Addresses are masked to 24 bits, as the 68000 has only 24 address lines. The
Macintosh memory manager relies on it to store flags on the high byte of the
master pointers.

## Status

Implemented, 76% of the opcode words:

MOVE, MOVEA, MOVEQ, LEA, PEA, EXT, SWAP, ORI, ANDI, SUBI, ADDI, EORI, CMPI,
ADDQ, SUBQ, OR, AND, SUB, ADD, CMP, EOR, SUBA, ADDA, CMPA, NEGX, CLR, NEG,
NOT, TST, BTST, BCHG, BCLR, BSET, ASL, ASR, LSL, LSR, ROL, ROR, ROXL, ROXR,
Bcc, BRA, BSR, DBcc, Scc, JMP, JSR, RTS, RTR, RTE, LINK, UNLK, TRAP, TRAPV,
NOP, RESET, STOP, ILLEGAL and the line A and line F traps.

Not implemented yet:

MOVEM, MOVEP, MULU, MULS, DIVU, DIVS, ABCD, SBCD, NBCD, ADDX, SUBX, CMPM,
CHK, TAS, EXG, MOVE to/from SR, MOVE to CCR, MOVE to/from USP, ORI/ANDI/EORI
to CCR and to SR.

The cycle counts are the base times of the manual plus the effective address
calculation times. They are not exact yet for the operands on address
registers and for some of the long variants.

Against a sample of 35 test files, 86963 of 87239 scenarios pass. The 276
failures are all MOVE.L aborted by an address error on the destination, where
the order in which the two halves of the long are written decides which flags
are already committed. That, and three of the seven words of the group 0
exception frame, are the prefetch queue of the real processor showing through
and are out of reach of an emulator with instruction level timing.

## Tests

The unit tests run with `go test`. To also run the
[SingleStepTests/m68000](https://github.com/SingleStepTests/m68000) suite,
clone it next to this repo and enable it in `harteSuite_test.go`:

```sh
git clone https://github.com/SingleStepTests/m68000 ../m68000-tests
```

The `.json.bin` files are read as they are, there is no need to run the
`decode.py` script of that repo. The tests of the instructions not implemented
yet are skipped, each one is dispatched by the opcode on its prefetch queue.
