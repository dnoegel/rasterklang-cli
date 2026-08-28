# rk.trace.v1 wire format

Binary, length-prefixed, append-only event log emitted by `trace.Recorder`
and decoded by `trace.Reader`. It is the Layer A observation `Tr_E(S)` of the
behavioral decompilation specification, section 2.2: a cycle-timed transaction
trace over which trace-exactness is defined.

It is deliberately not JSON. A 60 second capture with all categories enabled
is hundreds of megabytes of instruction and memory traffic; the format has to
stay linear in disk and streamable in one forward pass.

All integers except the fixed-width fields noted below are unsigned LEB128
varints (`binary.PutUvarint` / `binary.ReadUvarint`).

## Header

```
magic            4 bytes   "RKTR"
format_version   1 byte    1
schema           blob      "rk.trace.v1"
environment_hash 32 bytes  sha256 of the canonical serialization of E
environment      blob      canonical serialization of E (see env.go)
categories       varint    trace.Category bitmask actually recorded
```

A `blob` is `varint length` followed by `length` bytes.

The environment blob makes the trace self-describing: a consumer can
re-derive the hash from the trace alone and check it against
`environment_hash`, without needing the original SID file.

## Records

```
record := varint payload_length
          payload

payload := 1 byte  record_type
           varint  cycle_delta        ; cycles since the previous record
           type specific fields
```

`cycle_delta` is the reason for the varint encoding: consecutive events in a
player routine are separated by a handful of cycles, so the common delta fits
in one byte. Cycles are absolute and monotonically non-decreasing, which is
what makes the delta unsigned.

`ordinal` is not stored. It is the record's zero-based index in the log, and
the reader materializes it. Together, `(cycle, ordinal)` is the `event_key` of
spec 2.2, and it totally orders the trace including same-cycle events.

### Record types

| type | name | fields after `cycle_delta` |
|---|---|---|
| 0x01 | `instr` | `pc` varint, `opcode` byte, `cycles` varint |
| 0x02 | `sid.write` | `chip` varint, `reg` byte (`& 0x1F`), `value` byte, `pc` varint |
| 0x03 | `sid.read` | `chip` varint, `reg` byte (`& 0x1F`), `value` byte, `pc` varint |
| 0x04 | `mem.read` | `addr` varint, `value` byte, `version` varint, `pc` varint |
| 0x05 | `mem.write` | `addr` varint, `value` byte, `version` varint, `pc` varint |
| 0x06 | `smc.write` | `addr` varint, `value` byte, `owner_pc` varint, `pc` varint |
| 0x07 | `irq.enter` | `vector` varint |
| 0x08 | `irq.exit` | `pc` varint |
| 0x09 | `call.enter` | `addr` varint |
| 0x0a | `call.exit` | `addr` varint |
| 0x0b | `frame.start` | `frame` varint (0-based) |
| 0x0c | `frame.end` | `frame` varint (0-based) |
| 0x0d | `cia.underflow` | `base` varint, `latch` varint, `control` byte |
| 0x0e | `cia.timer` | `base` varint, `latch` varint, `control` byte, `timer` varint |
| 0x0f | `vic.rasterirq` | `line` varint |
| 0x10 | `timer.state` | `raster_line` varint, `cia1_timer` varint, `cia1_ctrl` byte, `cia2_timer` varint, `cia2_ctrl` byte |

The record length prefix means an unknown record type can be skipped without
understanding it, so adding record types is backward compatible for readers
that tolerate unknown types.

## Register normalization

The SID bus decodes 32 addresses with mirroring. `sid.write` and `sid.read`
carry the bus-normalized register index `addr & 0x1F` in `[0x00, 0x1F]`, per
spec 2.1. The `chip` field exists so 2SID/3SID support does not need a format
bump; the current engine bus decodes one chip at `$D400`.

## Memory versioning

Every write to an address creates a new version of that cell: `mem.write`
carries the new version number, `mem.read` carries the version the read
observed. Version 0 is the initial memory image described by the environment.
This is what keeps runtime-copied or decompressed tables, and per-note
repointed tables, from collapsing into one undifferentiated blob in the
dynamic dependence graph (spec Phase 1).

## Self-modification

Every byte an instruction fetch covers (`[pc, pc + length(opcode))`) is
recorded as owned by that instruction. A later store into an owned byte emits
`smc.write` in addition to the ordinary `mem.write`, carrying both the storing
instruction's `pc` and the `owner_pc` of the instruction whose bytes were
overwritten. This is the `code` edge of spec Phase 1.

## Cycle assignment and its known limit

The rasterklang CPU executes an instruction atomically and reports its total
cycle count afterwards, rather than being clocked cycle by cycle. Bus accesses
are therefore buffered for the duration of an instruction and timestamped when
the instruction boundary closes:

- **Writes** are placed on the instruction's final cycle
  (`start + cycles - 1`). This is exact: a 6502 store or read-modify-write
  drives the bus on its last cycle, and stores are the only way a program
  reaches the SID. Multiple writes in one instruction (stack pushes, `BRK`)
  are laid onto the trailing cycles in order, which is exact for `PHA`/`PHP`
  and approximate for `BRK`.
- **Reads** are placed at `start + min(read_index, cycles - 1)`. This is exact
  for the opcode fetch and the operand bytes, and for the data read of the
  zero-page and absolute addressing modes; it is up to one cycle early where
  the emulator does not model a dummy read (indexed modes that cross a page,
  and the second half of a read-modify-write).

Consequently the trace is exact for the SID write stream that Layer A
exactness is defined over, and accurate to within a small bounded error for
the read stream, which is only used as dependence-graph evidence. Recovering
exact read cycles would require a cycle-stepped CPU core, which is a larger
change than milestone M0 calls for.

Emulator-internal peeks (the lookahead reads that `cpu_run.go` and
`StepWithInfo` perform outside `CPU.Step`) are not recorded: recording is
gated on an open instruction boundary, so the trace contains only accesses a
real 6510 would have performed.

## Determinism

The format contains no timestamps, no map iteration and no host-dependent
values. Two runs of the same `(S, E)` produce byte-identical files, which is
the M0 determinism criterion and the precondition for the hash comparison
procedure of spec 3.3.
