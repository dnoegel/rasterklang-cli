package c64

import "testing"

// benchProgram is a tight loop that exercises the instrumented paths as hard
// as possible: an absolute load, an absolute store into the SID range, a
// zero-page read-modify-write and a branch, i.e. four bus reads and two bus
// writes per iteration.
func benchProgram(bus *Bus) {
	copy(bus.RAM[0x1000:], []byte{
		0xad, 0x00, 0x20, // LDA $2000
		0x8d, 0x04, 0xd4, // STA $D404
		0xe6, 0x10, // INC $10
		0x4c, 0x00, 0x10, // JMP $1000
	})
	bus.RAM[0x2000] = 0x41
}

func BenchmarkCPUStepBusTraffic(b *testing.B) {
	bus := NewBus(nil)
	benchProgram(bus)
	cpu := NewCPU(bus)
	cpu.PC = 0x1000

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := cpu.Step(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBusReadWrite(b *testing.B) {
	bus := NewBus(nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Write(uint16(i), byte(i))
		_ = bus.Read(uint16(i))
	}
}
