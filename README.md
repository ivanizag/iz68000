# iz68000 - Motorola 68000 emulator in Go

[![Go Reference](https://pkg.go.dev/badge/github.com/ivanizag/iz68000.svg)](https://pkg.go.dev/github.com/ivanizag/iz68000)

Motorola 68000 emulator library for Go, with instruction level timing.

It is being used in:

- Macintosh Plus emulator [izmac](https://github.com/ivanizag/izmac)
- Example [Motorola MC68000 Educational Computer Board](examples/tinyBasic)
  running Tiny BASIC

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

The cycle counts are verified against the suite. They follow the tables of the
manual, with a base time per instruction that has a column for the operands on
a register and another for the ones in memory, plus one of four effective
address calculation tables depending on whether the instruction reads the
operand, only computes its address, prefetches from it or is MOVEM. The times
that depend on the data are computed at run time: the shift counts, the bits
of MULU and MULS, the registers of MOVEM and the microcoded loops of DIVU and
DIVS.

## Accuracy

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

```
$10040  status, receiver ready on the bit 0, transmitter ready on the bit 1
$10042  data
```

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

The `.json.bin` files are read as they are, there is no need to run the
`decode.py` script of that repo. The tests of the instructions not implemented
yet are skipped, each one is dispatched by the opcode on its prefetch queue.
