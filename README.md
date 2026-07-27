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

All the 68000 instructions are implemented, 82% of the 65536 opcode words:

MOVE, MOVEA, MOVEQ, MOVEM, MOVEP, LEA, PEA, EXT, SWAP, EXG, LINK, UNLK, ORI,
ANDI, SUBI, ADDI, EORI, CMPI, ADDQ, SUBQ, OR, AND, SUB, ADD, CMP, EOR, SUBA,
ADDA, CMPA, ADDX, SUBX, CMPM, MULU, MULS, DIVU, DIVS, ABCD, SBCD, NBCD, NEGX,
CLR, NEG, NOT, TST, TAS, CHK, BTST, BCHG, BCLR, BSET, ASL, ASR, LSL, LSR, ROL,
ROR, ROXL, ROXR, Bcc, BRA, BSR, DBcc, Scc, JMP, JSR, RTS, RTR, RTE, TRAP,
TRAPV, NOP, RESET, STOP, ILLEGAL, MOVE to and from SR, CCR and USP, the ORI,
ANDI and EORI variants that reach CCR and SR, and the line A and line F traps
that the Macintosh uses for the toolbox calls.

The remaining opcode words are the encodings that are illegal on the 68000.

The cycle counts are the base times of the manual plus the effective address
calculation times. They are not exact yet for the operands on address
registers, for some of the long variants and for the divisions, where the
worst case is used.

## Accuracy

Against the whole test suite, 311787 of 315000 scenarios pass. The 3213 that
do not fall in three groups, all of them documented on `harteSuite_test.go`:

- The V flag of ABCD, SBCD and NBCD, that the manual leaves undefined.
- MOVE.L, ADDX.L, SUBX.L and CMPM.L aborted by an address error on their
  second operand, where which half of the long was transferred first decides
  what has already been committed.
- Three of the seven words of the group 0 exception frame.

The last two are the prefetch queue and the bus cycles of the real processor
showing through when an address error aborts an instruction, out of reach of
an emulator with instruction level timing. The tests of STOP are skipped, its
final states can't be reached by executing a single instruction.

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
